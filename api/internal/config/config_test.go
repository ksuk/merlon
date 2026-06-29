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
	if cfg.AdapterConfigPath != "" {
		t.Errorf("AdapterConfigPath = %q, want %q", cfg.AdapterConfigPath, "")
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

func TestLoadAdapterConfigFromEnv(t *testing.T) {
	t.Setenv("MERLON_ADAPTER_CONFIG_PATH", "/app/adapters/core.yaml")
	cfg := Load()
	if cfg.AdapterConfigPath != "/app/adapters/core.yaml" {
		t.Errorf("AdapterConfigPath = %q, want %q", cfg.AdapterConfigPath, "/app/adapters/core.yaml")
	}
}

func TestLoadBootstrapToken(t *testing.T) {
	t.Setenv("MERLON_BOOTSTRAP_TOKEN", "my-secret-token")
	cfg := Load()
	if cfg.BootstrapToken != "my-secret-token" {
		t.Errorf("BootstrapToken = %q, want %q", cfg.BootstrapToken, "my-secret-token")
	}
}

func TestValidateProductionWithoutAuth(t *testing.T) {
	cfg := &Config{Env: "production", AuthEnabled: false}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should fail for production without auth")
	}
}

func TestValidateProductionWithAuth(t *testing.T) {
	cfg := &Config{Env: "production", AuthEnabled: true}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}

func TestValidateDevelopmentWithoutAuth(t *testing.T) {
	cfg := &Config{Env: "development", AuthEnabled: false}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}
