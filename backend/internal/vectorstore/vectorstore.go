package vectorstore

import (
	"context"

	"backend/internal/model"
)

// VectorRecord is what gets written to the vector store for a single chunk.
type VectorRecord struct {
	ID              string
	KnowledgeBaseID string
	DocumentID      string
	ChunkID         string
	Filename        string
	SourceType      string
	SectionTitle    string
	PageStart       int
	PageEnd         int
	ChunkIndex      int
	Text            string
	Embedding       []float32
}

// SearchResult is a single hit returned by semantic search.
type SearchResult struct {
	ChunkID         string  `json:"chunk_id"`
	DocumentID      string  `json:"document_id"`
	KnowledgeBaseID string  `json:"knowledge_base_id"`
	Filename        string  `json:"filename"`
	SourceType      string  `json:"source_type"`
	SectionTitle    string  `json:"section_title"`
	PageStart       int     `json:"page_start"`
	PageEnd         int     `json:"page_end"`
	ChunkIndex      int     `json:"chunk_index"`
	Text            string  `json:"text"`
	Score           float32 `json:"score"`
}

// VectorStore persists and retrieves embedding vectors.
type VectorStore interface {
	// Upsert inserts or replaces records.
	Upsert(ctx context.Context, records []VectorRecord) error
	// DeleteByDocument removes all vectors belonging to a document.
	DeleteByDocument(ctx context.Context, knowledgeBaseID, documentID string) error
	// Search returns the topK most similar records to the query embedding.
	Search(ctx context.Context, knowledgeBaseID string, embedding []float32, topK int) ([]SearchResult, error)
}

// Noop discards all writes and returns empty search results.
type Noop struct{}

func (Noop) Upsert(_ context.Context, _ []VectorRecord) error { return nil }
func (Noop) DeleteByDocument(_ context.Context, _, _ string) error { return nil }
func (Noop) Search(_ context.Context, _ string, _ []float32, _ int) ([]SearchResult, error) {
	return nil, nil
}

// BuildRecords converts document chunks + embeddings into VectorRecords.
func BuildRecords(doc model.Document, chunks []model.DocumentChunk, embeddings [][]float32) []VectorRecord {
	records := make([]VectorRecord, 0, len(chunks))
	for i, c := range chunks {
		if i >= len(embeddings) {
			break
		}
		records = append(records, VectorRecord{
			ID:              c.ChunkID,
			KnowledgeBaseID: doc.KnowledgeBaseID,
			DocumentID:      doc.DocumentID,
			ChunkID:         c.ChunkID,
			Filename:        doc.Filename,
			SourceType:      doc.SourceType,
			SectionTitle:    c.SectionTitle,
			PageStart:       c.PageStart,
			PageEnd:         c.PageEnd,
			ChunkIndex:      c.ChunkIndex,
			Text:            c.Text,
			Embedding:       embeddings[i],
		})
	}
	return records
}
