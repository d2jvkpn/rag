package rag

import (
	"context"

	"github.com/d2jvkpn/rag/backend/internal/model"
)

// VectorRecord is what gets written to the vector store for a single chunk.
type VectorRecord struct {
	KnowledgeBaseID string
	DocumentID      string
	Filename        string
	ChunkID         string
	ChunkIndex      int
	SectionTitle    string
	PageStart       int
	PageEnd         int
	Tags            []string
	FileSHA256      string
	Text            string
	Embedding       []float32
}

// SearchResult is a single hit returned by semantic search.
type SearchResult struct {
	KnowledgeBaseID string   `json:"knowledge_base_id"`
	DocumentID      string   `json:"document_id"`
	Filename        string   `json:"filename"`
	ChunkID         string   `json:"chunk_id"`
	ChunkIndex      int      `json:"chunk_index"`
	SectionTitle    string   `json:"section_title"`
	PageStart       int      `json:"page_start"`
	PageEnd         int      `json:"page_end"`
	Tags            []string `json:"tags,omitempty"`
	FileSHA256      string   `json:"file_sha256,omitempty"`
	Text            string   `json:"text"`
	Score           float32  `json:"score"`
}

// SearchMode selects which retrieval method to use.
type SearchMode = string

const (
	SearchModeDense  SearchMode = "dense"  // vector similarity (default)
	SearchModeBM25   SearchMode = "bm25"   // full-text BM25 (Milvus 2.5+)
	SearchModeHybrid SearchMode = "hybrid" // dense + BM25 with RRF reranking
)

// SearchRequest carries all parameters for a VectorStore search.
type SearchRequest struct {
	KnowledgeBaseID string
	Embedding       []float32 // required for dense and hybrid modes
	Query           string    // required for bm25 and hybrid modes
	TopK            int
	DocumentIDs     []string   // optional: restrict results to these documents
	Mode            SearchMode // defaults to SearchModeDense when empty

	// Tuning params — zero value means use server default.
	EF        int     // HNSW ef (dense/hybrid); must be >= TopK when set
	DropRatio float64 // BM25 drop_ratio_search (bm25/hybrid); 0.0–1.0
	RRFK      int     // RRF k parameter (hybrid only); default 60
}

// VectorStore persists and retrieves embedding vectors.
type VectorStore interface {
	// ValidateKnowledgeBase returns an error if kbID is not a known collection.
	ValidateKnowledgeBase(kbID string) error
	// CreateKnowledgeBase creates or loads a collection at runtime.
	CreateKnowledgeBase(ctx context.Context, cfg CollectionConfig) error
	// DeleteKnowledgeBase drops the collection permanently.
	DeleteKnowledgeBase(ctx context.Context, kbID string) error
	// Upsert inserts or replaces records.
	Upsert(ctx context.Context, records []VectorRecord) error
	// DeleteByDocument removes all vectors belonging to a document.
	DeleteByDocument(ctx context.Context, knowledgeBaseID, documentID string) error
	// Search retrieves the topK most relevant records using the specified mode.
	Search(ctx context.Context, req SearchRequest) ([]SearchResult, error)
}

// NoopVectorStore discards all writes and returns empty search results.
type NoopVectorStore struct{}

func (NoopVectorStore) ValidateKnowledgeBase(_ string) error                            { return nil }
func (NoopVectorStore) CreateKnowledgeBase(_ context.Context, _ CollectionConfig) error { return nil }
func (NoopVectorStore) DeleteKnowledgeBase(_ context.Context, _ string) error           { return nil }
func (NoopVectorStore) Upsert(_ context.Context, _ []VectorRecord) error                { return nil }
func (NoopVectorStore) DeleteByDocument(_ context.Context, _, _ string) error           { return nil }
func (NoopVectorStore) Search(_ context.Context, _ SearchRequest) ([]SearchResult, error) {
	return nil, nil
}

// CollectionConfig holds per-collection settings read from config.
type CollectionConfig struct {
	Name         string
	Dim          int
	ChunkSize    int
	ChunkOverlap int
	MinChunks    int
	Analyzer     string // BM25 text analyzer; defaults to "chinese" if empty
}

// BuildRecords converts document chunks + embeddings into VectorRecords.
func BuildRecords(
	doc model.Document,
	chunks []model.DocumentChunk,
	embeddings [][]float32,
) []VectorRecord {
	var records []VectorRecord

	records = make([]VectorRecord, 0, len(chunks))
	for i, c := range chunks {
		if i >= len(embeddings) {
			break
		}
		records = append(records, VectorRecord{
			KnowledgeBaseID: doc.KnowledgeBaseID,
			DocumentID:      doc.DocumentID,
			Filename:        doc.Filename,
			ChunkID:         c.ChunkID,
			ChunkIndex:      c.ChunkIndex,
			SectionTitle:    c.SectionTitle,
			PageStart:       c.PageStart,
			PageEnd:         c.PageEnd,
			Tags:            doc.Tags,
			FileSHA256:      doc.SHA256,
			Text:            c.Text,
			Embedding:       embeddings[i],
		})
	}
	return records
}
