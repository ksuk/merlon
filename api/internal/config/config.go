package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env                  string
	HTTPAddr             string
	EngineAddr           string
	DatabaseURL          string
	MigrationDatabaseURL string
	MigrationBaseline    string
	EncryptionKeyRing    string
	Seed                 bool
	JWTSecret            string
	JWTPrivateKeyFile    string
	JWTPublicKeyFile     string
	ConfigPath           string
	CacheBackend         string
	EventBus             string
	LogLevel             string
	AdapterConfigPath    string
	UIDir                string
	RateLimit            int
	AuthEnabled          bool
	BootstrapToken       string
	EngineTLSCert        string
	EngineTLSServerName  string
	// WhitelistMaxValidDays is the maximum whitelist validity period (WL-002,
	// whitelist.md §要件表: "最大有効期間はシステム設定で制御可能（デフォルト：1年）").
	// TODO(WS-2): move to the rule management API once it supports
	// system-level settings, rather than an env var.
	WhitelistMaxValidDays int

	// Screening (WS-7): sanctions/PEP list auto-import and CDD-tier-driven
	// rescreening job startup, all disabled by default so a fresh
	// deployment never attempts external endpoint access or background
	// batches without explicit operator configuration (the screening workflow).
	ScreeningImportEnabled   bool
	ScreeningRescreenEnabled bool
	ScreeningImportInterval  time.Duration
	ScreeningCheckInterval   time.Duration
	ScreeningOFACURL         string
	ScreeningEUURL           string
	ScreeningUNURL           string
	ScreeningMOFURL          string
	ScreeningPEPURL          string

	// TM batch evaluation scheduler (WS-5 Task6,
	// the transaction-monitoring design「バッチ評価のスケジューリング」). TMBatchTimezone is
	// an IANA location name (e.g. "Asia/Tokyo"); empty means time.Local.
	TMBatchSchedule string
	TMBatchTimezone string

	// SMTP (WS-8 Task 5, NOTIF-001): mail server settings for alert email
	// notifications (notifications.md §1: "SMTP設定（ホスト、ポート、認証、
	// TLS）はシステム設定で構成する"). An empty SMTPHost disables the mailer.
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPTo       []string
	SMTPUseTLS   bool

	// NotifyRoutingPath points at a YAML file of notify.RoutingRule entries
	// (NOTIF-003). Empty uses notify.DefaultRoutingRules().
	NotifyRoutingPath string

	// PublicURL is the base URL of this Merlon instance, prefixed to alert
	// IDs to build the link carried in notification emails (notifications.md
	// §1: "ケース/アラートIDと本システムへのリンクのみを記載する").
	PublicURL string

	// EDD 3-stage escalation (the case-management workflow §EDD未実施継続時の段階的
	// 措置). Stage 1 (reminder) is fixed at 30 days; stages 2/3 are
	// "デフォルト、設定可" so they are configurable here.
	EDDStage2Days int
	EDDStage3Days int
}

func (c *Config) Validate() error {
	if c.Env == "production" {
		if !c.AuthEnabled {
			return fmt.Errorf("MERLON_AUTH_ENABLED must be true in production")
		}
		if strings.TrimSpace(c.DatabaseURL) == "" {
			return fmt.Errorf("MERLON_DATABASE_URL must be set in production")
		}
		if strings.TrimSpace(c.EncryptionKeyRing) == "" {
			return fmt.Errorf("MERLON_ENCRYPTION_KEY_RING must be set in production")
		}
		if c.Seed {
			return fmt.Errorf("MERLON_SEED must not be true in production")
		}
	}
	return nil
}

