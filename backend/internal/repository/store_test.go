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
