package config_test

import (
	"strings"
	"testing"

	"household-bills/backend/internal/config"
)

func TestLoadAdminPassword(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/household_bills_test")
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("COOKIE_SECURE", "")
	t.Setenv("ALLOWED_ORIGINS", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("缺少 ADMIN_PASSWORD 时配置加载应成功（由首次初始化决定是否必需）: %v", err)
	}
	if cfg.AdminPassword != "" {
		t.Fatal("ADMIN_PASSWORD 应为空")
	}

	t.Setenv("ADMIN_PASSWORD", "short")
	if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "ADMIN_PASSWORD") {
		t.Fatalf("过短密码应返回 ADMIN_PASSWORD 配置错误，实际: %v", err)
	}

	t.Setenv("ADMIN_PASSWORD", strings.Repeat("a", 73))
	if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "ADMIN_PASSWORD") {
		t.Fatalf("超过 72 字节的密码应返回配置错误，实际: %v", err)
	}

	const validPassword = "correct horse battery staple"
	t.Setenv("ADMIN_PASSWORD", validPassword)
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("有效 ADMIN_PASSWORD 应通过配置加载: %v", err)
	}
	if cfg.AdminPassword != validPassword {
		t.Fatal("ADMIN_PASSWORD 未按原值保留")
	}
}
