package repository

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/d2jvkpn/rag/backend/internal/model"
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

	store, _, err := NewJSONStore(path, InitAccount{})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.EnsureKnowledgeBasesFromDocuments(768, "text-embedding-v4"); err != nil {
		t.Fatalf("backfill knowledge bases: %v", err)
	}
	items := store.ListKnowledgeBases()
	if len(items) != 1 || items[0].KnowledgeBaseID != "legacy-kb" {
		t.Fatalf("expected legacy-kb backfill, got %+v", items)
	}
	if items[0].Dim != 768 || items[0].Model != "text-embedding-v4" || items[0].ChunkSize != 800 || items[0].ChunkOverlap != 100 || items[0].MinChunks != 2 {
		t.Fatalf("unexpected backfilled config: %+v", items[0])
	}
}

func TestJSONStoreListDocumentsPage(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	store, _, err := NewJSONStore(path, InitAccount{})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	base := time.Now().UTC()
	for i := 0; i < 5; i++ {
		status := "indexed"
		if i%2 == 0 {
			status = "failed"
		}
		document := model.Document{
			DocumentID:      "doc-" + string(rune('a'+i)),
			KnowledgeBaseID: "kb-1",
			CreatedAt:       base.Add(time.Duration(i) * time.Minute),
			UpdatedAt:       base.Add(time.Duration(i) * time.Minute),
			Status:          status,
			SHA256:          "sha-" + string(rune('a'+i)),
			Tags:            []string{"release"},
		}
		if err := store.CreateDocument(document); err != nil {
			t.Fatalf("create document: %v", err)
		}
	}
	if err := store.CreateDocument(model.Document{
		DocumentID:      "doc-other",
		KnowledgeBaseID: "kb-2",
		CreatedAt:       base.Add(10 * time.Minute),
		UpdatedAt:       base.Add(10 * time.Minute),
		Status:          "failed",
		SHA256:          "sha-other",
		Tags:            []string{"release"},
	}); err != nil {
		t.Fatalf("create other document: %v", err)
	}

	page, err := store.ListDocumentsPage("kb-1", "release", "failed", 1, 2)
	if err != nil {
		t.Fatalf("list documents page: %v", err)
	}
	if page.Page != 1 || page.PageSize != 2 || page.Total != 3 || page.TotalPages != 2 || !page.HasNext || page.HasPrev {
		t.Fatalf("unexpected page metadata: %+v", page)
	}
	if len(page.Items) != 2 || page.Items[0].DocumentID != "doc-e" || page.Items[1].DocumentID != "doc-c" {
		t.Fatalf("unexpected page items: %+v", page.Items)
	}

	last, err := store.ListDocumentsPage("kb-1", "release", "failed", 9, 2)
	if err != nil {
		t.Fatalf("list last documents page: %v", err)
	}
	if last.Page != 2 || len(last.Items) != 1 || last.Items[0].DocumentID != "doc-a" || last.HasNext || !last.HasPrev {
		t.Fatalf("unexpected last page: %+v", last)
	}
}

// TestJSONStoreUpdateDocumentAfterDeleteReturnsNotFound guards against a
// concurrent-delete race: a background task (embed/index) that started before
// DeleteDocument ran must not be able to resurrect the row via UpdateDocument.
func TestJSONStoreUpdateDocumentAfterDeleteReturnsNotFound(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	store, _, err := NewJSONStore(path, InitAccount{})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	now := time.Now().UTC()
	document := model.Document{
		DocumentID:      "doc-race",
		KnowledgeBaseID: "kb-1",
		CreatedAt:       now,
		UpdatedAt:       now,
		Status:          "processing",
		SHA256:          "sha-race",
	}
	if err := store.CreateDocument(document); err != nil {
		t.Fatalf("create document: %v", err)
	}
	if _, _, err := store.DeleteDocument(document.DocumentID); err != nil {
		t.Fatalf("delete document: %v", err)
	}

	document.Status = "indexed"
	document.UpdatedAt = time.Now().UTC()
	if err := store.UpdateDocument(document); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after concurrent delete, got %v", err)
	}
	if _, err := store.GetDocument(document.DocumentID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected document to stay deleted, got %v", err)
	}
}

func TestJSONStoreListChunksPage(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	store, _, err := NewJSONStore(path, InitAccount{})
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
