package api_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

const testUpdateVersionName = "1.0.2"

func newUpdateServer(t *testing.T, releaseDir, frontendDist string) *httptest.Server {
	t.Helper()
	handler := api.New(config.Config{
		AndroidReleaseDir: releaseDir,
		FrontendDist:      frontendDist,
		CookieSecure:      false,
	}, nil, log.New(io.Discard, "", 0))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func updateRequest(t *testing.T, method, url string) (int, http.Header, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("请求 %s 失败: %v", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	return response.StatusCode, response.Header, body
}

func writeTestAPK(t *testing.T, dir string, apk []byte) (string, string) {
	t.Helper()
	sum := sha256.Sum256(apk)
	sha := hex.EncodeToString(sum[:])
	filename := fmt.Sprintf("household-bills-3-%s.apk", sha)
	if err := os.WriteFile(filepath.Join(dir, filename), apk, 0o644); err != nil {
		t.Fatalf("写入测试 APK 失败: %v", err)
	}
	return sha, filename
}

func writeTestManifest(t *testing.T, dir string, manifest map[string]any) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("序列化测试清单失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.json"), data, 0o644); err != nil {
		t.Fatalf("写入测试清单失败: %v", err)
	}
}

func testManifest(sha, filename string, size int) map[string]any {
	return map[string]any{
		"version_code": 3,
		"version_name": testUpdateVersionName,
		"min_sdk":      26,
		"size_bytes":   size,
		"sha256":       sha,
		"apk_file":     filename,
	}
}

func TestAppUpdateUnavailable(t *testing.T) {
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("household-bills-spa"), 0o644); err != nil {
		t.Fatalf("写入 SPA 文件失败: %v", err)
	}

	for name, releaseDir := range map[string]string{
		"空发布目录":  t.TempDir(),
		"发布目录缺失": filepath.Join(t.TempDir(), "missing"),
	} {
		t.Run(name, func(t *testing.T) {
			server := newUpdateServer(t, releaseDir, dist)

			status, header, body := updateRequest(t, http.MethodGet, server.URL+"/api/app-update/")
			if status != http.StatusOK {
				t.Fatalf("未发布时应返回 200，实际 %d: %s", status, body)
			}
			if header.Get("Cache-Control") != "no-store" {
				t.Fatalf("更新查询必须 no-store，实际 %q", header.Get("Cache-Control"))
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("解析未发布响应失败: %v", err)
			}
			if payload["available"] != false || len(payload) != 1 {
				t.Fatalf("未发布响应应为 {available:false}，实际 %s", body)
			}

			for _, path := range []string{
				"/api/app-update/download/" + strings.Repeat("a", 64) + "/",
				"/api/app-update",
				"/api/app-update/unknown/",
			} {
				status, header, body := updateRequest(t, http.MethodGet, server.URL+path)
				if status != http.StatusNotFound || !strings.Contains(header.Get("Content-Type"), "application/json") {
					t.Fatalf("%s 应返回 JSON 404，实际 %d %q", path, status, header.Get("Content-Type"))
				}
				if strings.Contains(string(body), "household-bills-spa") {
					t.Fatalf("%s 不应回落 SPA", path)
				}
				if header.Get("Cache-Control") != "no-store" {
					t.Fatalf("%s 的 404 也应 no-store，实际 %q", path, header.Get("Cache-Control"))
				}
			}
		})
	}
}

