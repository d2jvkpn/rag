package infra

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

func GinSPA(c *gin.Context, uiPath, webDir string) bool {
	var (
		requestPath string
		relPath     string
	)

	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		return false
	}

	requestPath = c.Request.URL.Path
	if requestPath == uiPath {
		c.Redirect(http.StatusMovedPermanently, uiPath+"/")
		return true
	}
	if requestPath != uiPath+"/" && !strings.HasPrefix(requestPath, uiPath+"/") {
		return false
	}

	relPath = strings.TrimPrefix(requestPath, uiPath+"/")
	if relPath != "" {
		assetPath := filepath.Join(webDir, filepath.FromSlash(relPath))
		if info, err := os.Stat(assetPath); err == nil && !info.IsDir() {
			switch {
			case relPath == "app.json":
				c.Header("Cache-Control", "no-store")
			case strings.HasPrefix(relPath, "assets/"):
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			}
			c.File(assetPath)
			return true
		}
	}

	c.Header("Cache-Control", "no-store")
	c.File(filepath.Join(webDir, "index.html"))
	return true
}
