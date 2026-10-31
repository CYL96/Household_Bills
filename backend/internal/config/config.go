package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"household-bills/backend/internal/credentials"
)

// Config contains the runtime configuration for the HTTP server and database.
// Parent deployment owns environment files; this package only consumes env vars.
type Config struct {
	DatabaseURL       string
	ListenAddr        string
	CookieSecure      bool
	FrontendDist      string
	AndroidReleaseDir string
	AllowedOrigins    []string
	AdminPassword     string
}

// Load reads and validates environment configuration. DATABASE_URL is required;
// PostgreSQL is the only supported database.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:  strings.TrimSpace(os.Getenv("DATABASE_URL")),
		ListenAddr:   strings.TrimSpace(envOr("LISTEN_ADDR", ":8000")),
		FrontendDist: strings.TrimSpace(envOr("FRONTEND_DIST", "../frontend/dist")),
		AndroidReleaseDir: strings.TrimSpace(
			envOr("ANDROID_RELEASE_DIR", "../android/release")),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("必须设置 DATABASE_URL（仅支持 PostgreSQL）")
	}
	// ADMIN_PASSWORD may be absent once the bootstrap admin exists; store
	// initialization decides whether a value is required. A supplied value is
	// always subject to the shared password policy.
	if cfg.AdminPassword != "" {
		if message := credentials.PasswordValidationError(cfg.AdminPassword); message != "" {
			return Config{}, fmt.Errorf("ADMIN_PASSWORD 无效：%s", message)
		}
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8000"
	}
	if cfg.FrontendDist == "" {
		cfg.FrontendDist = "../frontend/dist"
	}
	if cfg.AndroidReleaseDir == "" {
		cfg.AndroidReleaseDir = "../android/release"
	}

	rawSecure, ok := os.LookupEnv("COOKIE_SECURE")
	if !ok || strings.TrimSpace(rawSecure) == "" {
		cfg.CookieSecure = true
	} else {
		secure, err := parseBool(rawSecure)
		if err != nil {
			return Config{}, errors.New("COOKIE_SECURE 必须是 true 或 false")
		}
		cfg.CookieSecure = secure
	}

	origins, err := parseOrigins(os.Getenv("ALLOWED_ORIGINS"))
	if err != nil {
		return Config{}, err
	}
	cfg.AllowedOrigins = origins

	return cfg, nil
}

func envOr(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean %q", raw)
	}
}

// parseOrigins normalizes a comma-separated allow-list. Entries are exact
// origins such as https://bills.example.com and are compared case-insensitively.
func parseOrigins(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	seen := make(map[string]struct{})
	origins := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		normalized, ok := normalizeOrigin(part)
		if !ok {
			return nil, fmt.Errorf("ALLOWED_ORIGINS 包含无效来源 %q", part)
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		origins = append(origins, normalized)
	}
	return origins, nil
}

func normalizeOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Host == "" {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	if u.Path != "" && u.Path != "/" {
		return "", false
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), true
}
