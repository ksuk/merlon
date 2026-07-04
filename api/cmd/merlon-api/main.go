package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/merlon-aml/merlon/api/internal/auth"
	"github.com/merlon-aml/merlon/api/internal/config"
	"github.com/merlon-aml/merlon/api/internal/engine"
	"github.com/merlon-aml/merlon/api/internal/logging"
	"github.com/merlon-aml/merlon/api/internal/seed"
	"github.com/merlon-aml/merlon/api/internal/server"
	"github.com/merlon-aml/merlon/api/internal/store"
)

func main() {
	slog.SetDefault(logging.NewLogger(os.Stdout))

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		slog.Error("config validation", "error", err)
		os.Exit(1)
	}

	deps := server.Deps{}

	var pool *pgxpool.Pool
	if os.Getenv("MERLON_DATABASE_URL") != "" {
		var err error
		pool, err = pgxpool.New(context.Background(), cfg.DatabaseURL)
		if err != nil {
			slog.Error("database connection", "error", err)
			os.Exit(1)
		}
		defer pool.Close()

		if err := pool.Ping(context.Background()); err != nil {
			slog.Error("database ping", "error", err)
			os.Exit(1)
		}

		deps.Customers = store.NewPgCustomerRepo(pool)
		deps.Transactions = store.NewPgTransactionRepo(pool)
		deps.Alerts = store.NewPgAlertRepo(pool)
		deps.Audit = store.NewPgAuditRepo(pool)
		deps.Cases = store.NewPgCaseRepo(pool)
		deps.Webhooks = store.NewMemoryWebhookRepo()
		deps.ScreeningResults = store.NewPgScreeningResultRepo(pool)
		deps.DB = pool
		slog.Info("database connected", "backend", "postgresql")
	} else {
		deps.Customers = store.NewMemoryCustomerRepo()
		deps.Transactions = store.NewMemoryTransactionRepo()
		deps.Alerts = store.NewMemoryAlertRepo()
		deps.Audit = store.NewMemoryAuditRepo()
		deps.Cases = store.NewMemoryCaseRepo()
		deps.Webhooks = store.NewMemoryWebhookRepo()
		deps.ScreeningResults = store.NewMemoryScreeningResultRepo()
		slog.Info("using in-memory store (set MERLON_DATABASE_URL for PostgreSQL)")
	}

	if cfg.AuthEnabled {
		if pool != nil {
			deps.APIKeys = store.NewPgAPIKeyRepo(pool)
			deps.Users = store.NewPgUserRepo(pool)
			deps.RefreshTokens = store.NewPgRefreshTokenRepo(pool)
		} else {
			deps.APIKeys = store.NewMemoryAPIKeyRepo()
			deps.Users = store.NewMemoryUserRepo()
			deps.RefreshTokens = store.NewMemoryRefreshTokenRepo()
		}
		deps.BootstrapToken = cfg.BootstrapToken
		deps.Denylist = auth.NewInMemoryDenylist()
		slog.Info("API key authentication enabled")

		switch {
		case cfg.JWTPrivateKeyFile != "" && cfg.JWTPublicKeyFile != "":
			issuer, err := auth.NewRS256Issuer(cfg.JWTPrivateKeyFile, cfg.JWTPublicKeyFile)
			if err != nil {
				slog.Error("jwt issuer", "error", err)
				os.Exit(1)
			}
			deps.TokenIssuer = issuer
			slog.Info("JWT session authentication enabled", "algorithm", "RS256")
		case cfg.JWTSecret != "":
			issuer, err := auth.NewHS256Issuer(cfg.JWTSecret)
			if err != nil {
				slog.Error("jwt issuer", "error", err)
				os.Exit(1)
			}
			deps.TokenIssuer = issuer
			slog.Warn("JWT session authentication enabled with HS256/MERLON_JWT_SECRET (development only; set MERLON_JWT_PRIVATE_KEY_FILE/MERLON_JWT_PUBLIC_KEY_FILE for production)")
		default:
			slog.Warn("no JWT signing key configured; local user login (email/password) is disabled, API key authentication is still available")
		}
	}

	deps.RateLimit = cfg.RateLimit
	if cfg.RateLimit > 0 {
		slog.Info("rate limit configured", "requests_per_minute", cfg.RateLimit)
	}

	if cfg.EngineAddr != "" {
		var engineOpts []engine.ClientOption
		if cfg.EngineTLSCert != "" {
			engineOpts = append(engineOpts, engine.WithTLS(cfg.EngineTLSCert, cfg.EngineTLSServerName))
		}
		client, err := engine.NewClient(cfg.EngineAddr, engineOpts...)
		if err != nil {
			slog.Error("engine client", "error", err)
			os.Exit(1)
		}
		defer client.Close()
		deps.Scoring = client
		deps.Monitoring = client
		deps.Screening = client
		deps.Backtest = client
		deps.Config = client
		deps.EngineHealth = client
		slog.Info("engine connected", "addr", cfg.EngineAddr)
	} else {
		slog.Warn("MERLON_ENGINE_ADDR not set, engine endpoints disabled")
	}

	if os.Getenv("MERLON_SEED") == "true" {
		seed.Run(context.Background(), seed.Repos{
			Customers:    deps.Customers,
			Transactions: deps.Transactions,
			Alerts:       deps.Alerts,
			Cases:        deps.Cases,
			Audit:        deps.Audit,
		})
	}

	srv := server.New(cfg.HTTPAddr, deps)

	if cfg.UIDir != "" {
		srv.SetUIDir(cfg.UIDir)
		slog.Info("serving UI", "dir", cfg.UIDir)
	}

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: srv.Handler(),
	}

	go func() {
		slog.Info("merlon-api starting", "env", cfg.Env, "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	slog.Info("merlon-api shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	slog.Info("merlon-api stopped")
}
