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
	"github.com/merlon-aml/merlon/api/internal/batch"
	"github.com/merlon-aml/merlon/api/internal/config"
	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/engine"
	"github.com/merlon-aml/merlon/api/internal/engineclient"
	"github.com/merlon-aml/merlon/api/internal/events"
	"github.com/merlon-aml/merlon/api/internal/events/handlers"
	_ "github.com/merlon-aml/merlon/api/internal/events/nats"
	_ "github.com/merlon-aml/merlon/api/internal/events/pgnotify"
	"github.com/merlon-aml/merlon/api/internal/logging"
	"github.com/merlon-aml/merlon/api/internal/screening"
	"github.com/merlon-aml/merlon/api/internal/seed"
	"github.com/merlon-aml/merlon/api/internal/server"
	"github.com/merlon-aml/merlon/api/internal/store"
)

// whitelistExpiryCheckInterval governs how often the ticker checks for
// overdue/soon-to-expire whitelist entries (whitelist.md §2, WL-006). The
// job itself (batch.RunWhitelistExpiryJob) is idempotent, so an hourly
// cadence is safe regardless of exact expiry timing.
const whitelistExpiryCheckInterval = time.Hour

// screeningListIDs are the WS-7 sanctions/PEP lists this deployment
// imports and screens against (screening.md §リスト自動取り込み table).
var screeningListIDs = []string{"ofac_sdn", "eu_sanctions", "un_sc", "mof_japan", "pep_provider"}

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
		deps.Whitelist = store.NewPostgresWhitelistRepo(pool)
		deps.ScreeningResults = store.NewPgScreeningResultRepo(pool)
		deps.PendingEvaluations = store.NewPgPendingEvaluationRepo(pool)
		deps.DB = pool
		slog.Info("database connected", "backend", "postgresql")
	} else {
		deps.Customers = store.NewMemoryCustomerRepo()
		deps.Transactions = store.NewMemoryTransactionRepo()
		deps.Alerts = store.NewMemoryAlertRepo()
		deps.Audit = store.NewMemoryAuditRepo()
		deps.Cases = store.NewMemoryCaseRepo()
		deps.Webhooks = store.NewMemoryWebhookRepo()
		deps.Whitelist = store.NewMemoryWhitelistRepo()
		deps.ScreeningResults = store.NewMemoryScreeningResultRepo()
		deps.PendingEvaluations = store.NewMemoryPendingEvaluationRepo()
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
	deps.WhitelistMaxValidDays = cfg.WhitelistMaxValidDays
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
		// Wrap in a circuit breaker (overview.md §4.4: 3s timeout, 2 retries,
		// 30s open, half-open allows 1 request) so a stalled engine trips
		// instead of leaving every caller blocked on a hung gRPC call.
		cbClient := engineclient.Wrap(client)
		deps.Scoring = cbClient
		deps.Monitoring = cbClient
		deps.Screening = cbClient
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

	jobsCtx, cancelJobs := context.WithCancel(context.Background())
	defer cancelJobs()

	// Event bus (Task 6/7, EVENT_BUS driver selection): pg_notify requires a
	// real PostgreSQL connection, so it's only wired in the Postgres-backed
	// deployment mode. The in-memory dev mode has no event bus (tier-change
	// propagation, Task 8, is a no-op then, same as any other Postgres-only
	// background job in this file).
	if pool != nil {
		bus, err := events.NewBus(events.Config{Driver: cfg.EventBus, Pool: pool})
		if err != nil {
			slog.Error("event bus", "error", err)
			os.Exit(1)
		}
		deps.Events = bus
		tierChangeHandler := handlers.NewTierChangeHandler(deps.Transactions, deps.Monitoring, deps.Alerts)
		if err := bus.Subscribe(jobsCtx, "cdd.tier_changed", tierChangeHandler); err != nil {
			slog.Error("event bus subscribe", "topic", "cdd.tier_changed", "error", err)
			os.Exit(1)
		}
		slog.Info("event bus configured", "driver", cfg.EventBus)
	} else {
		slog.Warn("no PostgreSQL connection, event bus disabled (CDD tier-change propagation, Task 8, will not run)")
	}

	if deps.Whitelist != nil {
		batch.StartExpiryTicker(jobsCtx, deps.Whitelist, whitelistExpiryCheckInterval, func(entries []domain.WhitelistEntry) {
			for _, e := range entries {
				slog.Info("whitelist entry expiring soon", "id", e.ID, "customer_id", e.CustomerID, "valid_until", e.ValidUntil)
			}
		})
	}

	if cfg.ScreeningImportEnabled || cfg.ScreeningRescreenEnabled {
		// Persistent (Postgres-backed) list storage is a future enhancement
		// (WS-7 Task 2 design note); the in-process store is sufficient for
		// the import job's own fail-alert continuity within one running
		// process.
		listStore := screening.NewMemoryListStore()
		failureTracker := screening.NewMemoryFailureTracker()
		deps.ScreeningListStore = listStore
		deps.ScreeningFailureTracker = failureTracker
		deps.ScreeningListIDs = screeningListIDs

		if cfg.ScreeningImportEnabled {
			fetcher := screening.NewDefaultHTTPFetcher(30 * time.Second)
			adapters := map[string]screening.ListAdapter{
				"ofac_sdn":     &screening.OFACAdapter{ListID: "ofac_sdn", URL: cfg.ScreeningOFACURL, Fetcher: fetcher},
				"eu_sanctions": &screening.EUAdapter{ListID: "eu_sanctions", URL: cfg.ScreeningEUURL, Fetcher: fetcher},
				"un_sc":        &screening.UNAdapter{ListID: "un_sc", URL: cfg.ScreeningUNURL, Fetcher: fetcher},
				"mof_japan":    &screening.MOFAdapter{ListID: "mof_japan", URL: cfg.ScreeningMOFURL, Fetcher: fetcher},
				"pep_provider": &screening.PEPAdapter{ListID: "pep_provider", URL: cfg.ScreeningPEPURL, Fetcher: fetcher},
			}
			go screening.RunImportJobPeriodically(jobsCtx, cfg.ScreeningImportInterval, adapters, listStore, failureTracker)
			slog.Info("screening list import job enabled", "interval", cfg.ScreeningImportInterval)
		}

		if cfg.ScreeningRescreenEnabled && deps.Screening != nil {
			scheduler := screening.NewScheduler(screening.SchedulerDeps{
				Customers: deps.Customers,
				Screening: deps.Screening,
				Results:   deps.ScreeningResults,
				ListIDs:   screeningListIDs,
			})
			go scheduler.RunPeriodic(jobsCtx, cfg.ScreeningCheckInterval)
			slog.Info("screening rescreening scheduler enabled", "check_interval", cfg.ScreeningCheckInterval)
		} else if cfg.ScreeningRescreenEnabled {
			slog.Warn("MERLON_SCREENING_RESCREEN_ENABLED=true but no engine configured (MERLON_ENGINE_ADDR), rescreening scheduler disabled")
		}
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

	recoveryCtx, cancelRecovery := context.WithCancel(context.Background())
	defer cancelRecovery()
	if deps.PendingEvaluations != nil && deps.Monitoring != nil {
		recoveryJob := batch.NewRecoveryJob(deps.PendingEvaluations, deps.Monitoring, deps.Alerts, deps.Transactions, deps.Customers)
		go func() {
			if err := recoveryJob.Run(recoveryCtx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("pending evaluation recovery job stopped", "error", err)
			}
		}()
		slog.Info("pending evaluation recovery job started")
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	slog.Info("merlon-api shutting down")
	cancelRecovery()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	slog.Info("merlon-api stopped")
}
