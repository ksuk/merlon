package config

import (
	"crypto/sha256"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env string
	// Mode controls process ownership: api, worker, or all.
	Mode                 string
	HTTPAddr             string
	WorkerHTTPAddr       string
	WorkerConcurrency    int
	DatabaseURL          string
	MigrationDatabaseURL string
	MigrationBaseline    string
	EncryptionKeyRing    string
	Seed                 bool
	// DemoDataDir is MERLON_DEMO_DATA_DIR, trimmed. The seed package reads the
	// same environment value; final synthetic-data provenance comes from the
	// seed completion state rather than this configuration hint.
	DemoDataDir            string
	JWTSecret              string
	JWTPrivateKeyFile      string
	JWTPublicKeyFile       string
	ConfigPath             string
	CacheBackend           string
	EventBus               string
	LogLevel               string
	AdapterConfigPath      string
	UIDir                  string
	RateLimit              int
	TrustedProxyCIDRs      []netip.Prefix
	AuthEnabled            bool
	BootstrapToken         string
	CountryRiskPath        string
	TMBaseCurrency         string
	RealtimeMonitorTimeout time.Duration
	trustedProxyCIDRsErr   error
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

	// OperatorTeams is the configured assignment-team directory. Queue rows
	// are deliberately not a source of truth for this list.
	OperatorTeams []string
	// CasePriorityPath points to the versioned CDD-to-case-priority policy.
	CasePriorityPath string

	// Wave 3 policy documents (ADR-0016). Each is a versioned YAML file with
	// an in-code default; a blank path selects that default. They are the
	// single source for rules that used to be hardcoded in Go or duplicated
	// across packages.
	KYCRequiredFieldsPath  string
	EDDPolicyPath          string
	CDDRuleSelectionPath   string
	TravelRulePolicyPath   string
	ScreeningReadinessPath string

	// PublicURL is the base URL of this Merlon instance, prefixed to alert
	// IDs to build the link carried in notification emails (notifications.md
	// §1: "ケース/アラートIDと本システムへのリンクのみを記載する").
	PublicURL string

	// EDD 3-stage escalation (the case-management workflow §EDD未実施継続時の段階的
	// 措置).
	//
	// Deprecated: superseded by EDDPolicyPath, which is the single source for
	// the whole stage schedule including stage 1. These remain so an existing
	// deployment's environment keeps parsing; when both are set the policy
	// file wins and main logs a warning.
	EDDStage2Days int
	EDDStage3Days int
}

// DigestPath returns a stable SHA-256 for a config file or directory. Directory
// entries are sorted and include their relative names, so operators can pin
// the exact rule/list snapshot used by an evaluation without trusting mtime.
func DigestPath(path string) (string, error) {
	h := sha256.New()
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", err
		}
		for _, entry := range entries {
			if entry.IsDir() || (filepath.Ext(entry.Name()) != ".yaml" && filepath.Ext(entry.Name()) != ".yml") {
				continue
			}
			content, err := os.ReadFile(filepath.Join(path, entry.Name()))
			if err != nil {
				return "", err
			}
			h.Write([]byte(entry.Name()))
			h.Write([]byte{0})
			h.Write(content)
			h.Write([]byte{0})
		}
	} else {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		h.Write(content)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func (c *Config) Validate() error {
	mode := c.Mode
	if mode == "" {
		mode = "all" // zero-value Config remains useful in focused unit tests
	}
	if mode != "api" && mode != "worker" && mode != "all" {
		return fmt.Errorf("MERLON_MODE must be one of api, worker, all (got %q)", mode)
	}
	if c.WorkerConcurrency < 0 {
		return fmt.Errorf("MERLON_WORKER_CONCURRENCY must be at least 1")
	}
	if c.WorkerConcurrency > 256 {
		return fmt.Errorf("MERLON_WORKER_CONCURRENCY must not exceed 256")
	}
	if c.WorkerConcurrency == 0 {
		// A zero value is interpreted as the Load default. This also keeps
		// hand-built Config values backwards compatible.
		c.WorkerConcurrency = 4
	}
	if strings.TrimSpace(c.TMBaseCurrency) == "" {
		c.TMBaseCurrency = "JPY"
	}
	if c.RealtimeMonitorTimeout < 0 {
		return fmt.Errorf("MERLON_REALTIME_MONITOR_TIMEOUT must be positive")
	}
	if c.RealtimeMonitorTimeout == 0 {
		c.RealtimeMonitorTimeout = 30 * time.Second
	}
	if c.RateLimit < 0 {
		return fmt.Errorf("MERLON_RATE_LIMIT must not be negative")
	}
	if c.trustedProxyCIDRsErr != nil {
		return c.trustedProxyCIDRsErr
	}
	for _, prefix := range c.TrustedProxyCIDRs {
		if !prefix.IsValid() {
			return fmt.Errorf("MERLON_TRUSTED_PROXY_CIDRS contains an invalid prefix")
		}
		if prefix.Bits() == 0 {
			return fmt.Errorf("MERLON_TRUSTED_PROXY_CIDRS must not trust an entire address family")
		}
	}
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
		if c.RateLimit > 0 && len(c.TrustedProxyCIDRs) == 0 {
			return fmt.Errorf("MERLON_TRUSTED_PROXY_CIDRS must be set when MERLON_RATE_LIMIT is enabled in production")
		}
	}
	return nil
}

