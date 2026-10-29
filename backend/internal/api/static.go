package api

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

func isAPIPath(requestPath string) bool {
	return requestPath == "/api" || strings.HasPrefix(requestPath, "/api/")
}

func (a *API) handleNoRoute(c *gin.Context) {
	if isAPIPath(c.Request.URL.Path) {
		abortDetail(c, http.StatusNotFound, "接口不存在。")
		return
	}
	a.serveSPA(c)
}

func (a *API) handleNoMethod(c *gin.Context) {
	if isAPIPath(c.Request.URL.Path) {
		abortDetail(c, http.StatusMethodNotAllowed, "请求方法不允许。")
		return
	}
	abortDetail(c, http.StatusNotFound, "资源不存在。")
}

func (a *API) serveSPA(c *gin.Context) {
	request := c.Request
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		abortDetail(c, http.StatusNotFound, "资源不存在。")
		return
	}

	dist := strings.TrimSpace(a.cfg.FrontendDist)
	if dist == "" {
		abortDetail(c, http.StatusServiceUnavailable, "前端资源尚未构建。")
		return
	}
	root, err := filepath.Abs(dist)
	if err != nil {
		abortDetail(c, http.StatusServiceUnavailable, "前端资源路径无效。")
		return
	}

	if candidate, ok := safeDistPath(root, request.URL.Path); ok {
		if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() {
			a.serveFile(c, candidate, info, isAssetPath(request.URL.Path))
			return
		}
	}

	if isAssetPath(request.URL.Path) {
		abortDetail(c, http.StatusNotFound, "资源不存在。")
		return
	}

	indexPath := filepath.Join(root, "index.html")
	info, err := os.Stat(indexPath)
	if err != nil || !info.Mode().IsRegular() {
		abortDetail(c, http.StatusServiceUnavailable, "前端资源尚未构建。")
		return
	}
	a.serveFile(c, indexPath, info, false)
}

func (a *API) serveFile(c *gin.Context, filename string, info os.FileInfo, immutable bool) {
	file, err := os.Open(filename)
	if err != nil {
		abortDetail(c, http.StatusNotFound, "资源不存在。")
		return
	}
	defer file.Close()

	c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
	if immutable {
		c.Writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		c.Writer.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(c.Writer, c.Request, filepath.Base(filename), info.ModTime(), file)
}

func safeDistPath(root, requestPath string) (string, bool) {
	cleaned := path.Clean("/" + strings.TrimPrefix(requestPath, "/"))
	if cleaned == "/" {
		return root, true
	}
	relative := strings.TrimPrefix(cleaned, "/")
	for _, part := range strings.Split(relative, "/") {
		if part == "" || part == ".." || strings.HasPrefix(part, ".") {
			return "", false
		}
	}
	candidate := filepath.Join(root, filepath.FromSlash(relative))
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return candidate, true
}

func isAssetPath(requestPath string) bool {
	if strings.HasPrefix(requestPath, "/assets/") {
		return true
	}
	base := path.Base(requestPath)
	return strings.Contains(base, ".")
}
