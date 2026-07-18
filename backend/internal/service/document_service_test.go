package service

import (
	"context"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/d2jvkpn/rag/backend/internal/model"
	"github.com/d2jvkpn/rag/backend/pkg/rag"
	"github.com/d2jvkpn/rag/backend/internal/repository"
)

func testConfig(tmpDir string) *viper.Viper {
	v := viper.New()
	v.Set("app.data_dir", filepath.Join(tmpDir, "data"))
	v.Set("app.state_path", filepath.Join(tmpDir, "data", "app-state.json"))
	v.Set("embedder.dim", 1536)
	return v
}

func TestCreateDocumentDuplicateDoesNotLeaveFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	v := testConfig(tmpDir)

	store, _, err := repository.NewJSONStore(
		v.GetString("app.state_path"),
		repository.InitAccount{Username: "admin", Password: "admin123"},
	)
	if err != nil {
		t.Fatalf("init store: %v", err)
	}

	documentService, err := NewDocumentService(v, store)
	if err != nil {
		t.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()
	createKnowledgeBaseForServiceTest(t, documentService, "kb-1")

	first, err := documentService.CreateDocument(
		newMultipartFile(t, "same content"),
		&multipart.FileHeader{Filename: "sample.md"},
		"kb-1",
		"Sample",
		nil,
		false,
		"user-1",
		"testuser",
	)
	if err != nil {
		t.Fatalf("create first document: %v", err)
	}

	second, err := documentService.CreateDocument(
		newMultipartFile(t, "same content"),
		&multipart.FileHeader{Filename: "sample.md"},
		"kb-1",
		"Sample",
		nil,
		false,
		"user-1",
		"testuser",
	)
	if err == nil {
		t.Fatalf("expected duplicate upload error, got second document %+v", second)
	}

	firstDir := filepath.Dir(first.StoragePath)
	if _, err := os.Stat(firstDir); err != nil {
		t.Fatalf("first document dir should exist: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(firstDir))
	if err != nil {
		t.Fatalf("read document date dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 document dir after duplicate upload, got %d", len(entries))
	}
	wantDirName := first.CreatedAt.UTC().Format("2006-01-02") + "_" + first.DocumentID
	if entries[0].Name() != wantDirName {
		t.Fatalf("expected only first document dir %s, got %s", wantDirName, entries[0].Name())
	}
}

func TestCreateDocumentWithoutHumanReviewAutoApprovesAndIndexes(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	v := testConfig(tmpDir)

	store, _, err := repository.NewJSONStore(
		v.GetString("app.state_path"),
		repository.InitAccount{Username: "admin", Password: "admin123"},
	)
	if err != nil {
		t.Fatalf("init store: %v", err)
	}

	documentService, err := NewDocumentService(v, store)
	if err != nil {
		t.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()
	createKnowledgeBaseForServiceTest(t, documentService, "kb-1")

	document, err := documentService.CreateDocument(
		newMultipartFile(t, "# Title\n\nhello rag"),
		&multipart.FileHeader{Filename: "sample.md"},
		"kb-1",
		"Sample",
		nil,
		false,
		"user-1",
		"testuser",
	)
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	waitForServiceTest(t, 2*time.Second, func() bool {
		current, err := store.GetDocument(document.DocumentID)
		if err != nil {
			t.Fatalf("get document: %v", err)
		}
		return current.Status == "indexed"
	})

	current, err := store.GetDocument(document.DocumentID)
	if err != nil {
		t.Fatalf("get indexed document: %v", err)
	}
	if current.Stage != "done" {
		t.Fatalf("expected stage done, got %s", current.Stage)
	}

	chunks, err := store.GetChunks(document.DocumentID)
	if err != nil {
		t.Fatalf("get chunks: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	for _, chunk := range chunks {
		if chunk.Status != "approved" {
			t.Fatalf("expected approved chunk, got %s", chunk.Status)
		}
	}
}

func createKnowledgeBaseForServiceTest(t *testing.T, svc *DocumentService, kbID string) {
	t.Helper()
	if _, err := svc.CreateKnowledgeBase(
		kbID,
		"chinese",
		DefaultChunkSize,
		DefaultChunkOverlap,
		DefaultMinChunks,
		"testuser",
	); err != nil {
		t.Fatalf("create knowledge base: %v", err)
	}
}

func TestCreateKnowledgeBaseUsesEmbedderConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	v := testConfig(tmpDir)
	v.Set("embedder.dim", 768)
	v.Set("embedder.model", "text-embedding-v4")
	store, _, err := repository.NewJSONStore(
		v.GetString("app.state_path"),
		repository.InitAccount{Username: "admin", Password: "admin123"},
	)
	if err != nil {
		t.Fatalf("init store: %v", err)
	}
	documentService, err := NewDocumentService(v, store)
	if err != nil {
		t.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()

	kb, err := documentService.CreateKnowledgeBase(
		"kb-dim",
		"chinese",
		DefaultChunkSize,
		DefaultChunkOverlap,
		DefaultMinChunks,
		"testuser",
	)
	if err != nil {
		t.Fatalf("create knowledge base: %v", err)
	}
	if kb.Dim != 768 {
		t.Fatalf("expected embedder.dim 768, got %d", kb.Dim)
	}
	if kb.Model != "text-embedding-v4" {
		t.Fatalf("expected embedder.model text-embedding-v4, got %s", kb.Model)
	}
	if got := documentService.DefaultKnowledgeBaseDim(); got != 768 {
		t.Fatalf("expected default dim 768, got %d", got)
	}
	if got := documentService.DefaultKnowledgeBaseModel(); got != "text-embedding-v4" {
		t.Fatalf("expected default model text-embedding-v4, got %s", got)
	}
}

func TestNewDocumentServiceHydratesKnowledgeBasesIntoVectorStore(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	v := testConfig(tmpDir)
	store, _, err := repository.NewJSONStore(
		v.GetString("app.state_path"),
		repository.InitAccount{Username: "admin", Password: "admin123"},
	)
	if err != nil {
		t.Fatalf("init store: %v", err)
	}
	now := time.Now().UTC()
	if err := store.CreateKnowledgeBase(model.KnowledgeBase{
		KnowledgeBaseID: "kb-1",
		CreatedAt:       now,
		UpdatedAt:       now,
		Dim:             1536,
		Analyzer:        "chinese",
		ChunkSize:       DefaultChunkSize,
		ChunkOverlap:    DefaultChunkOverlap,
		MinChunks:       DefaultMinChunks,
	}); err != nil {
		t.Fatalf("seed knowledge base: %v", err)
	}
	vs := &trackingVectorStore{}
	documentService, err := NewDocumentService(v, store, WithVectorStore(vs))
	if err != nil {
		t.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()
	if len(vs.created) != 1 || vs.created[0].Name != "kb-1" {
		t.Fatalf("expected kb-1 to be hydrated, got %+v", vs.created)
	}
}

func newMultipartFile(t *testing.T, content string) multipart.File {
	t.Helper()
	return &memoryFile{Reader: strings.NewReader(content)}
}

func waitForServiceTest(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

type memoryFile struct {
	*strings.Reader
}

func (f *memoryFile) Close() error {
	return nil
}

func (f *memoryFile) ReadAt(p []byte, off int64) (n int, err error) {
	reader := strings.NewReader(f.String())
	return reader.ReadAt(p, off)
}

func (f *memoryFile) Seek(offset int64, whence int) (int64, error) {
	return f.Reader.Seek(offset, whence)
}

func (f *memoryFile) String() string {
	if seeker, ok := interface{}(f.Reader).(io.Seeker); ok {
		current, _ := seeker.Seek(0, io.SeekCurrent)
		_, _ = seeker.Seek(0, io.SeekStart)
		raw, _ := io.ReadAll(f.Reader)
		_, _ = seeker.Seek(current, io.SeekStart)
		return string(raw)
	}
	return ""
}

type trackingVectorStore struct {
	rag.NoopVectorStore
	created []rag.CollectionConfig
}

func (s *trackingVectorStore) CreateKnowledgeBase(_ context.Context, cfg rag.CollectionConfig) error {
	s.created = append(s.created, cfg)
	return nil
}
