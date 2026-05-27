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

	"github.com/spf13/viper"

	"backend/internal/repository"
	"backend/internal/service"
)

func testConfig(tmpDir string) *viper.Viper {
	v := viper.New()
	v.Set("app.data_dir", filepath.Join(tmpDir, "data"))
	v.Set("app.state_path", filepath.Join(tmpDir, "data", "app-state.json"))
	v.Set("http.session_cookie", "rag_session")
	v.Set("admin.username", "admin")
	v.Set("admin.password", "admin123")
	return v
}

func TestDocumentLifecycle(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	v := testConfig(tmpDir)

	accounts := []repository.AccountSeed{{Username: "admin", Password: "admin123", Permissions: []string{"view_user_list", "delete_documents", "disable_users"}}}
	store, err := repository.NewJSONStore(v.GetString("app.state_path"), accounts)
	if err != nil {
		t.Fatalf("init store: %v", err)
	}

	documentService, err := service.NewDocumentService(v, store)
	if err != nil {
		t.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()

	authService := service.NewAuthService(store, "test-secret", 0, accounts)
	handler := NewHandler(v, authService, documentService).Routes()

	sessionCookie := loginForTest(t, handler, "admin", "admin123")
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

	currentDocument, err := store.GetDocument(documentID)
	if err != nil {
		t.Fatalf("get document before delete: %v", err)
	}
	documentDir := filepath.Dir(currentDocument.StoragePath)
	if _, err := os.Stat(filepath.Join(documentDir, "chunks-v2.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no chunk snapshot before indexing, got err=%v", err)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/documents/"+documentID, nil)
	deleteReq.AddCookie(sessionCookie)
	deleteRec := httptest.NewRecorder()
	handler.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete document status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	waitForTest(t, time.Second, func() bool {
		_, err := os.Stat(documentDir)
		return os.IsNotExist(err)
	})
}

func TestAuthRequired(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	v := testConfig(tmpDir)

	accounts := []repository.AccountSeed{{Username: "admin", Password: "admin123"}}
	store, err := repository.NewJSONStore(v.GetString("app.state_path"), accounts)
	if err != nil {
		t.Fatalf("init store: %v", err)
	}
	documentService, err := service.NewDocumentService(v, store)
	if err != nil {
		t.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()

	authService := service.NewAuthService(store, "test-secret", 0, accounts)
	handler := NewHandler(v, authService, documentService).Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestUserListRequiresPermission(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	v := testConfig(tmpDir)
	accounts := []repository.AccountSeed{
		{Username: "admin", Password: "admin123", Permissions: []string{"view_user_list"}},
		{Username: "user1", Password: "user123"},
	}

	store, err := repository.NewJSONStore(v.GetString("app.state_path"), accounts)
	if err != nil {
		t.Fatalf("init store: %v", err)
	}
	documentService, err := service.NewDocumentService(v, store)
	if err != nil {
		t.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()

	authService := service.NewAuthService(store, "test-secret", 0, accounts)
	handler := NewHandler(v, authService, documentService).Routes()

	adminCookie := loginForTest(t, handler, "admin", "admin123")
	userCookie := loginForTest(t, handler, "user1", "user123")

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list users status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.AddCookie(userCookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user list users status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDocumentTagsEndpoint(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	v := testConfig(tmpDir)
	accounts := []repository.AccountSeed{{Username: "admin", Password: "admin123"}}

	store, err := repository.NewJSONStore(v.GetString("app.state_path"), accounts)
	if err != nil {
		t.Fatalf("init store: %v", err)
	}
	documentService, err := service.NewDocumentService(v, store)
	if err != nil {
		t.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()

	authService := service.NewAuthService(store, "test-secret", 0, accounts)
	handler := NewHandler(v, authService, documentService).Routes()
	sessionCookie := loginForTest(t, handler, "admin", "admin123")

	createMarkdownDocumentForTestWithTagsAndContent(t, handler, sessionCookie, "kb-1", []string{"faq", "policy"}, "# One\n\nfaq")
	createMarkdownDocumentForTestWithTagsAndContent(t, handler, sessionCookie, "kb-1", []string{"faq"}, "# Two\n\npolicy")
	createMarkdownDocumentForTestWithTagsAndContent(t, handler, sessionCookie, "kb-2", []string{"guide"}, "# Three\n\nguide")

	req := httptest.NewRequest(http.MethodGet, "/api/document-tags?knowledge_base_id=kb-1", nil)
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list document tags status=%d body=%s", rec.Code, rec.Body.String())
	}

	var response struct {
		Data struct {
			Items []struct {
				Tag   string `json:"tag"`
				Count int    `json:"count"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode tags response: %v", err)
	}
	if len(response.Data.Items) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(response.Data.Items))
	}
	if response.Data.Items[0].Tag != "faq" || response.Data.Items[0].Count != 2 {
		t.Fatalf("expected faq count 2 first, got %+v", response.Data.Items[0])
	}
}

func TestDisableUserBlocksFurtherRequests(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	v := testConfig(tmpDir)
	accounts := []repository.AccountSeed{
		{Username: "admin", Password: "admin123", Permissions: []string{"disable_users", "view_user_list"}},
		{Username: "user1", Password: "user123"},
	}

	store, err := repository.NewJSONStore(v.GetString("app.state_path"), accounts)
	if err != nil {
		t.Fatalf("init store: %v", err)
	}
	documentService, err := service.NewDocumentService(v, store)
	if err != nil {
		t.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()

	authService := service.NewAuthService(store, "test-secret", 0, accounts)
	handler := NewHandler(v, authService, documentService).Routes()

	adminCookie := loginForTest(t, handler, "admin", "admin123")
	userCookie := loginForTest(t, handler, "user1", "user123")

	user, err := store.FindUserByUsername("user1")
	if err != nil {
		t.Fatalf("find user1: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/users/"+user.UserID+"/disable", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable user status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(userCookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disabled user /me status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"user1","password":"user123"}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disabled user login status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDeleteDocumentPermissionOverridesOwnership(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	v := testConfig(tmpDir)
	accounts := []repository.AccountSeed{
		{Username: "admin", Password: "admin123", Permissions: []string{"delete_documents"}},
		{Username: "user1", Password: "user123"},
	}

	store, err := repository.NewJSONStore(v.GetString("app.state_path"), accounts)
	if err != nil {
		t.Fatalf("init store: %v", err)
	}
	documentService, err := service.NewDocumentService(v, store)
	if err != nil {
		t.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()

	authService := service.NewAuthService(store, "test-secret", 0, accounts)
	handler := NewHandler(v, authService, documentService).Routes()

	userCookie := loginForTest(t, handler, "user1", "user123")
	adminCookie := loginForTest(t, handler, "admin", "admin123")
	documentID := createMarkdownDocumentForTest(t, handler, userCookie)

	waitForTest(t, 2*time.Second, func() bool {
		req := httptest.NewRequest(http.MethodGet, "/api/documents/"+documentID, nil)
		req.AddCookie(userCookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			return false
		}
		var response struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode document response: %v", err)
		}
		return response.Data["status"] == "review_pending"
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/documents/"+documentID, nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin delete other user's doc status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func loginForTest(t *testing.T, handler http.Handler, username, password string) *http.Cookie {
	t.Helper()

	body := strings.NewReader(`{"username":"` + username + `","password":"` + password + `"}`)
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
	return createMarkdownDocumentForTestWithTagsAndContent(t, handler, sessionCookie, "kb-1", nil, "# Title\n\nhello rag")
}

func createMarkdownDocumentForTestWithTags(t *testing.T, handler http.Handler, sessionCookie *http.Cookie, knowledgeBaseID string, tags []string) string {
	t.Helper()
	return createMarkdownDocumentForTestWithTagsAndContent(t, handler, sessionCookie, knowledgeBaseID, tags, "# Title\n\nhello rag")
}

func createMarkdownDocumentForTestWithTagsAndContent(t *testing.T, handler http.Handler, sessionCookie *http.Cookie, knowledgeBaseID string, tags []string, content string) string {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("knowledge_base_id", knowledgeBaseID); err != nil {
		t.Fatalf("write knowledge base field: %v", err)
	}
	if err := writer.WriteField("title", "Sample"); err != nil {
		t.Fatalf("write title field: %v", err)
	}
	if err := writer.WriteField("human_review", "true"); err != nil {
		t.Fatalf("write human_review field: %v", err)
	}
	for _, tag := range tags {
		if err := writer.WriteField("tags", tag); err != nil {
			t.Fatalf("write tag field: %v", err)
		}
	}
	fileWriter, err := writer.CreateFormFile("file", "sample.md")
	if err != nil {
		t.Fatalf("create file field: %v", err)
	}
	if _, err := fileWriter.Write([]byte(content)); err != nil {
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
