package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/d2jvkpn/rag/backend/pkg/rag"
)

type fakeEmbedder struct {
	calls int
	err   error
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{0.1, 0.2}
	}
	return out, nil
}

func (f *fakeEmbedder) Model() string { return "fake" }

type fakeVectorStore struct {
	lastReq rag.SearchRequest
	results []rag.SearchResult
	err     error
}

func (f *fakeVectorStore) ValidateKnowledgeBase(string) error { return nil }
func (f *fakeVectorStore) CreateKnowledgeBase(context.Context, rag.CollectionConfig) error {
	return nil
}
func (f *fakeVectorStore) DeleteKnowledgeBase(context.Context, string) error      { return nil }
func (f *fakeVectorStore) Upsert(context.Context, []rag.VectorRecord) error       { return nil }
func (f *fakeVectorStore) DeleteByDocument(context.Context, string, string) error { return nil }
func (f *fakeVectorStore) Search(_ context.Context, req rag.SearchRequest) ([]rag.SearchResult, error) {
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

func testServer(t *testing.T, embedder *fakeEmbedder, store *fakeVectorStore) *Server {
	t.Helper()
	s, err := newServer(embedder, store, map[string]string{
		"kb-a": "collection A",
		"kb-b": "",
	}, "test server")
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	return s
}

func TestHandleSearchRejectsUnconfiguredCollection(t *testing.T) {
	s := testServer(t, &fakeEmbedder{}, &fakeVectorStore{})

	_, _, err := s.handleSearch(context.Background(), &mcp.CallToolRequest{}, SearchInput{
		Collection: "not-configured",
		Query:      "hello",
	})
	if err == nil {
		t.Fatal("expected error for unconfigured collection, got nil")
	}
}

func TestHandleSearchRejectsEmptyQuery(t *testing.T) {
	s := testServer(t, &fakeEmbedder{}, &fakeVectorStore{})

	_, _, err := s.handleSearch(context.Background(), &mcp.CallToolRequest{}, SearchInput{
		Collection: "kb-a",
		Query:      "   ",
	})
	if err == nil {
		t.Fatal("expected error for empty query, got nil")
	}
}

func TestHandleSearchDefaultsAndClampsTopK(t *testing.T) {
	store := &fakeVectorStore{}
	s := testServer(t, &fakeEmbedder{}, store)

	cases := []struct {
		in   int
		want int
	}{
		{0, 5},
		{-3, 5},
		{10, 10},
		{500, 50},
	}
	for _, c := range cases {
		_, _, err := s.handleSearch(context.Background(), &mcp.CallToolRequest{}, SearchInput{
			Collection: "kb-a",
			Query:      "hello",
			TopK:       c.in,
		})
		if err != nil {
			t.Fatalf("topK=%d: unexpected error: %v", c.in, err)
		}
		if store.lastReq.TopK != c.want {
			t.Errorf("topK=%d: got TopK=%d, want %d", c.in, store.lastReq.TopK, c.want)
		}
	}
}

func TestHandleSearchDefaultsToDenseAndEmbeds(t *testing.T) {
	embedder := &fakeEmbedder{}
	store := &fakeVectorStore{}
	s := testServer(t, embedder, store)

	_, _, err := s.handleSearch(context.Background(), &mcp.CallToolRequest{}, SearchInput{
		Collection: "kb-a",
		Query:      "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.lastReq.Mode != rag.SearchModeDense {
		t.Errorf("got mode %q, want %q", store.lastReq.Mode, rag.SearchModeDense)
	}
	if embedder.calls != 1 {
		t.Errorf("got %d embed calls, want 1", embedder.calls)
	}
	if len(store.lastReq.Embedding) == 0 {
		t.Error("expected embedding to be populated for dense search")
	}
}

func TestHandleSearchBM25SkipsEmbedding(t *testing.T) {
	embedder := &fakeEmbedder{}
	store := &fakeVectorStore{}
	s := testServer(t, embedder, store)

	_, _, err := s.handleSearch(context.Background(), &mcp.CallToolRequest{}, SearchInput{
		Collection: "kb-a",
		Query:      "hello",
		SearchMode: rag.SearchModeBM25,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if embedder.calls != 0 {
		t.Errorf("got %d embed calls for bm25, want 0", embedder.calls)
	}
	if len(store.lastReq.Embedding) != 0 {
		t.Error("expected no embedding to be set for bm25 search")
	}
}

func TestHandleSearchPropagatesEmbedError(t *testing.T) {
	embedder := &fakeEmbedder{err: context.DeadlineExceeded}
	store := &fakeVectorStore{}
	s := testServer(t, embedder, store)

	_, _, err := s.handleSearch(context.Background(), &mcp.CallToolRequest{}, SearchInput{
		Collection: "kb-a",
		Query:      "hello",
	})
	if err == nil {
		t.Fatal("expected error when embedder fails, got nil")
	}
}

// TestSearchToolOverProtocol drives the registered tool through a real MCP
// client/server session (in-memory transport), exercising schema inference,
// the collection enum, and JSON marshaling/unmarshaling end-to-end — not just
// the Go-level handleSearch call.
func TestSearchToolOverProtocol(t *testing.T) {
	store := &fakeVectorStore{results: []rag.SearchResult{{ChunkID: "c1", Text: "hit"}}}
	s := testServer(t, &fakeEmbedder{}, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() {
		_ = s.mcpServer.Run(ctx, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	toolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(toolsResult.Tools) != 1 || toolsResult.Tools[0].Name != "search" {
		t.Fatalf("got tools %+v, want a single \"search\" tool", toolsResult.Tools)
	}

	callResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"collection": "kb-a", "query": "hello"},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if callResult.IsError {
		t.Fatalf("unexpected tool error: %+v", callResult.Content)
	}

	badResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"collection": "not-configured", "query": "hello"},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if !badResult.IsError {
		t.Fatal("expected tool error for unconfigured collection via protocol call, got success")
	}
}

func TestHTTPHandlerRequiresBearerTokenWhenConfigured(t *testing.T) {
	s := testServer(t, &fakeEmbedder{}, &fakeVectorStore{})
	handler := s.HTTPHandler("secret")

	cases := []struct {
		name   string
		header string
	}{
		{"missing header", ""},
		{"wrong token", "Bearer wrong"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("got status %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestHTTPHandlerAcceptsCorrectBearerToken(t *testing.T) {
	s := testServer(t, &fakeEmbedder{}, &fakeVectorStore{})
	handler := s.HTTPHandler("secret")

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("got status %d, want request to pass auth", rec.Code)
	}
}

func TestHTTPHandlerSkipsAuthWhenAPIKeyEmpty(t *testing.T) {
	s := testServer(t, &fakeEmbedder{}, &fakeVectorStore{})
	handler := s.HTTPHandler("")

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("got status %d, want no auth to be enforced", rec.Code)
	}
}