func Load() *Config {
	trustedProxyCIDRs, trustedProxyCIDRsErr := parseCIDRs(getEnvList("MERLON_TRUSTED_PROXY_CIDRS"))

	return &Config{
		Env:                    getEnv("MERLON_ENV", "development"),
		Mode:                   getEnv("MERLON_MODE", "all"),
		HTTPAddr:               getEnv("MERLON_HTTP_ADDR", ":8080"),
		WorkerHTTPAddr:         getEnv("MERLON_WORKER_HTTP_ADDR", ":8081"),
		WorkerConcurrency:      getEnvInt("MERLON_WORKER_CONCURRENCY", 4),
		DatabaseURL:            getEnv("MERLON_DATABASE_URL", ""),
		MigrationDatabaseURL:   getEnv("MERLON_MIGRATION_DATABASE_URL", ""),
		MigrationBaseline:      getEnv("MERLON_MIGRATION_BASELINE", ""),
		EncryptionKeyRing:      getEnv("MERLON_ENCRYPTION_KEY_RING", ""),
		Seed:                   getEnv("MERLON_SEED", "") == "true",
		DemoDataDir:            strings.TrimSpace(getEnv("MERLON_DEMO_DATA_DIR", "")),
		JWTSecret:              getEnv("MERLON_JWT_SECRET", ""),
		JWTPrivateKeyFile:      getEnv("MERLON_JWT_PRIVATE_KEY_FILE", ""),
		JWTPublicKeyFile:       getEnv("MERLON_JWT_PUBLIC_KEY_FILE", ""),
		ConfigPath:             getEnv("MERLON_CONFIG_PATH", "config.yaml"),
		CacheBackend:           getEnv("MERLON_CACHE_BACKEND", "memory"),
		EventBus:               getEnv("MERLON_EVENT_BUS", "pg_notify"),
		LogLevel:               getEnv("MERLON_LOG_LEVEL", "info"),
		AdapterConfigPath:      getEnv("MERLON_ADAPTER_CONFIG_PATH", ""),
		UIDir:                  getEnv("MERLON_UI_DIR", ""),
		RateLimit:              getEnvInt("MERLON_RATE_LIMIT", 0),
		TrustedProxyCIDRs:      trustedProxyCIDRs,
		AuthEnabled:            getEnv("MERLON_AUTH_ENABLED", "") == "true",
		BootstrapToken:         getEnv("MERLON_BOOTSTRAP_TOKEN", ""),
		CountryRiskPath:        getEnv("MERLON_COUNTRY_RISK_PATH", ""),
		TMBaseCurrency:         strings.ToUpper(getEnv("MERLON_TM_BASE_CURRENCY", "JPY")),
		RealtimeMonitorTimeout: getEnvDuration("MERLON_REALTIME_MONITOR_TIMEOUT", 30*time.Second),
		WhitelistMaxValidDays:  getEnvInt("MERLON_WHITELIST_MAX_VALID_DAYS", 365),

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
		OperatorTeams:     getEnvList("MERLON_OPERATOR_TEAMS"),
		CasePriorityPath:  getEnv("MERLON_CASE_PRIORITY_PATH", "content/case_priority_v1.yaml"),
		PublicURL:         getEnv("MERLON_PUBLIC_URL", ""),

		KYCRequiredFieldsPath:  getEnv("MERLON_KYC_REQUIRED_FIELDS_PATH", "content/kyc_required_fields_v1.yaml"),
		EDDPolicyPath:          getEnv("MERLON_EDD_POLICY_PATH", "content/edd_policy_v1.yaml"),
		CDDRuleSelectionPath:   getEnv("MERLON_CDD_RULE_SELECTION_PATH", "content/cdd_rule_selection_v1.yaml"),
		TravelRulePolicyPath:   getEnv("MERLON_TRAVEL_RULE_POLICY_PATH", "content/travel_rule_v1.yaml"),
		ScreeningReadinessPath: getEnv("MERLON_SCREENING_READINESS_PATH", "content/screening_readiness_v1.yaml"),

		EDDStage2Days: getEnvInt("MERLON_EDD_STAGE2_DAYS", 60),
		EDDStage3Days: getEnvInt("MERLON_EDD_STAGE3_DAYS", 90),

		trustedProxyCIDRsErr: trustedProxyCIDRsErr,
	}
}

func parseCIDRs(values []string) ([]netip.Prefix, error) {
	if len(values) == 0 {
		return nil, nil
	}

	prefixes := make([]netip.Prefix, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("MERLON_TRUSTED_PROXY_CIDRS contains invalid CIDR %q: %w", value, err)
		}
		prefix = canonicalPrefix(prefix)
		if prefix.Bits() == 0 {
			return nil, fmt.Errorf("MERLON_TRUSTED_PROXY_CIDRS must not trust an entire address family (%q)", value)
		}
		key := prefix.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

func canonicalPrefix(prefix netip.Prefix) netip.Prefix {
	addr := prefix.Addr()
	if addr.Is4In6() && prefix.Bits() >= 96 {
		return netip.PrefixFrom(addr.Unmap(), prefix.Bits()-96).Masked()
	}
	return prefix.Masked()
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
