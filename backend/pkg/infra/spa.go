package infra

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

const defaultSPARoot = "target/spa"

// SPA describes one discovered SPA and its HTTP mount path.
type SPA struct {
	// Path is the HTTP mount path, including the configured base path.
	Path string
	// WebDir is the directory containing the SPA's index.html and assets.
	WebDir string
}

// ScanSPAs discovers direct children of target/spa that contain an index.html.
// A directory name becomes its HTTP path segment; for example, target/spa/ui maps to /ui.
func ScanSPAs(basePath string) ([]SPA, error) {
	var (
		entries []os.DirEntry
		spas    []SPA
		webDir  string
		info    os.FileInfo
		err     error
	)

	entries, err = os.ReadDir(defaultSPARoot)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read SPA root: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		webDir = filepath.Join(defaultSPARoot, entry.Name())
		info, err = os.Stat(filepath.Join(webDir, "index.html"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("stat SPA index %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		spas = append(spas, SPA{
			Path:   path.Join("/", basePath, entry.Name()),
			WebDir: webDir,
		})
	}
	return spas, nil
}

// GinSPA serves one SPA at spaPath from webDir.
// It returns true when the request belongs to the SPA and has been handled.
func GinSPA(c *gin.Context, spaPath, webDir string) bool {
	var (
		requestPath string
		relPath     string
		assetPath   string
	)

	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		return false
	}
	if !isRegularFile(filepath.Join(webDir, "index.html")) {
		return false
	}

	spaPath = path.Clean(spaPath)
	requestPath = c.Request.URL.Path
	if requestPath == spaPath {
		c.Redirect(http.StatusMovedPermanently, spaPath+"/")
		return true
	}
	if requestPath != spaPath+"/" && !strings.HasPrefix(requestPath, spaPath+"/") {
		return false
	}

	relPath = strings.TrimPrefix(requestPath, spaPath+"/")
	if relPath != "" && filepath.IsLocal(filepath.FromSlash(relPath)) {
		assetPath = filepath.Join(webDir, filepath.FromSlash(relPath))
		if isRegularFile(assetPath) {
			setSPACacheControl(c, relPath)
			c.File(assetPath)
			return true
		}
	}

	c.Header("Cache-Control", "no-store")
	c.File(filepath.Join(webDir, "index.html"))
	return true
}

func isRegularFile(path string) bool {
	var (
		info os.FileInfo
		err  error
	)

	info, err = os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func setSPACacheControl(c *gin.Context, relPath string) {
	switch {
	case relPath == "index.html", relPath == "app.json":
		c.Header("Cache-Control", "no-store")
	case strings.HasPrefix(relPath, "assets/"):
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	}
}
