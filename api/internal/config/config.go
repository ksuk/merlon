package config

import "os"

type Config struct {
	Env               string
	HTTPAddr          string
	EngineAddr        string
	DatabaseURL       string
	JWTSecret         string
	ConfigPath        string
	CacheBackend      string
	EventBus          string
	LogLevel          string
	AdapterConfigPath string
}

func Load() *Config {
	return &Config{
		Env:          getEnv("MERLON_ENV", "development"),
		HTTPAddr:     getEnv("MERLON_HTTP_ADDR", ":8080"),
		EngineAddr:   getEnv("MERLON_ENGINE_ADDR", ""),
		DatabaseURL:  getEnv("MERLON_DATABASE_URL", "postgres://merlon:merlon@localhost:5432/merlon?sslmode=disable"),
		JWTSecret:    getEnv("MERLON_JWT_SECRET", ""),
		ConfigPath:   getEnv("MERLON_CONFIG_PATH", "config.yaml"),
		CacheBackend: getEnv("MERLON_CACHE_BACKEND", "memory"),
		EventBus:     getEnv("MERLON_EVENT_BUS", "pg_notify"),
		LogLevel:          getEnv("MERLON_LOG_LEVEL", "info"),
		AdapterConfigPath: getEnv("MERLON_ADAPTER_CONFIG_PATH", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
