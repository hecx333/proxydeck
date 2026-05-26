package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		ProxyListen          string `yaml:"proxy_listen"`
		AdminListen          string `yaml:"admin_listen"`
		ProxyDialTimeoutSec  int    `yaml:"proxy_dial_timeout_seconds"`
		ProxyIdleTimeoutSec  int    `yaml:"proxy_idle_timeout_seconds"`
		ProxyRespTimeoutSec  int    `yaml:"proxy_response_header_timeout_seconds"`
		ProxyConnectTimeoutS int    `yaml:"proxy_connect_timeout_seconds"`
	} `yaml:"server"`
	SQLite struct {
		Path string `yaml:"path"`
	} `yaml:"sqlite"`
	Redis struct {
		Addr     string `yaml:"addr"`
		Password string `yaml:"password"`
		DB       int    `yaml:"db"`
	} `yaml:"redis"`
	Healthcheck struct {
		IntervalSeconds int `yaml:"interval_seconds"`
		TimeoutSeconds  int `yaml:"timeout_seconds"`
		MaxFailCount    int `yaml:"max_fail_count"`
	} `yaml:"healthcheck"`
	Retry struct {
		MaxAttempts   int `yaml:"max_attempts"`
		BaseBackoffMS int `yaml:"base_backoff_ms"`
	} `yaml:"retry"`
	Sticky struct {
		TTLSeconds int `yaml:"ttl_seconds"`
	} `yaml:"sticky"`
	Security struct {
		EncryptionKey string `yaml:"encryption_key"`
		SessionTTL    int    `yaml:"session_ttl_seconds"`
	} `yaml:"security"`
	Admin struct {
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"admin"`
}

func Default() Config {
	var cfg Config
	cfg.Server.ProxyListen = "0.0.0.0:20000"
	cfg.Server.AdminListen = "0.0.0.0:8080"
	cfg.Server.ProxyDialTimeoutSec = 10
	cfg.Server.ProxyIdleTimeoutSec = 90
	cfg.Server.ProxyRespTimeoutSec = 15
	cfg.Server.ProxyConnectTimeoutS = 15
	cfg.SQLite.Path = "./data/app.db"
	cfg.Redis.Addr = "127.0.0.1:6379"
	cfg.Healthcheck.IntervalSeconds = 300
	cfg.Healthcheck.TimeoutSeconds = 10
	cfg.Healthcheck.MaxFailCount = 3
	cfg.Retry.MaxAttempts = 3
	cfg.Retry.BaseBackoffMS = 500
	cfg.Sticky.TTLSeconds = 3600
	cfg.Security.EncryptionKey = "change-this-with-a-strong-random-secret"
	cfg.Security.SessionTTL = 86400
	cfg.Admin.Username = "admin"
	cfg.Admin.Password = "change-this-before-first-run"
	return cfg
}

func Load(path string) (Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	applyEnvOverrides(&cfg)
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Security.EncryptionKey) == "" {
		return errors.New("security.encryption_key is required")
	}
	if insecureEncryptionKey(c.Security.EncryptionKey) {
		return fmt.Errorf("security.encryption_key uses a sample placeholder; set PROXYDECK_SECURITY_ENCRYPTION_KEY or update the config file")
	}
	if strings.TrimSpace(c.Admin.Username) == "" {
		return errors.New("admin.username is required")
	}
	if strings.TrimSpace(c.Admin.Password) == "" {
		return errors.New("admin.password is required")
	}
	if insecureAdminPassword(c.Admin.Password) {
		return fmt.Errorf("admin.password uses a sample placeholder; set PROXYDECK_ADMIN_PASSWORD or update the config file")
	}
	return nil
}

func applyEnvOverrides(cfg *Config) {
	applyString(&cfg.Server.ProxyListen, "PROXYDECK_PROXY_LISTEN")
	applyString(&cfg.Server.AdminListen, "PROXYDECK_ADMIN_LISTEN")
	applyInt(&cfg.Server.ProxyDialTimeoutSec, "PROXYDECK_PROXY_DIAL_TIMEOUT_SECONDS")
	applyInt(&cfg.Server.ProxyIdleTimeoutSec, "PROXYDECK_PROXY_IDLE_TIMEOUT_SECONDS")
	applyInt(&cfg.Server.ProxyRespTimeoutSec, "PROXYDECK_PROXY_RESPONSE_HEADER_TIMEOUT_SECONDS")
	applyInt(&cfg.Server.ProxyConnectTimeoutS, "PROXYDECK_PROXY_CONNECT_TIMEOUT_SECONDS")

	applyString(&cfg.SQLite.Path, "PROXYDECK_SQLITE_PATH")

	applyString(&cfg.Redis.Addr, "PROXYDECK_REDIS_ADDR")
	applyString(&cfg.Redis.Password, "PROXYDECK_REDIS_PASSWORD")
	applyInt(&cfg.Redis.DB, "PROXYDECK_REDIS_DB")

	applyInt(&cfg.Healthcheck.IntervalSeconds, "PROXYDECK_HEALTHCHECK_INTERVAL_SECONDS")
	applyInt(&cfg.Healthcheck.TimeoutSeconds, "PROXYDECK_HEALTHCHECK_TIMEOUT_SECONDS")
	applyInt(&cfg.Healthcheck.MaxFailCount, "PROXYDECK_HEALTHCHECK_MAX_FAIL_COUNT")

	applyInt(&cfg.Retry.MaxAttempts, "PROXYDECK_RETRY_MAX_ATTEMPTS")
	applyInt(&cfg.Retry.BaseBackoffMS, "PROXYDECK_RETRY_BASE_BACKOFF_MS")

	applyInt(&cfg.Sticky.TTLSeconds, "PROXYDECK_STICKY_TTL_SECONDS")

	applyString(&cfg.Security.EncryptionKey, "PROXYDECK_SECURITY_ENCRYPTION_KEY")
	applyInt(&cfg.Security.SessionTTL, "PROXYDECK_SESSION_TTL_SECONDS")

	applyString(&cfg.Admin.Username, "PROXYDECK_ADMIN_USERNAME")
	applyString(&cfg.Admin.Password, "PROXYDECK_ADMIN_PASSWORD")
}

func applyString(target *string, key string) {
	if value := os.Getenv(key); value != "" {
		*target = value
	}
}

func applyInt(target *int, key string) {
	value := os.Getenv(key)
	if value == "" {
		return
	}
	if parsed, err := strconv.Atoi(value); err == nil {
		*target = parsed
	}
}

func (c Config) StickyTTL() time.Duration {
	return time.Duration(c.Sticky.TTLSeconds) * time.Second
}

func (c Config) HealthcheckInterval() time.Duration {
	return time.Duration(c.Healthcheck.IntervalSeconds) * time.Second
}

func (c Config) HealthcheckTimeout() time.Duration {
	return time.Duration(c.Healthcheck.TimeoutSeconds) * time.Second
}

func (c Config) SessionTTL() time.Duration {
	return time.Duration(c.Security.SessionTTL) * time.Second
}

func (c Config) ProxyDialTimeout() time.Duration {
	return time.Duration(c.Server.ProxyDialTimeoutSec) * time.Second
}

func (c Config) ProxyIdleTimeout() time.Duration {
	return time.Duration(c.Server.ProxyIdleTimeoutSec) * time.Second
}

func (c Config) ProxyResponseHeaderTimeout() time.Duration {
	return time.Duration(c.Server.ProxyRespTimeoutSec) * time.Second
}

func (c Config) ProxyConnectTimeout() time.Duration {
	return time.Duration(c.Server.ProxyConnectTimeoutS) * time.Second
}

func (c Config) RetryBaseBackoff() time.Duration {
	return time.Duration(c.Retry.BaseBackoffMS) * time.Millisecond
}

func insecureEncryptionKey(value string) bool {
	switch strings.TrimSpace(value) {
	case "change-this-with-a-strong-random-secret", "replace-this-with-a-strong-32-byte-key", "change-me-32-bytes-secret-key!!":
		return true
	default:
		return false
	}
}

func insecureAdminPassword(value string) bool {
	switch strings.TrimSpace(value) {
	case "admin123456", "change-this-before-first-run":
		return true
	default:
		return false
	}
}
