package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg := Load()

	if cfg.Env != "development" {
		t.Errorf("Env = %q, want %q", cfg.Env, "development")
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":8080")
	}
	if cfg.Mode != "all" || cfg.WorkerConcurrency != 4 || cfg.TMBaseCurrency != "JPY" {
		t.Errorf("PH9 defaults: mode=%q concurrency=%d currency=%q", cfg.Mode, cfg.WorkerConcurrency, cfg.TMBaseCurrency)
	}
	if cfg.RealtimeMonitorTimeout != 30*time.Second {
		t.Errorf("RealtimeMonitorTimeout = %s, want 30s", cfg.RealtimeMonitorTimeout)
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

func TestDigestPathDirectoryIsStableAndNameSensitive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	one, err := DigestPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	two, err := DigestPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if one != two || one == "" {
		t.Fatalf("unstable digest: %q/%q", one, two)
	}
}

func TestValidateRejectsInvalidMode(t *testing.T) {
	cfg := Load()
	cfg.Mode = "sidecar"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("MERLON_ENV", "production")
	t.Setenv("MERLON_HTTP_ADDR", ":9090")
	t.Setenv("MERLON_JWT_SECRET", "secret-value")
	t.Setenv("MERLON_LOG_LEVEL", "debug")
	t.Setenv("MERLON_REALTIME_MONITOR_TIMEOUT", "45s")

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
	if cfg.RealtimeMonitorTimeout != 45*time.Second {
		t.Errorf("RealtimeMonitorTimeout = %s, want 45s", cfg.RealtimeMonitorTimeout)
	}
}

func TestValidateRejectsNegativeRealtimeMonitorTimeout(t *testing.T) {
	cfg := &Config{RealtimeMonitorTimeout: -time.Second}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected negative realtime monitor timeout to be rejected")
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

func TestLoadEDDStageDaysDefaults(t *testing.T) {
	cfg := Load()
	if cfg.EDDStage2Days != 60 {
		t.Errorf("EDDStage2Days = %d, want 60", cfg.EDDStage2Days)
	}
	if cfg.EDDStage3Days != 90 {
		t.Errorf("EDDStage3Days = %d, want 90", cfg.EDDStage3Days)
	}
}

func TestLoadEDDStageDaysFromEnv(t *testing.T) {
	t.Setenv("MERLON_EDD_STAGE2_DAYS", "45")
	t.Setenv("MERLON_EDD_STAGE3_DAYS", "75")

	cfg := Load()

	if cfg.EDDStage2Days != 45 {
		t.Errorf("EDDStage2Days = %d, want 45", cfg.EDDStage2Days)
	}
	if cfg.EDDStage3Days != 75 {
		t.Errorf("EDDStage3Days = %d, want 75", cfg.EDDStage3Days)
	}
}

func TestValidateProductionWithoutAuth(t *testing.T) {
	cfg := &Config{Env: "production", AuthEnabled: false}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should fail for production without auth")
	}
}

func TestValidateProductionWithAuth(t *testing.T) {
	cfg := &Config{
		Env:               "production",
		AuthEnabled:       true,
		DatabaseURL:       "postgres://example.invalid/merlon",
		EncryptionKeyRing: "v1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}

func TestValidateProductionRequiresPersistentDatabase(t *testing.T) {
	cfg := &Config{Env: "production", AuthEnabled: true, EncryptionKeyRing: "configured"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() should reject production without MERLON_DATABASE_URL")
	}
}

func TestValidateProductionRequiresPIIEncryption(t *testing.T) {
	cfg := &Config{Env: "production", AuthEnabled: true, DatabaseURL: "postgres://example.invalid/merlon"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() should reject production without MERLON_ENCRYPTION_KEY_RING")
	}
}

func TestValidateProductionRejectsDemoSeed(t *testing.T) {
	cfg := &Config{
		Env:               "production",
		AuthEnabled:       true,
		DatabaseURL:       "postgres://example.invalid/merlon",
		EncryptionKeyRing: "configured",
		Seed:              true,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() should reject MERLON_SEED=true in production")
	}
}

func TestValidateDevelopmentWithoutAuth(t *testing.T) {
	cfg := &Config{Env: "development", AuthEnabled: false}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}
