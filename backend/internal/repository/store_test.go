package repository

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"backend/internal/model"
)

func TestJSONStoreBackfillsKnowledgeBasesFromDocuments(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	now := time.Now().UTC()
	state := State{
		Users: map[string]model.User{},
		Documents: map[string]model.Document{
			"doc-1": {
				DocumentID:      "doc-1",
				KnowledgeBaseID: "legacy-kb",
				CreatedAt:       now,
				UpdatedAt:       now,
			},
		},
		Chunks: map[string][]model.DocumentChunk{},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	store, err := NewJSONStore(path, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.EnsureKnowledgeBasesFromDocuments(768); err != nil {
		t.Fatalf("backfill knowledge bases: %v", err)
	}
	items := store.ListKnowledgeBases()
	if len(items) != 1 || items[0].KnowledgeBaseID != "legacy-kb" {
		t.Fatalf("expected legacy-kb backfill, got %+v", items)
	}
	if items[0].Dim != 768 || items[0].ChunkSize != 1000 || items[0].ChunkOverlap != 150 || items[0].MinChunks != 3 {
		t.Fatalf("unexpected backfilled config: %+v", items[0])
	}
}

func TestJSONStoreListChunksPage(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	store, err := NewJSONStore(path, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	chunks := make([]model.DocumentChunk, 0, 5)
	for i := 0; i < 5; i++ {
		chunks = append(chunks, model.DocumentChunk{
			ChunkID:    "chunk-" + string(rune('a'+i)),
			DocumentID: "doc-1",
			ChunkIndex: i,
			Text:       "text",
		})
	}
	if err := store.ReplaceChunks("doc-1", chunks); err != nil {
		t.Fatalf("replace chunks: %v", err)
	}

	page, err := store.ListChunksPage("doc-1", 2, 2)
	if err != nil {
		t.Fatalf("list chunks page: %v", err)
	}
	if page.Page != 2 || page.PageSize != 2 || page.Total != 5 || page.TotalPages != 3 || !page.HasNext || !page.HasPrev {
		t.Fatalf("unexpected page metadata: %+v", page)
	}
	if len(page.Items) != 2 || page.Items[0].ChunkIndex != 2 || page.Items[1].ChunkIndex != 3 {
		t.Fatalf("unexpected page items: %+v", page.Items)
	}

	last, err := store.ListChunksPage("doc-1", 99, 2)
	if err != nil {
		t.Fatalf("list last chunks page: %v", err)
	}
	if last.Page != 3 || len(last.Items) != 1 || last.Items[0].ChunkIndex != 4 || last.HasNext || !last.HasPrev {
		t.Fatalf("unexpected last page: %+v", last)
	}
}
