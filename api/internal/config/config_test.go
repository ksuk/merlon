package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	cfg := Load()

	if cfg.Env != "development" {
		t.Errorf("Env = %q, want %q", cfg.Env, "development")
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":8080")
	}
	if cfg.CacheBackend != "memory" {
		t.Errorf("CacheBackend = %q, want %q", cfg.CacheBackend, "memory")
	}
	if cfg.EventBus != "pg_notify" {
		t.Errorf("EventBus = %q, want %q", cfg.EventBus, "pg_notify")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("MERLON_ENV", "production")
	t.Setenv("MERLON_HTTP_ADDR", ":9090")
	t.Setenv("MERLON_JWT_SECRET", "secret-value")
	t.Setenv("MERLON_LOG_LEVEL", "debug")

	cfg := Load()

	if cfg.Env != "production" {
		t.Errorf("Env = %q, want %q", cfg.Env, "production")
	}
	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":9090")
	}
	if cfg.JWTSecret != "secret-value" {
		t.Errorf("JWTSecret = %q, want %q", cfg.JWTSecret, "secret-value")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
}
