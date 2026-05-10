package webui

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed all:dashboard-dist
var embeddedRaw embed.FS

// Mount registers a NoRoute handler that serves the Vite dashboard build
// (same origin as /api and /ws). API paths must stay registered before this.
func Mount(r *gin.Engine) {
	root, err := fs.Sub(embeddedRaw, "dashboard-dist")
	if err != nil {
		return
	}

	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.AbortWithStatus(http.StatusMethodNotAllowed)
			return
		}
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api") || strings.HasPrefix(p, "/ws") {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		rel := strings.TrimPrefix(p, "/")
		if rel == "" {
			rel = "index.html"
		}

		b, err := fs.ReadFile(root, rel)
		if err != nil {
			b, err = fs.ReadFile(root, "index.html")
			if err != nil {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", b)
			return
		}

		ct := mime.TypeByExtension(filepath.Ext(rel))
		if ct == "" {
			ct = "application/octet-stream"
		}
		if c.Request.Method == http.MethodHead {
			c.Status(http.StatusOK)
			c.Header("Content-Type", ct)
			return
		}
		c.Data(http.StatusOK, ct, b)
	})
}