func Load() *Config {
	return &Config{
		Env:                   getEnv("MERLON_ENV", "development"),
		HTTPAddr:              getEnv("MERLON_HTTP_ADDR", ":8080"),
		EngineAddr:            getEnv("MERLON_ENGINE_ADDR", ""),
		DatabaseURL:           getEnv("MERLON_DATABASE_URL", ""),
		MigrationDatabaseURL:  getEnv("MERLON_MIGRATION_DATABASE_URL", ""),
		MigrationBaseline:     getEnv("MERLON_MIGRATION_BASELINE", ""),
		EncryptionKeyRing:     getEnv("MERLON_ENCRYPTION_KEY_RING", ""),
		Seed:                  getEnv("MERLON_SEED", "") == "true",
		JWTSecret:             getEnv("MERLON_JWT_SECRET", ""),
		JWTPrivateKeyFile:     getEnv("MERLON_JWT_PRIVATE_KEY_FILE", ""),
		JWTPublicKeyFile:      getEnv("MERLON_JWT_PUBLIC_KEY_FILE", ""),
		ConfigPath:            getEnv("MERLON_CONFIG_PATH", "config.yaml"),
		CacheBackend:          getEnv("MERLON_CACHE_BACKEND", "memory"),
		EventBus:              getEnv("MERLON_EVENT_BUS", "pg_notify"),
		LogLevel:              getEnv("MERLON_LOG_LEVEL", "info"),
		AdapterConfigPath:     getEnv("MERLON_ADAPTER_CONFIG_PATH", ""),
		UIDir:                 getEnv("MERLON_UI_DIR", ""),
		RateLimit:             getEnvInt("MERLON_RATE_LIMIT", 0),
		AuthEnabled:           getEnv("MERLON_AUTH_ENABLED", "") == "true",
		BootstrapToken:        getEnv("MERLON_BOOTSTRAP_TOKEN", ""),
		EngineTLSCert:         getEnv("MERLON_ENGINE_TLS_CERT", ""),
		EngineTLSServerName:   getEnv("MERLON_ENGINE_TLS_SERVER_NAME", ""),
		WhitelistMaxValidDays: getEnvInt("MERLON_WHITELIST_MAX_VALID_DAYS", 365),

		ScreeningImportEnabled:   getEnv("MERLON_SCREENING_IMPORT_ENABLED", "") == "true",
		ScreeningRescreenEnabled: getEnv("MERLON_SCREENING_RESCREEN_ENABLED", "") == "true",
		ScreeningImportInterval:  getEnvDuration("MERLON_SCREENING_IMPORT_INTERVAL", 24*time.Hour),
		ScreeningCheckInterval:   getEnvDuration("MERLON_SCREENING_CHECK_INTERVAL", time.Hour),
		ScreeningOFACURL:         getEnv("MERLON_SCREENING_OFAC_URL", ""),
		ScreeningEUURL:           getEnv("MERLON_SCREENING_EU_URL", ""),
		ScreeningUNURL:           getEnv("MERLON_SCREENING_UN_URL", ""),
		ScreeningMOFURL:          getEnv("MERLON_SCREENING_MOF_URL", ""),
		ScreeningPEPURL:          getEnv("MERLON_SCREENING_PEP_URL", ""),

		TMBatchSchedule: getEnv("MERLON_TM_BATCH_SCHEDULE", "02:00"),
		TMBatchTimezone: getEnv("MERLON_TM_BATCH_TIMEZONE", ""),

		SMTPHost:     getEnv("MERLON_SMTP_HOST", ""),
		SMTPPort:     getEnvInt("MERLON_SMTP_PORT", 587),
		SMTPUsername: getEnv("MERLON_SMTP_USERNAME", ""),
		SMTPPassword: getEnv("MERLON_SMTP_PASSWORD", ""),
		SMTPFrom:     getEnv("MERLON_SMTP_FROM", ""),
		SMTPTo:       getEnvList("MERLON_SMTP_TO"),
		SMTPUseTLS:   getEnv("MERLON_SMTP_USE_TLS", "") == "true",

		NotifyRoutingPath: getEnv("MERLON_NOTIFY_ROUTING_PATH", ""),
		PublicURL:         getEnv("MERLON_PUBLIC_URL", ""),

		EDDStage2Days: getEnvInt("MERLON_EDD_STAGE2_DAYS", 60),
		EDDStage3Days: getEnvInt("MERLON_EDD_STAGE3_DAYS", 90),
	}
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

// getEnvList splits a comma-separated env var into trimmed, non-empty
// entries (e.g. MERLON_SMTP_TO="a@example.com,b@example.com").
func getEnvList(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
