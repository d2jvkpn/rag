package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/viper"

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

// HTTPHandler exposes the server over the MCP Streamable HTTP transport.
func (s *Server) HTTPHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return s.mcpServer
	}, nil)
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
	desc.WriteString("Search a configured knowledge base collection using dense, bm25, or hybrid retrieval.\nAvailable collections:\n")
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
) (*mcp.CallToolResult, SearchOutput, error) {
	if _, ok := s.collections[in.Collection]; !ok {
		return nil, SearchOutput{}, fmt.Errorf("collection %q is not configured for this server", in.Collection)
	}
	if strings.TrimSpace(in.Query) == "" {
		return nil, SearchOutput{}, fmt.Errorf("query is required")
	}

	topK := in.TopK
	if topK <= 0 {
		topK = 5
	}
	if topK > 50 {
		topK = 50
	}

	searchMode := in.SearchMode
	if searchMode == "" {
		searchMode = rag.SearchModeDense
	}

	req := rag.SearchRequest{
		KnowledgeBaseID: in.Collection,
		Query:           in.Query,
		TopK:            topK,
		DocumentIDs:     in.DocumentIDs,
		Mode:            searchMode,
	}

	if searchMode != rag.SearchModeBM25 {
		embeddings, err := s.embedder.Embed(ctx, []string{in.Query})
		if err != nil {
			return nil, SearchOutput{}, fmt.Errorf("embed query: %w", err)
		}
		if len(embeddings) == 0 || len(embeddings[0]) == 0 {
			return nil, SearchOutput{}, fmt.Errorf("embedder returned no vector for query")
		}
		req.Embedding = embeddings[0]
	}

	items, err := s.vectorStore.Search(ctx, req)
	if err != nil {
		return nil, SearchOutput{}, fmt.Errorf("vector search: %w", err)
	}

	return nil, SearchOutput{Items: items}, nil
}
