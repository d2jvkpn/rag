package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/repository"
	"backend/internal/service"
)

func TestDocumentLifecycle(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfg := config.Config{
		Release:       false,
		ConfigPath:    filepath.Join(tmpDir, "local.yaml"),
		HTTPAddr:      ":0",
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

	documentService, err := service.NewDocumentService(cfg, store)
	if err != nil {
		t.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()

	authService := service.NewAuthService(store, "test-secret")
	handler := NewHandler(cfg, authService, documentService).Routes()

	sessionCookie := loginForTest(t, handler)
	documentID := createMarkdownDocumentForTest(t, handler, sessionCookie)

	var documentResponse struct {
		Data map[string]any `json:"data"`
	}
	waitForTest(t, 2*time.Second, func() bool {
		req := httptest.NewRequest(http.MethodGet, "/api/documents/"+documentID, nil)
		req.AddCookie(sessionCookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			return false
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &documentResponse); err != nil {
			t.Fatalf("decode document response: %v", err)
		}
		return documentResponse.Data["status"] == "review_pending"
	})

	if got := documentResponse.Data["stage"]; got != "done" {
		t.Fatalf("expected stage done, got %v", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/documents/"+documentID+"/chunks", nil)
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get chunks status=%d body=%s", rec.Code, rec.Body.String())
	}

	var chunksResponse struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &chunksResponse); err != nil {
		t.Fatalf("decode chunks response: %v", err)
	}
	if len(chunksResponse.Data.Items) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunksResponse.Data.Items))
	}

	rechunkReq := httptest.NewRequest(http.MethodPost, "/api/documents/"+documentID+"/chunks/rechunk", nil)
	rechunkReq.AddCookie(sessionCookie)
	rechunkRec := httptest.NewRecorder()
	handler.ServeHTTP(rechunkRec, rechunkReq)
	if rechunkRec.Code != http.StatusAccepted {
		t.Fatalf("rechunk status=%d body=%s", rechunkRec.Code, rechunkRec.Body.String())
	}

	waitForTest(t, 2*time.Second, func() bool {
		req := httptest.NewRequest(http.MethodGet, "/api/documents/"+documentID, nil)
		req.AddCookie(sessionCookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			return false
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &documentResponse); err != nil {
			t.Fatalf("decode document response after rechunk: %v", err)
		}
		version, ok := documentResponse.Data["chunk_version"].(float64)
		return ok && int(version) == 2 && documentResponse.Data["status"] == "review_pending"
	})

	if _, err := os.Stat(filepath.Join(cfg.DataDir, "chunks", "kb-1", documentID, "chunks-v2.json")); err != nil {
		t.Fatalf("expected chunk snapshot v2: %v", err)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/documents/"+documentID, nil)
	deleteReq.AddCookie(sessionCookie)
	deleteRec := httptest.NewRecorder()
	handler.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete document status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	waitForTest(t, time.Second, func() bool {
		_, err := os.Stat(filepath.Join(cfg.DataDir, "documents", "kb-1", documentID))
		return os.IsNotExist(err)
	})
}

func TestAuthRequired(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfg := config.Config{
		HTTPAddr:      ":0",
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
	documentService, err := service.NewDocumentService(cfg, store)
	if err != nil {
		t.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()

	authService := service.NewAuthService(store, "test-secret")
	handler := NewHandler(cfg, authService, documentService).Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func loginForTest(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()

	body := strings.NewReader(`{"username":"admin","password":"admin123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}

	res := rec.Result()
	cookies := res.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}
	return cookies[0]
}

func createMarkdownDocumentForTest(t *testing.T, handler http.Handler, sessionCookie *http.Cookie) string {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("knowledge_base_id", "kb-1"); err != nil {
		t.Fatalf("write knowledge base field: %v", err)
	}
	if err := writer.WriteField("title", "Sample"); err != nil {
		t.Fatalf("write title field: %v", err)
	}
	fileWriter, err := writer.CreateFormFile("file", "sample.md")
	if err != nil {
		t.Fatalf("create file field: %v", err)
	}
	if _, err := fileWriter.Write([]byte("# Title\n\nhello rag")); err != nil {
		t.Fatalf("write file content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/documents", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("create document status=%d body=%s", rec.Code, rec.Body.String())
	}

	var response struct {
		Data struct {
			DocumentID string `json:"document_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode create document response: %v", err)
	}
	if response.Data.DocumentID == "" {
		t.Fatal("expected document_id")
	}
	return response.Data.DocumentID
}

func waitForTest(t *testing.T, timeout time.Duration, fn func() bool) {
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
