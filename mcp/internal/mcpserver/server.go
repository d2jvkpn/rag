package mcpserver

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/d2jvkpn/rag/backend/pkg/infra"
	"github.com/d2jvkpn/rag/backend/pkg/rag"
)

// Server is a standalone MCP server exposing semantic/BM25/hybrid search over
// a configured subset of Milvus collections. It talks to the embedder and
// Milvus directly (reusing backend/pkg/rag), independent of the main
// backend's Store/Service/auth stack.
type Server struct {
	mcpServer   *mcp.Server
	embedder    rag.Embedder
	vectorStore rag.VectorStore
	collections map[string]string // name -> description
}

// New builds a Server from an MCP config loaded via LoadConfig.
func New(v *viper.Viper) (*Server, error) {
	embedder := rag.NewOpenAIEmbedder(
		v.GetString("embedder.base_url"),
		v.GetString("embedder.api_key"),
		v.GetString("embedder.model"),
		v.GetInt("embedder.batch_size"),
	)

	milvus, err := rag.NewMilvus(
		v.GetString("milvus.addr"),
		v.GetString("milvus.db"),
		v.GetString("milvus.api_key"),
	)
	if err != nil {
		return nil, fmt.Errorf("connect milvus: %w", err)
	}

	collections := make(map[string]string)
	for _, c := range Collections(v) {
		collections[c.Name] = c.Description
	}
	for name := range collections {
		if err := milvus.ValidateKnowledgeBase(name); err != nil {
			_ = milvus.Close(context.Background())
			return nil, fmt.Errorf("configured collection %q: %w", name, err)
		}
	}

	s, err := newServer(embedder, milvus, collections, v.GetString("mcp.description"))
	if err != nil {
		_ = milvus.Close(context.Background())
		return nil, err
	}
	return s, nil
}

// newServer wires a Server from already-constructed components, independent
// of config loading or how the VectorStore was connected. Split out from New
// so the search tool's domain logic (allow-list, topK clamping, mode
// defaulting) can be unit-tested with fakes, without a live Milvus.
func newServer(
	embedder rag.Embedder, vectorStore rag.VectorStore, collections map[string]string, description string,
) (*Server, error) {
	s := &Server{
		embedder:    embedder,
		vectorStore: vectorStore,
		collections: collections,
	}

	s.mcpServer = mcp.NewServer(&mcp.Implementation{
		Name:    "rag-mcp",
		Version: "v0.1.0",
	}, &mcp.ServerOptions{
		Instructions: description,
	})

	if err := s.registerSearchTool(); err != nil {
		return nil, fmt.Errorf("register search tool: %w", err)
	}
	return s, nil
}

// HTTPHandler exposes the server over the MCP Streamable HTTP transport. When
// apiKey is non-empty, requests must carry "Authorization: Bearer <apiKey>";
// an empty apiKey leaves the server unauthenticated (logged as a warning).
func (s *Server) HTTPHandler(apiKey string) http.Handler {
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return s.mcpServer
	}, nil)

	if apiKey == "" {
		infra.L.Warn("auth.api_key is not configured; mcp server is running without authentication")
		return handler
	}

	verifier := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if subtle.ConstantTimeCompare([]byte(token), []byte(apiKey)) != 1 {
			return nil, auth.ErrInvalidToken
		}
		// The shared key never expires; the library requires a non-zero
		// Expiration, so report one comfortably past any single request.
		return &auth.TokenInfo{Expiration: time.Now().Add(time.Hour)}, nil
	}
	return auth.RequireBearerToken(verifier, nil)(handler)
}

// Shutdown releases the Milvus connection.
func (s *Server) Shutdown(ctx context.Context) error {
	if c, ok := s.vectorStore.(interface{ Close(context.Context) error }); ok {
		return c.Close(ctx)
	}
	return nil
}

// SearchInput is the input schema for the "search" MCP tool.
type SearchInput struct {
	Collection  string   `json:"collection" jsonschema:"name of the collection to search"`
	Query       string   `json:"query" jsonschema:"the search query text"`
	TopK        int      `json:"top_k,omitempty" jsonschema:"number of results to return (default 5, max 50)"`
	SearchMode  string   `json:"search_mode,omitempty" jsonschema:"dense (default), bm25, or hybrid"`
	DocumentIDs []string `json:"document_ids,omitempty" jsonschema:"optional: restrict results to these document IDs"`
}

// SearchOutput is the output schema for the "search" MCP tool.
type SearchOutput struct {
	Items []rag.SearchResult `json:"items"`
}

// registerSearchTool builds the search tool's input schema with a "collection"
// enum drawn from the configured collections, so an agent can discover what's
// searchable directly from the tool schema without a separate list-tool.
func (s *Server) registerSearchTool() error {
	schema, err := jsonschema.For[SearchInput](nil)
	if err != nil {
		return fmt.Errorf("infer search input schema: %w", err)
	}
	prop, ok := schema.Properties["collection"]
	if !ok {
		return fmt.Errorf("search input schema missing collection property")
	}

	var desc strings.Builder
	desc.WriteString("Search a configured knowledge base collection using dense, bm25, or hybrid retrieval.\n" +
		"Available collections:\n")
	names := make([]any, 0, len(s.collections))
	for name, d := range s.collections {
		names = append(names, name)
		if d != "" {
			fmt.Fprintf(&desc, "- %s: %s\n", name, d)
		} else {
			fmt.Fprintf(&desc, "- %s\n", name)
		}
	}
	prop.Enum = names

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "search",
		Description: desc.String(),
		InputSchema: schema,
	}, s.handleSearch)
	return nil
}

func (s *Server) handleSearch(
	ctx context.Context, _ *mcp.CallToolRequest, in SearchInput,
) (result *mcp.CallToolResult, out SearchOutput, err error) {
	start := time.Now()

	topK := rag.ClampTopK(in.TopK)
	searchMode := in.SearchMode
	if searchMode == "" {
		searchMode = rag.SearchModeDense
	}

	var stats rag.QueryStats
	defer func() {
		fields := []zap.Field{
			zap.String("collection", in.Collection),
			zap.String("query", in.Query),
			zap.String("search_mode", searchMode),
			zap.Int("top_k", topK),
			zap.Int("document_ids", len(in.DocumentIDs)),
			zap.Int("embedding_tokens", stats.EmbeddingTokens),
			zap.Duration("embed_latency", stats.EmbedLatency.Truncate(time.Millisecond)),
			zap.Duration("milvus_latency", stats.SearchLatency.Truncate(time.Millisecond)),
			zap.Int("items", len(out.Items)),
			zap.Duration("latency", time.Since(start).Truncate(time.Millisecond)),
		}
		if err != nil {
			infra.L.Warn("search", append(fields, zap.Error(err))...)
		} else {
			infra.L.Info("search", fields...)
		}
	}()

	if _, ok := s.collections[in.Collection]; !ok {
		return nil, SearchOutput{}, fmt.Errorf("collection %q is not configured for this server", in.Collection)
	}
	if strings.TrimSpace(in.Query) == "" {
		return nil, SearchOutput{}, fmt.Errorf("query is required")
	}

	items, stats, err := rag.Query(ctx, s.embedder, s.vectorStore, rag.QueryParams{
		KnowledgeBaseID: in.Collection,
		Query:           in.Query,
		TopK:            topK,
		Mode:            searchMode,
		DocumentIDs:     in.DocumentIDs,
	})
	if err != nil {
		return nil, SearchOutput{}, err
	}

	out = SearchOutput{Items: items}
	return nil, out, nil
}
