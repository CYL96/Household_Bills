package api_test

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"household-bills/backend/internal/api"
	"household-bills/backend/internal/config"
)

func TestSPAFallbackAndAPI404(t *testing.T) {
	dist := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dist, "assets"), 0o755); err != nil {
		t.Fatalf("创建静态资源目录失败: %v", err)
	}
	const indexHTML = "<!doctype html><html><body>household-bills-spa</body></html>"
	const appJS = "console.log('household-bills-asset')"
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(indexHTML), 0o644); err != nil {
		t.Fatalf("写入 index.html 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dist, "assets", "app.js"), []byte(appJS), 0o644); err != nil {
		t.Fatalf("写入静态资源失败: %v", err)
	}

	handler := api.New(config.Config{FrontendDist: dist, CookieSecure: false},
		nil, log.New(io.Discard, "", 0))
	server := httptest.NewServer(handler)
	defer server.Close()

	read := func(path string) (int, string, string) {
		t.Helper()
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("请求 %s 失败: %v", path, err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", path, err)
		}
		return response.StatusCode, response.Header.Get("Content-Type"), string(body)
	}

	status, contentType, body := read("/transactions")
	if status != http.StatusOK || body != indexHTML || !strings.Contains(contentType, "text/html") {
		t.Fatalf("SPA 路由未回退 index.html: status=%d content-type=%q body=%q",
			status, contentType, body)
	}
	status, _, body = read("/assets/app.js")
	if status != http.StatusOK || body != appJS {
		t.Fatalf("静态资源未正确返回: status=%d body=%q", status, body)
	}
	status, contentType, body = read("/api/no-such-endpoint/")
	if status != http.StatusNotFound || !strings.Contains(contentType, "application/json") ||
		strings.Contains(body, "household-bills-spa") {
		t.Fatalf("未知 API 路径不应回退 SPA: status=%d content-type=%q body=%q",
			status, contentType, body)
	}
	status, _, _ = read("/assets/missing.js")
	if status != http.StatusNotFound {
		t.Fatalf("缺失静态资源应返回 404，实际 %d", status)
	}
}
