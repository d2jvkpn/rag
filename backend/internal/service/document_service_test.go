package service

import (
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/config"
	"backend/internal/repository"
)

func TestCreateDocumentDuplicateDoesNotLeaveFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfg := config.Config{
		DataDir:       filepath.Join(tmpDir, "data"),
		StatePath:     filepath.Join(tmpDir, "data", "app-state.json"),
		SessionCookie: "rag_session",
		AdminUsername: "admin",
		AdminPassword: "admin123",
	}

	store, err := repository.NewJSONStore(cfg.StatePath, cfg.AdminUsername, cfg.AdminPassword)
	if err != nil {
		t.Fatalf("init store: %v", err)
	}

	documentService, err := NewDocumentService(cfg, store)
	if err != nil {
		t.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()

	first, err := documentService.CreateDocument(
		newMultipartFile(t, "same content"),
		&multipart.FileHeader{Filename: "sample.md"},
		"kb-1",
		"Sample",
		nil,
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
	)
	if err == nil {
		t.Fatalf("expected duplicate upload error, got second document %+v", second)
	}

	documentsDir := filepath.Join(cfg.DataDir, "documents", "kb-1")
	entries, err := os.ReadDir(documentsDir)
	if err != nil {
		t.Fatalf("read documents dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 document dir after duplicate upload, got %d", len(entries))
	}
	if entries[0].Name() != first.DocumentID {
		t.Fatalf("expected only first document dir %s, got %s", first.DocumentID, entries[0].Name())
	}
}

func newMultipartFile(t *testing.T, content string) multipart.File {
	t.Helper()
	return &memoryFile{Reader: strings.NewReader(content)}
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
