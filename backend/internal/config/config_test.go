package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesEnvOverrides(t *testing.T) {
	t.Setenv("PROXYDECK_REDIS_ADDR", "redis:6379")
	t.Setenv("PROXYDECK_SQLITE_PATH", "/app/data/app.db")
	t.Setenv("PROXYDECK_ADMIN_PASSWORD", "override-secret")
	t.Setenv("PROXYDECK_SECURITY_ENCRYPTION_KEY", "override-encryption-secret")
	t.Setenv("PROXYDECK_PROXY_CONNECT_TIMEOUT_SECONDS", "22")
	t.Setenv("PROXYDECK_RETRY_MAX_ATTEMPTS", "7")

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := []byte(`
server:
  proxy_connect_timeout_seconds: 15
sqlite:
  path: "./data/app.db"
redis:
  addr: "127.0.0.1:6379"
admin:
  password: "admin123456"
retry:
  max_attempts: 3
security:
  encryption_key: "change-this-with-a-strong-random-secret"
`)
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Redis.Addr != "redis:6379" {
		t.Fatalf("redis addr = %q, want redis:6379", cfg.Redis.Addr)
	}
	if cfg.SQLite.Path != "/app/data/app.db" {
		t.Fatalf("sqlite path = %q, want /app/data/app.db", cfg.SQLite.Path)
	}
	if cfg.Admin.Password != "override-secret" {
		t.Fatalf("admin password = %q, want override-secret", cfg.Admin.Password)
	}
	if cfg.Server.ProxyConnectTimeoutS != 22 {
		t.Fatalf("proxy connect timeout = %d, want 22", cfg.Server.ProxyConnectTimeoutS)
	}
	if cfg.Retry.MaxAttempts != 7 {
		t.Fatalf("retry max attempts = %d, want 7", cfg.Retry.MaxAttempts)
	}
}

func TestLoadRejectsSampleSecrets(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := []byte(`
security:
  encryption_key: "change-this-with-a-strong-random-secret"
admin:
  username: "admin"
  password: "change-this-before-first-run"
`)
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected sample secrets to be rejected")
	}
}
