package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const (
	maxAppUpdateManifestBytes = 16 << 10
	maxAppUpdatePackageBytes  = 512 << 20
	maxAppUpdateVersionCode   = 2100000000
	minAppUpdateSDK           = 26
	maxAppUpdateVersionName   = 64
	appUpdateWriteTimeout     = 10 * time.Minute
)

var (
	appUpdateSHA256Pattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	appUpdatePackagePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,126}\.apk$`)
)

// appUpdateManifest mirrors the publisher-produced latest.json. The file is
// untrusted deployment input: every value is validated before use and the
// package is addressed only through the configured release directory.
type appUpdateManifest struct {
	VersionCode int64  `json:"version_code"`
	VersionName string `json:"version_name"`
	MinSDK      int64  `json:"min_sdk"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
	APKFile     string `json:"apk_file"`
}

func isAppUpdatePath(requestPath string) bool {
	return requestPath == "/api/app-update" ||
		strings.HasPrefix(requestPath, "/api/app-update/")
}

func (a *API) handleAppUpdate(c *gin.Context) {
	root, available, err := a.openAppUpdateRoot()
	if err != nil {
		a.logger.Printf("读取应用更新目录失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	if !available {
		c.JSON(http.StatusOK, gin.H{"available": false})
		return
	}
	defer root.Close()

	manifest, available, err := readAppUpdateManifest(root)
	if err != nil {
		a.logger.Printf("读取应用更新清单失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	if !available {
		c.JSON(http.StatusOK, gin.H{"available": false})
		return
	}

	file, _, err := openAppUpdatePackage(root, manifest)
	if err != nil {
		a.logger.Printf("应用更新包与清单不一致: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	if err := file.Close(); err != nil {
		a.logger.Printf("关闭应用更新包失败: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"available":     true,
		"version_code":  manifest.VersionCode,
		"version_name":  manifest.VersionName,
		"min_sdk":       manifest.MinSDK,
		"size_bytes":    manifest.SizeBytes,
		"sha256":        manifest.SHA256,
		"download_path": "/api/app-update/download/" + manifest.SHA256 + "/",
	})
}

func (a *API) handleAppUpdateDownload(c *gin.Context) {
	requestedSHA := c.Param("sha256")
	if !appUpdateSHA256Pattern.MatchString(requestedSHA) {
		abortDetail(c, http.StatusNotFound, "更新包不存在。")
		return
	}

	root, available, err := a.openAppUpdateRoot()
	if err != nil {
		a.logger.Printf("读取应用更新目录失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	if !available {
		abortDetail(c, http.StatusNotFound, "更新包不存在。")
		return
	}
	defer root.Close()

	manifest, available, err := readAppUpdateManifest(root)
	if err != nil {
		a.logger.Printf("读取应用更新清单失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	if !available || manifest.SHA256 != requestedSHA {
		abortDetail(c, http.StatusNotFound, "更新包不存在。")
		return
	}

	file, info, err := openAppUpdatePackage(root, manifest)
	if err != nil {
		a.logger.Printf("读取应用更新包失败: %v", err)
		abortDetail(c, http.StatusInternalServerError, "服务器内部错误。")
		return
	}
	defer file.Close()

	// The server's global write timeout is shorter than a slow mobile
	// download. Keep a finite per-response deadline instead of clearing it.
	if err := http.NewResponseController(c.Writer).SetWriteDeadline(
		time.Now().Add(appUpdateWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		a.logger.Printf("设置应用更新下载超时失败: %v", err)
	}

	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Type", "application/vnd.android.package-archive")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, manifest.APKFile))
	// net/http deliberately strips Cache-Control from ServeContent's own error
	// responses (e.g. 416). Re-assert the update headers whenever the response
	// status is committed so every download response remains no-store/nosniff.
	http.ServeContent(appUpdateResponseWriter{c.Writer}, c.Request,
		manifest.APKFile, info.ModTime(), file)
}

type appUpdateResponseWriter struct {
	http.ResponseWriter
}

func (w appUpdateResponseWriter) WriteHeader(statusCode int) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.ResponseWriter.WriteHeader(statusCode)
}

func (a *API) openAppUpdateRoot() (*os.Root, bool, error) {
	dir := strings.TrimSpace(a.cfg.AndroidReleaseDir)
	if dir == "" {
		return nil, false, nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return root, true, nil
}

// readAppUpdateManifest reports available=false when no release has been
// published yet (missing latest.json). A present but invalid manifest is an
// internal deployment error, not a "no update" signal.
func readAppUpdateManifest(root *os.Root) (appUpdateManifest, bool, error) {
	file, err := root.Open("latest.json")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return appUpdateManifest{}, false, nil
		}
		return appUpdateManifest{}, false, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return appUpdateManifest{}, false, err
	}
	if !info.Mode().IsRegular() {
		return appUpdateManifest{}, false, errors.New("latest.json 不是普通文件")
	}
	if info.Size() > maxAppUpdateManifestBytes {
		return appUpdateManifest{}, false, errors.New("latest.json 超过 16KiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxAppUpdateManifestBytes+1))
	if err != nil {
		return appUpdateManifest{}, false, err
	}
	if len(data) > maxAppUpdateManifestBytes {
		return appUpdateManifest{}, false, errors.New("latest.json 超过 16KiB")
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest appUpdateManifest
	if err := decoder.Decode(&manifest); err != nil {
		return appUpdateManifest{}, false, fmt.Errorf("解析 latest.json: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return appUpdateManifest{}, false, errors.New("latest.json 包含尾随内容")
	}
	if err := validateAppUpdateManifest(manifest); err != nil {
		return appUpdateManifest{}, false, err
	}
	return manifest, true, nil
}

func validateAppUpdateManifest(manifest appUpdateManifest) error {
	if manifest.VersionCode <= 0 || manifest.VersionCode > maxAppUpdateVersionCode {
		return fmt.Errorf("version_code 越界: %d", manifest.VersionCode)
	}
	name := strings.TrimSpace(manifest.VersionName)
	if name == "" || utf8.RuneCountInString(name) > maxAppUpdateVersionName ||
		!utf8.ValidString(name) {
		return errors.New("version_name 无效")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("version_name 包含控制字符")
		}
	}
	if manifest.MinSDK < minAppUpdateSDK {
		return fmt.Errorf("min_sdk 过小: %d", manifest.MinSDK)
	}
	if manifest.SizeBytes <= 0 || manifest.SizeBytes > maxAppUpdatePackageBytes {
		return fmt.Errorf("size_bytes 越界: %d", manifest.SizeBytes)
	}
	if !appUpdateSHA256Pattern.MatchString(manifest.SHA256) {
		return errors.New("sha256 必须是 64 位小写十六进制")
	}
	if !appUpdatePackagePattern.MatchString(manifest.APKFile) ||
		strings.ContainsAny(manifest.APKFile, `/\`) {
		return fmt.Errorf("apk_file 不安全: %q", manifest.APKFile)
	}
	return nil
}

func openAppUpdatePackage(root *os.Root, manifest appUpdateManifest) (*os.File, os.FileInfo, error) {
	file, err := root.Open(manifest.APKFile)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, nil, errors.New("更新包不是普通文件")
	}
	if info.Size() != manifest.SizeBytes {
		file.Close()
		return nil, nil, fmt.Errorf(
			"更新包大小 %d 与清单 %d 不一致", info.Size(), manifest.SizeBytes)
	}
	return file, info, nil
}
