package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Env                 string
	HTTPAddr            string
	EngineAddr          string
	DatabaseURL         string
	JWTSecret           string
	JWTPrivateKeyFile   string
	JWTPublicKeyFile    string
	ConfigPath          string
	CacheBackend        string
	EventBus            string
	LogLevel            string
	AdapterConfigPath   string
	UIDir               string
	RateLimit           int
	AuthEnabled         bool
	BootstrapToken      string
	EngineTLSCert       string
	EngineTLSServerName string
	// WhitelistMaxValidDays is the maximum whitelist validity period (WL-002,
	// whitelist.md §要件表: "最大有効期間はシステム設定で制御可能（デフォルト：1年）").
	// TODO(WS-2): move to the rule management API once it supports
	// system-level settings, rather than an env var.
	WhitelistMaxValidDays int
}

func (c *Config) Validate() error {
	if c.Env == "production" && !c.AuthEnabled {
		return fmt.Errorf("MERLON_AUTH_ENABLED must be true in production")
	}
	return nil
}

func Load() *Config {
	return &Config{
		Env:                   getEnv("MERLON_ENV", "development"),
		HTTPAddr:              getEnv("MERLON_HTTP_ADDR", ":8080"),
		EngineAddr:            getEnv("MERLON_ENGINE_ADDR", ""),
		DatabaseURL:           getEnv("MERLON_DATABASE_URL", "postgres://merlon:merlon@localhost:5432/merlon?sslmode=disable"),
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
	}
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
