package rag

import (
	"context"
	"fmt"
	"time"
)

// QueryParams carries the parameters shared by callers that embed a query
// text and search a VectorStore: DocumentService.Query (backend) and the MCP
// search tool (mcp/internal/mcpserver).
type QueryParams struct {
	KnowledgeBaseID string
	Query           string
	TopK            int
	Mode            SearchMode // defaults to SearchModeDense when empty
	DocumentIDs     []string

	// Tuning params — zero value means use server default.
	EF        int
	DropRatio float64
	RRFK      int
}

// QueryStats reports embedding/search cost for a Query call, for callers that
// want to log or meter it (e.g. the MCP search server).
type QueryStats struct {
	EmbeddingTokens int
	EmbedLatency    time.Duration
	SearchLatency   time.Duration
}

// ClampTopK normalizes topK to the [1, 50] range used across search callers,
// defaulting to 5.
func ClampTopK(topK int) int {
	if topK <= 0 {
		return 5
	}
	if topK > 50 {
		return 50
	}
	return topK
}

// Query embeds p.Query (unless p.Mode is bm25) and searches store, applying
// the topK/mode defaulting shared by every search caller.
func Query(
	ctx context.Context, embedder Embedder, store VectorStore, p QueryParams,
) (items []SearchResult, stats QueryStats, err error) {
	mode := p.Mode
	if mode == "" {
		mode = SearchModeDense
	}

	req := SearchRequest{
		KnowledgeBaseID: p.KnowledgeBaseID,
		Query:           p.Query,
		TopK:            ClampTopK(p.TopK),
		DocumentIDs:     p.DocumentIDs,
		Mode:            mode,
		EF:              p.EF,
		DropRatio:       p.DropRatio,
		RRFK:            p.RRFK,
	}

	if mode != SearchModeBM25 {
		embedStart := time.Now()
		var embeddings [][]float32
		if ue, ok := embedder.(EmbedderWithUsage); ok {
			embeddings, stats.EmbeddingTokens, err = ue.EmbedWithUsage(ctx, []string{p.Query})
		} else {
			embeddings, err = embedder.Embed(ctx, []string{p.Query})
		}
		stats.EmbedLatency = time.Since(embedStart)
		if err != nil {
			return nil, stats, fmt.Errorf("embed query: %w", err)
		}
		if len(embeddings) == 0 || len(embeddings[0]) == 0 {
			return nil, stats, fmt.Errorf(
				"embedder returned no vector for query; configure embedder.base_url and embedder.api_key",
			)
		}
		req.Embedding = embeddings[0]
	}

	searchStart := time.Now()
	items, err = store.Search(ctx, req)
	stats.SearchLatency = time.Since(searchStart)
	if err != nil {
		return nil, stats, fmt.Errorf("vector search: %w", err)
	}
	return items, stats, nil
}
