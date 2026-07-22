package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTargetUIServesDirectorySPAsWithoutDefaultApp(t *testing.T) {
	var (
		previousWD string
		tmpDir     string
		spaRoot    string
		handler    http.Handler
		err        error
		tests      []struct {
			name     string
			path     string
			wantCode int
			wantBody string
		}
	)

	previousWD, err = os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	tmpDir = t.TempDir()
	err = os.Chdir(tmpDir)
	if err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	spaRoot = filepath.Join(tmpDir, "target", "spa")
	writeAPIUIFile(t, spaRoot, "h5/index.html", "<html>h5 app</html>")
	writeAPIUIFile(t, spaRoot, "web/index.html", "<html>web app</html>")

	handler = NewHandler(testConfig(tmpDir), nil, nil).Routes()
	tests = []struct {
		name     string
		path     string
		wantCode int
		wantBody string
	}{
		{name: "h5 app", path: "/h5/home", wantCode: http.StatusOK, wantBody: "<html>h5 app</html>"},
		{name: "web app", path: "/web/login", wantCode: http.StatusOK, wantBody: "<html>web app</html>"},
		{name: "no default app", path: "/", wantCode: http.StatusNotFound, wantBody: "route not found"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				req *http.Request
				rec *httptest.ResponseRecorder
			)

			req = httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec = httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Errorf("status=%d, want=%d, body=%s", rec.Code, tc.wantCode, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("body=%q, want body containing %q", rec.Body.String(), tc.wantBody)
			}
		})
	}
}

func writeAPIUIFile(t *testing.T, root, name, contents string) {
	var (
		path string
		err  error
	)

	t.Helper()
	path = filepath.Join(root, filepath.FromSlash(name))
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("create UI directory: %v", err)
	}
	err = os.WriteFile(path, []byte(contents), 0o644)
	if err != nil {
		t.Fatalf("write UI file: %v", err)
	}
}
