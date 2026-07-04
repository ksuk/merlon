package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/merlon-aml/merlon/api/internal/auth"
	"github.com/merlon-aml/merlon/api/internal/config"
	"github.com/merlon-aml/merlon/api/internal/engine"
	"github.com/merlon-aml/merlon/api/internal/seed"
	"github.com/merlon-aml/merlon/api/internal/server"
	"github.com/merlon-aml/merlon/api/internal/store"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config validation: %v", err)
	}

	deps := server.Deps{}

	var pool *pgxpool.Pool
	if os.Getenv("MERLON_DATABASE_URL") != "" {
		var err error
		pool, err = pgxpool.New(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("database connection: %v", err)
		}
		defer pool.Close()

		if err := pool.Ping(context.Background()); err != nil {
			log.Fatalf("database ping: %v", err)
		}

		deps.Customers = store.NewPgCustomerRepo(pool)
		deps.Transactions = store.NewPgTransactionRepo(pool)
		deps.Alerts = store.NewPgAlertRepo(pool)
		deps.Audit = store.NewPgAuditRepo(pool)
		deps.Cases = store.NewPgCaseRepo(pool)
		deps.Webhooks = store.NewMemoryWebhookRepo()
		log.Printf("database connected: PostgreSQL")
	} else {
		deps.Customers = store.NewMemoryCustomerRepo()
		deps.Transactions = store.NewMemoryTransactionRepo()
		deps.Alerts = store.NewMemoryAlertRepo()
		deps.Audit = store.NewMemoryAuditRepo()
		deps.Cases = store.NewMemoryCaseRepo()
		deps.Webhooks = store.NewMemoryWebhookRepo()
		log.Printf("using in-memory store (set MERLON_DATABASE_URL for PostgreSQL)")
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
		log.Printf("API key authentication enabled")

		switch {
		case cfg.JWTPrivateKeyFile != "" && cfg.JWTPublicKeyFile != "":
			issuer, err := auth.NewRS256Issuer(cfg.JWTPrivateKeyFile, cfg.JWTPublicKeyFile)
			if err != nil {
				log.Fatalf("jwt issuer: %v", err)
			}
			deps.TokenIssuer = issuer
			log.Printf("JWT session authentication enabled (RS256)")
		case cfg.JWTSecret != "":
			issuer, err := auth.NewHS256Issuer(cfg.JWTSecret)
			if err != nil {
				log.Fatalf("jwt issuer: %v", err)
			}
			deps.TokenIssuer = issuer
			log.Printf("warning: JWT session authentication enabled with HS256/MERLON_JWT_SECRET (development only; set MERLON_JWT_PRIVATE_KEY_FILE/MERLON_JWT_PUBLIC_KEY_FILE for production)")
		default:
			log.Printf("warning: no JWT signing key configured; local user login (email/password) is disabled, API key authentication is still available")
		}
	}

	deps.RateLimit = cfg.RateLimit
	if cfg.RateLimit > 0 {
		log.Printf("rate limit: %d req/min", cfg.RateLimit)
	}

	if cfg.EngineAddr != "" {
		var engineOpts []engine.ClientOption
		if cfg.EngineTLSCert != "" {
			engineOpts = append(engineOpts, engine.WithTLS(cfg.EngineTLSCert, cfg.EngineTLSServerName))
		}
		client, err := engine.NewClient(cfg.EngineAddr, engineOpts...)
		if err != nil {
			log.Fatalf("engine client: %v", err)
		}
		defer client.Close()
		deps.Scoring = client
		deps.Monitoring = client
		deps.Screening = client
		deps.Backtest = client
		deps.Config = client
		deps.EngineHealth = client
		log.Printf("engine connected: %s", cfg.EngineAddr)
	} else {
		log.Printf("warning: MERLON_ENGINE_ADDR not set, engine endpoints disabled")
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
		log.Printf("serving UI from: %s", cfg.UIDir)
	}

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: srv.Handler(),
	}

	go func() {
		log.Printf("merlon-api starting env=%s addr=%s", cfg.Env, cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Printf("merlon-api shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("graceful shutdown failed: %v", err)
	}

	log.Printf("merlon-api stopped")
}