func TestAppUpdateManifestAndDownload(t *testing.T) {
	releaseDir := t.TempDir()
	apk := []byte("PK\x03\x04-household-bills-apk-test-payload")
	sha, filename := writeTestAPK(t, releaseDir, apk)
	writeTestManifest(t, releaseDir, testManifest(sha, filename, len(apk)))
	server := newUpdateServer(t, releaseDir, "")

	status, header, body := updateRequest(t, http.MethodGet, server.URL+"/api/app-update/")
	if status != http.StatusOK {
		t.Fatalf("已发布时应返回 200，实际 %d: %s", status, body)
	}
	if header.Get("Cache-Control") != "no-store" {
		t.Fatalf("更新查询必须 no-store，实际 %q", header.Get("Cache-Control"))
	}
	var payload struct {
		Available    bool   `json:"available"`
		VersionCode  int    `json:"version_code"`
		VersionName  string `json:"version_name"`
		MinSDK       int    `json:"min_sdk"`
		SizeBytes    int    `json:"size_bytes"`
		SHA256       string `json:"sha256"`
		DownloadPath string `json:"download_path"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("解析更新响应失败: %v", err)
	}
	if !payload.Available || payload.VersionCode != 3 || payload.VersionName != testUpdateVersionName ||
		payload.MinSDK != 26 || payload.SizeBytes != len(apk) || payload.SHA256 != sha {
		t.Fatalf("更新元数据不符: %+v", payload)
	}
	if payload.DownloadPath != "/api/app-update/download/"+sha+"/" {
		t.Fatalf("download_path 不符: %q", payload.DownloadPath)
	}

	status, header, body = updateRequest(t, http.MethodGet, server.URL+payload.DownloadPath)
	if status != http.StatusOK || string(body) != string(apk) {
		t.Fatalf("APK 下载失败: status=%d body=%q", status, body)
	}
	if got := header.Get("Content-Type"); got != "application/vnd.android.package-archive" {
		t.Fatalf("APK Content-Type 不符: %q", got)
	}
	if got := header.Get("Content-Disposition"); !strings.Contains(got, "attachment") ||
		!strings.Contains(got, filename) {
		t.Fatalf("APK Content-Disposition 不符: %q", got)
	}
	if header.Get("Cache-Control") != "no-store" || header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("APK 下载缺少 no-store/nosniff: %v", header)
	}

	request, err := http.NewRequest(http.MethodGet, server.URL+payload.DownloadPath, nil)
	if err != nil {
		t.Fatalf("构造 Range 请求失败: %v", err)
	}
	request.Header.Set("Range", "bytes=2-5")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Range 请求失败: %v", err)
	}
	rangeBody, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatalf("读取 Range 响应失败: %v", err)
	}
	if response.StatusCode != http.StatusPartialContent || string(rangeBody) != string(apk[2:6]) {
		t.Fatalf("Range 下载不符: status=%d body=%q", response.StatusCode, rangeBody)
	}

	request, err = http.NewRequest(http.MethodGet, server.URL+payload.DownloadPath, nil)
	if err != nil {
		t.Fatalf("构造越界 Range 请求失败: %v", err)
	}
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-", len(apk)))
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("越界 Range 请求失败: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("越界 Range 应返回 416，实际 %d", response.StatusCode)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("416 响应必须保留 no-store，实际 %q", got)
	}
	if got := response.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("416 响应必须保留 nosniff，实际 %q", got)
	}

	status, header, body = updateRequest(t, http.MethodHead, server.URL+payload.DownloadPath)
	if status != http.StatusOK || len(body) != 0 {
		t.Fatalf("HEAD 下载失败: status=%d body=%q", status, body)
	}
	if header.Get("Content-Length") != fmt.Sprint(len(apk)) {
		t.Fatalf("HEAD Content-Length 不符: %q", header.Get("Content-Length"))
	}

	status, _, body = updateRequest(t, http.MethodGet,
		server.URL+"/api/app-update/download/"+strings.Repeat("b", 64)+"/")
	if status != http.StatusNotFound || !strings.Contains(string(body), "detail") {
		t.Fatalf("不存在的 SHA 应返回 JSON 404，实际 %d: %s", status, body)
	}
}

func TestAppUpdateRejectsBrokenRelease(t *testing.T) {
	t.Run("清单损坏", func(t *testing.T) {
		releaseDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(releaseDir, "latest.json"), []byte("{not json"), 0o644); err != nil {
			t.Fatalf("写入损坏清单失败: %v", err)
		}
		server := newUpdateServer(t, releaseDir, "")
		status, _, body := updateRequest(t, http.MethodGet, server.URL+"/api/app-update/")
		if status != http.StatusInternalServerError || strings.Contains(string(body), releaseDir) {
			t.Fatalf("损坏清单应返回不泄露路径的 500，实际 %d: %s", status, body)
		}
	})

	t.Run("清单尾随内容", func(t *testing.T) {
		releaseDir := t.TempDir()
		apk := []byte("apk")
		sha, filename := writeTestAPK(t, releaseDir, apk)
		data, err := json.Marshal(testManifest(sha, filename, len(apk)))
		if err != nil {
			t.Fatalf("序列化清单失败: %v", err)
		}
		data = append(data, []byte("{}")...)
		if err := os.WriteFile(filepath.Join(releaseDir, "latest.json"), data, 0o644); err != nil {
			t.Fatalf("写入清单失败: %v", err)
		}
		server := newUpdateServer(t, releaseDir, "")
		status, _, body := updateRequest(t, http.MethodGet, server.URL+"/api/app-update/")
		if status != http.StatusInternalServerError {
			t.Fatalf("尾随内容应返回 500，实际 %d: %s", status, body)
		}
	})

	t.Run("包大小不符", func(t *testing.T) {
		releaseDir := t.TempDir()
		apk := []byte("small-apk")
		sha, filename := writeTestAPK(t, releaseDir, apk)
		writeTestManifest(t, releaseDir, testManifest(sha, filename, len(apk)+1))
		server := newUpdateServer(t, releaseDir, "")
		for _, path := range []string{
			"/api/app-update/",
			"/api/app-update/download/" + sha + "/",
		} {
			status, _, body := updateRequest(t, http.MethodGet, server.URL+path)
			if status != http.StatusInternalServerError {
				t.Fatalf("%s 在包大小不符时应返回 500，实际 %d: %s", path, status, body)
			}
		}
	})

	t.Run("路径逃逸", func(t *testing.T) {
		base := t.TempDir()
		releaseDir := filepath.Join(base, "release")
		if err := os.Mkdir(releaseDir, 0o755); err != nil {
			t.Fatalf("创建发布目录失败: %v", err)
		}
		secret := []byte("do-not-serve-outside-release-dir")
		if err := os.WriteFile(filepath.Join(base, "secret.apk"), secret, 0o644); err != nil {
			t.Fatalf("写入目录外文件失败: %v", err)
		}
		sum := sha256.Sum256(secret)
		sha := hex.EncodeToString(sum[:])
		writeTestManifest(t, releaseDir, testManifest(sha, "../secret.apk", len(secret)))
		server := newUpdateServer(t, releaseDir, "")

		for _, path := range []string{
			"/api/app-update/",
			"/api/app-update/download/" + sha + "/",
		} {
			status, _, body := updateRequest(t, http.MethodGet, server.URL+path)
			if status != http.StatusInternalServerError {
				t.Fatalf("%s 应拒绝逃逸路径并返回 500，实际 %d", path, status)
			}
			if strings.Contains(string(body), string(secret)) || strings.Contains(string(body), "secret.apk") {
				t.Fatalf("%s 泄露了发布目录外文件: %s", path, body)
			}
		}
	})

	t.Run("符号链接逃逸", func(t *testing.T) {
		base := t.TempDir()
		releaseDir := filepath.Join(base, "release")
		if err := os.Mkdir(releaseDir, 0o755); err != nil {
			t.Fatalf("创建发布目录失败: %v", err)
		}
		secret := []byte("do-not-follow-symlink-outside-release-dir")
		secretPath := filepath.Join(base, "secret.apk")
		if err := os.WriteFile(secretPath, secret, 0o644); err != nil {
			t.Fatalf("写入目录外文件失败: %v", err)
		}
		if err := os.Symlink(secretPath, filepath.Join(releaseDir, "app.apk")); err != nil {
			t.Skipf("当前平台无法创建符号链接: %v", err)
		}
		sum := sha256.Sum256(secret)
		sha := hex.EncodeToString(sum[:])
		writeTestManifest(t, releaseDir, testManifest(sha, "app.apk", len(secret)))
		server := newUpdateServer(t, releaseDir, "")

		for _, path := range []string{
			"/api/app-update/",
			"/api/app-update/download/" + sha + "/",
		} {
			status, _, body := updateRequest(t, http.MethodGet, server.URL+path)
			if status != http.StatusInternalServerError {
				t.Fatalf("%s 应拒绝符号链接逃逸并返回 500，实际 %d", path, status)
			}
			if strings.Contains(string(body), string(secret)) {
				t.Fatalf("%s 泄露了符号链接目标内容", path)
			}
		}
	})
}
