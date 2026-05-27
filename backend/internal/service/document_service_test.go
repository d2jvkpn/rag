package service

import (
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"backend/internal/repository"
)

func testConfig(tmpDir string) *viper.Viper {
	v := viper.New()
	v.Set("app.data_dir", filepath.Join(tmpDir, "data"))
	v.Set("app.state_path", filepath.Join(tmpDir, "data", "app-state.json"))
	v.Set("admin.username", "admin")
	v.Set("admin.password", "admin123")
	return v
}

func TestCreateDocumentDuplicateDoesNotLeaveFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	v := testConfig(tmpDir)

	store, err := repository.NewJSONStore(v.GetString("app.state_path"), []repository.AccountSeed{{Username: "admin", Password: "admin123"}})
	if err != nil {
		t.Fatalf("init store: %v", err)
	}

	documentService, err := NewDocumentService(v, store)
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

	store, err := repository.NewJSONStore(v.GetString("app.state_path"), []repository.AccountSeed{{Username: "admin", Password: "admin123"}})
	if err != nil {
		t.Fatalf("init store: %v", err)
	}

	documentService, err := NewDocumentService(v, store)
	if err != nil {
		t.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()

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
