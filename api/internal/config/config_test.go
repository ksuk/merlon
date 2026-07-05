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

func TestLoadWhitelistMaxValidDaysDefault(t *testing.T) {
	cfg := Load()
	if cfg.WhitelistMaxValidDays != 365 {
		t.Errorf("WhitelistMaxValidDays = %d, want 365", cfg.WhitelistMaxValidDays)
	}
}

func TestLoadWhitelistMaxValidDaysFromEnv(t *testing.T) {
	t.Setenv("MERLON_WHITELIST_MAX_VALID_DAYS", "180")
	cfg := Load()
	if cfg.WhitelistMaxValidDays != 180 {
		t.Errorf("WhitelistMaxValidDays = %d, want 180", cfg.WhitelistMaxValidDays)
	}
}

func TestLoadSMTPDefaults(t *testing.T) {
	cfg := Load()
	if cfg.SMTPHost != "" {
		t.Errorf("SMTPHost = %q, want empty (mailer disabled by default)", cfg.SMTPHost)
	}
	if cfg.SMTPPort != 587 {
		t.Errorf("SMTPPort = %d, want 587", cfg.SMTPPort)
	}
	if cfg.SMTPUseTLS {
		t.Error("SMTPUseTLS = true, want false by default")
	}
	if len(cfg.SMTPTo) != 0 {
		t.Errorf("SMTPTo = %v, want empty", cfg.SMTPTo)
	}
}

func TestLoadSMTPFromEnv(t *testing.T) {
	t.Setenv("MERLON_SMTP_HOST", "smtp.example.com")
	t.Setenv("MERLON_SMTP_PORT", "465")
	t.Setenv("MERLON_SMTP_USERNAME", "merlon")
	t.Setenv("MERLON_SMTP_PASSWORD", "hunter2")
	t.Setenv("MERLON_SMTP_FROM", "merlon@example.com")
	t.Setenv("MERLON_SMTP_TO", "compliance@example.com, security@example.com")
	t.Setenv("MERLON_SMTP_USE_TLS", "true")

	cfg := Load()

	if cfg.SMTPHost != "smtp.example.com" {
		t.Errorf("SMTPHost = %q, want smtp.example.com", cfg.SMTPHost)
	}
	if cfg.SMTPPort != 465 {
		t.Errorf("SMTPPort = %d, want 465", cfg.SMTPPort)
	}
	if cfg.SMTPUsername != "merlon" {
		t.Errorf("SMTPUsername = %q, want merlon", cfg.SMTPUsername)
	}
	if cfg.SMTPPassword != "hunter2" {
		t.Errorf("SMTPPassword = %q, want hunter2", cfg.SMTPPassword)
	}
	if cfg.SMTPFrom != "merlon@example.com" {
		t.Errorf("SMTPFrom = %q, want merlon@example.com", cfg.SMTPFrom)
	}
	want := []string{"compliance@example.com", "security@example.com"}
	if len(cfg.SMTPTo) != len(want) || cfg.SMTPTo[0] != want[0] || cfg.SMTPTo[1] != want[1] {
		t.Errorf("SMTPTo = %v, want %v", cfg.SMTPTo, want)
	}
	if !cfg.SMTPUseTLS {
		t.Error("SMTPUseTLS = false, want true")
	}
}

func TestLoadNotifyRoutingAndPublicURLFromEnv(t *testing.T) {
	t.Setenv("MERLON_NOTIFY_ROUTING_PATH", "/app/content/notify_routing.yaml")
	t.Setenv("MERLON_PUBLIC_URL", "https://merlon.internal")

	cfg := Load()

	if cfg.NotifyRoutingPath != "/app/content/notify_routing.yaml" {
		t.Errorf("NotifyRoutingPath = %q, want /app/content/notify_routing.yaml", cfg.NotifyRoutingPath)
	}
	if cfg.PublicURL != "https://merlon.internal" {
		t.Errorf("PublicURL = %q, want https://merlon.internal", cfg.PublicURL)
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
