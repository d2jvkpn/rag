package infra

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestScanSPAsAndGinSPA(t *testing.T) {
	var (
		rootDir      string
		spaRoot      string
		spas         []SPA
		prefixedSPAs []SPA
		router       *gin.Engine
		err          error
		tests        []struct {
			name         string
			method       string
			path         string
			wantCode     int
			wantBody     string
			wantLocation string
			wantCache    string
		}
	)

	rootDir = useSPAWorkingDir(t)
	spaRoot = filepath.Join(rootDir, "target", "spa")
	writeSPAFile(t, spaRoot, "h5/index.html", "<html>h5</html>")
	writeSPAFile(t, spaRoot, "h5/app.json", `{"app":"h5"}`)
	writeSPAFile(t, spaRoot, "h5/assets/app.js", "console.log('h5')")
	writeSPAFile(t, spaRoot, "ui/index.html", "<html>ui</html>")
	writeSPAFile(t, spaRoot, "web/index.html", "<html>web</html>")
	writeSPAFile(t, spaRoot, "ignored/readme.txt", "not an SPA")

	spas, err = ScanSPAs("")
	if err != nil {
		t.Fatalf("scan SPAs: %v", err)
	}
	if len(spas) != 3 {
		t.Fatalf("SPA count=%d, want=3", len(spas))
	}
	if spas[0].Path != "/h5" || spas[1].Path != "/ui" || spas[2].Path != "/web" {
		t.Fatalf("unexpected SPA paths: %+v", spas)
	}

	prefixedSPAs, err = ScanSPAs("/rag")
	if err != nil {
		t.Fatalf("scan prefixed SPAs: %v", err)
	}
	if prefixedSPAs[1].Path != "/rag/ui" {
		t.Fatalf("prefixed UI path=%q, want=/rag/ui", prefixedSPAs[1].Path)
	}

	router = gin.New()
	router.NoRoute(func(c *gin.Context) {
		for _, spa := range spas {
			if GinSPA(c, spa.Path, spa.WebDir) {
				return
			}
		}
		c.String(http.StatusNotFound, "not found")
	})

	tests = []struct {
		name         string
		method       string
		path         string
		wantCode     int
		wantBody     string
		wantLocation string
		wantCache    string
	}{
		{name: "app root redirects", method: http.MethodGet, path: "/h5", wantCode: http.StatusMovedPermanently,
			wantLocation: "/h5/"},
		{name: "h5 root", method: http.MethodGet, path: "/h5/", wantCode: http.StatusOK,
			wantBody: "<html>h5</html>", wantCache: "no-store"},
		{name: "h5 route fallback", method: http.MethodGet, path: "/h5/orders/1", wantCode: http.StatusOK,
			wantBody: "<html>h5</html>", wantCache: "no-store"},
		{name: "h5 config", method: http.MethodGet, path: "/h5/app.json", wantCode: http.StatusOK,
			wantBody: `{"app":"h5"}`, wantCache: "no-store"},
		{name: "h5 asset", method: http.MethodGet, path: "/h5/assets/app.js", wantCode: http.StatusOK,
			wantBody: "console.log('h5')", wantCache: "public, max-age=31536000, immutable"},
		{name: "ui route fallback", method: http.MethodGet, path: "/ui/documents", wantCode: http.StatusOK,
			wantBody: "<html>ui</html>", wantCache: "no-store"},
		{name: "web route fallback", method: http.MethodGet, path: "/web/settings", wantCode: http.StatusOK,
			wantBody: "<html>web</html>", wantCache: "no-store"},
		{name: "unknown app", method: http.MethodGet, path: "/admin/", wantCode: http.StatusNotFound,
			wantBody: "not found"},
		{name: "unsupported method", method: http.MethodPost, path: "/h5/", wantCode: http.StatusNotFound,
			wantBody: "not found"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				req *http.Request
				rec *httptest.ResponseRecorder
			)

			req = httptest.NewRequest(tc.method, tc.path, nil)
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Errorf("status=%d, want=%d, body=%s", rec.Code, tc.wantCode, rec.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("body=%q, want body containing %q", rec.Body.String(), tc.wantBody)
			}
			if rec.Header().Get("Location") != tc.wantLocation {
				t.Errorf("location=%q, want=%q", rec.Header().Get("Location"), tc.wantLocation)
			}
			if rec.Header().Get("Cache-Control") != tc.wantCache {
				t.Errorf("cache-control=%q, want=%q", rec.Header().Get("Cache-Control"), tc.wantCache)
			}
		})
	}
}

func TestScanSPAsAllowsMissingRoot(t *testing.T) {
	var (
		spas []SPA
		err  error
	)

	useSPAWorkingDir(t)
	spas, err = ScanSPAs("")
	if err != nil {
		t.Fatalf("scan missing SPA root: %v", err)
	}
	if len(spas) != 0 {
		t.Errorf("SPA count=%d, want=0", len(spas))
	}
}

func useSPAWorkingDir(t *testing.T) string {
	var (
		previousDir string
		rootDir     string
		err         error
	)

	t.Helper()
	previousDir, err = os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	rootDir = t.TempDir()
	err = os.Chdir(rootDir)
	if err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	return rootDir
}

func writeSPAFile(t *testing.T, root, name, contents string) {
	var (
		filePath string
		err      error
	)

	t.Helper()
	filePath = filepath.Join(root, filepath.FromSlash(name))
	err = os.MkdirAll(filepath.Dir(filePath), 0o755)
	if err != nil {
		t.Fatalf("create SPA directory: %v", err)
	}
	err = os.WriteFile(filePath, []byte(contents), 0o644)
	if err != nil {
		t.Fatalf("write SPA file: %v", err)
	}
}
