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
	"github.com/ksuk/merlon/api/internal/auth"
	backtestworker "github.com/ksuk/merlon/api/internal/backtest"
	"github.com/ksuk/merlon/api/internal/batch"
	"github.com/ksuk/merlon/api/internal/config"
	"github.com/ksuk/merlon/api/internal/crypto"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine/native"
	"github.com/ksuk/merlon/api/internal/events"
	"github.com/ksuk/merlon/api/internal/events/handlers"
	_ "github.com/ksuk/merlon/api/internal/events/nats"
	_ "github.com/ksuk/merlon/api/internal/events/pgnotify"
	"github.com/ksuk/merlon/api/internal/logging"
	"github.com/ksuk/merlon/api/internal/notify"
	"github.com/ksuk/merlon/api/internal/retention"
	"github.com/ksuk/merlon/api/internal/screening"
	"github.com/ksuk/merlon/api/internal/seed"
	"github.com/ksuk/merlon/api/internal/server"
	"github.com/ksuk/merlon/api/internal/store"
)

// whitelistExpiryCheckInterval governs how often the ticker checks for
// overdue/soon-to-expire whitelist entries (whitelist.md §2, WL-006). The
// job itself (batch.RunWhitelistExpiryJob) is idempotent, so an hourly
// cadence is safe regardless of exact expiry timing.
var version = "dev"

const whitelistExpiryCheckInterval = time.Hour

// webhookRetryCheckInterval governs how often the retry worker polls for
// deliveries whose next_attempt_at is due (the HTTP API contract §3.1). 30s matches the
// shortest possible backoff (attempt 1) so a due retry isn't delayed further
// than necessary.
const webhookRetryCheckInterval = 30 * time.Second

// eddEscalationCheckInterval governs how often RunEDDEscalationJob runs
// (the case-management workflow §EDD未実施継続時の段階的措置). Its finest granularity
// is one calendar day (stage 1 dedup), so hourly is more than sufficient and
// harmless since the job is idempotent.
const eddEscalationCheckInterval = time.Hour

// retentionPurgeSchedule is deliberately separate from transaction
// monitoring so that a retention pass is observable and operationally
// controllable as its own daily job. Concrete purge targets are registered
// only after their logical-delete lifecycle and referential constraints have
// been migrated.
const retentionPurgeSchedule = "03:00"

// screeningListIDs are the WS-7 sanctions/PEP lists this deployment
// imports and screens against (the screening workflow §リスト自動取り込み table).
var screeningListIDs = []string{"ofac_sdn", "eu_sanctions", "un_sc", "mof_japan", "pep_provider"}

func main() {
	server.Version = version
	slog.SetDefault(logging.NewLogger(os.Stdout))

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		slog.Error("config validation", "error", err)
		os.Exit(1)
	}
	runAPIJobs := cfg.Mode == "api" || cfg.Mode == "all"
	runWorkerJobs := cfg.Mode == "worker" || cfg.Mode == "all"

	deps := server.Deps{}
	deps.ConfigDigests = make(map[string]string)
	for name, path := range map[string]string{
		"application":     cfg.ConfigPath,
		"adapter":         cfg.AdapterConfigPath,
		"country_risk":    cfg.CountryRiskPath,
		"tm_scenarios":    os.Getenv("MERLON_TM_SCENARIOS_PATH"),
		"screening_lists": os.Getenv("MERLON_SCREENING_LISTS_PATH"),
	} {
		if path == "" {
			continue
		}
		if digest, err := config.DigestPath(path); err == nil {
			deps.ConfigDigests[name] = digest
		} else {
			slog.Warn("config digest unavailable", "name", name, "path", path, "error", err)
		}
	}

	var batchRuns domain.BatchRunRepository

	// Email notifications (NOTIF-001/NOTIF-003, WS-8 Task 5). A blank
	// SMTPHost disables the mailer entirely rather than attempting an
	// unconfigured SMTP connection.
	if cfg.SMTPHost != "" {
		deps.Notifier = notify.NewMailer(notify.SMTPConfig{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword,
			From:     cfg.SMTPFrom,
			To:       cfg.SMTPTo,
			UseTLS:   cfg.SMTPUseTLS,
		})
		slog.Info("email notifications enabled", "smtp_host", cfg.SMTPHost)
	}
	if cfg.NotifyRoutingPath != "" {
		rules, err := notify.LoadRoutingRules(cfg.NotifyRoutingPath)
		if err != nil {
			slog.Warn("notify routing rules load failed, using defaults", "path", cfg.NotifyRoutingPath, "error", err)
			rules = notify.DefaultRoutingRules()
		}
		deps.RoutingRules = rules
	} else {
		deps.RoutingRules = notify.DefaultRoutingRules()
	}
	deps.PublicURL = cfg.PublicURL

	// PII field encryption (security.md §2.1, WS-11 Task 7). An unset key
	// ring leaves customers.attributes' direct PII fields in plaintext --
	// acceptable for local/dev use but not production.
	var encryptor *crypto.Encryptor
	if cfg.EncryptionKeyRing != "" {
		keyRing, err := crypto.NewKeyRingFromEnv("MERLON_ENCRYPTION_KEY_RING")
		if err != nil {
			slog.Error("encryption key ring", "error", err)
			os.Exit(1)
		}
		encryptor = crypto.NewEncryptor(keyRing)
		slog.Info("customer PII field encryption enabled")
	} else {
		slog.Warn("MERLON_ENCRYPTION_KEY_RING not set, customer PII fields will be stored in plaintext")
	}

	var pool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
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
		if cfg.Env == "production" {
			if err := store.VerifyAuditLogPrivileges(context.Background(), pool); err != nil {
				slog.Error("database audit privilege preflight failed", "error", err)
				os.Exit(1)
			}
		}

		deps.Customers = store.NewPgCustomerRepo(pool, encryptor)
		deps.Transactions = store.NewPgTransactionRepo(pool)
		deps.Alerts = store.NewPgAlertRepo(pool)
		deps.Audit = store.NewPgAuditRepo(pool)
		deps.Cases = store.NewPgCaseRepo(pool)
		deps.Webhooks = store.NewMemoryWebhookRepo()
		deps.Whitelist = store.NewPostgresWhitelistRepo(pool)
		deps.ScreeningResults = store.NewPgScreeningResultRepo(pool)
		deps.PendingEvaluations = store.NewPgPendingEvaluationRepo(pool)
		deps.Retention = store.NewPgRetentionRepo(pool)
		deps.Accounts = store.NewPgAccountRepo(pool)
		deps.Rules = store.NewPostgresRuleRepo(pool)
		deps.DB = pool
		batchRuns = store.NewPgBatchRunRepo(pool)
		deps.BacktestJobs = store.NewPgBacktestJobRepo(pool)
		slog.Info("database connected", "backend", "postgresql")
	} else {
		memCustomers := store.NewMemoryCustomerRepo()
		deps.Customers = memCustomers
		deps.Transactions = store.NewMemoryTransactionRepo()
		deps.Alerts = store.NewMemoryAlertRepo()
		deps.Audit = store.NewMemoryAuditRepo()
		deps.Cases = store.NewMemoryCaseRepo()
		deps.Webhooks = store.NewMemoryWebhookRepo()
		deps.Whitelist = store.NewMemoryWhitelistRepo()
		deps.ScreeningResults = store.NewMemoryScreeningResultRepo()
		deps.PendingEvaluations = store.NewMemoryPendingEvaluationRepo()
		deps.Retention = store.NewMemoryRetentionRepo()
		deps.Accounts = store.NewMemoryAccountRepo(memCustomers)
		deps.Rules = store.NewMemoryRuleRepo()
		batchRuns = store.NewMemoryBatchRunRepo()
		deps.BacktestJobs = store.NewMemoryBacktestJobRepo()
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
	deps.TMBaseCurrency = cfg.TMBaseCurrency
	if cfg.RateLimit > 0 {
		slog.Info("rate limit configured", "requests_per_minute", cfg.RateLimit)
	}

	// PH9 native mode is the sole production engine path after Go
	// consolidation. A deployment may intentionally run without rule roots
	// during setup/migrations; engine-backed endpoints remain disabled then.
	if nativeEngine, nativeErr := native.NewFromEnv(); nativeErr == nil {
		deps.Scoring = nativeEngine
		deps.Monitoring = nativeEngine
		deps.Screening = nativeEngine
		deps.Backtest = nativeEngine
		deps.Config = nativeEngine
		deps.EngineHealth = nativeEngine
		slog.Info("native Go engine loaded", "tm_digest", deps.ConfigDigests["tm_scenarios"])
	} else {
		slog.Warn("native Go engine unavailable", "error", nativeErr)
	}

	if cfg.Seed {
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
	if runWorkerJobs && pool != nil {
		bus, err := events.NewBus(events.Config{Driver: cfg.EventBus, Pool: pool})
		if err != nil {
			slog.Error("event bus", "error", err)
			os.Exit(1)
		}
		deps.Events = bus
		tierChangeHandler := handlers.NewTierChangeHandler(deps.Transactions, deps.Monitoring, deps.Alerts, deps.Cases)
		if err := bus.Subscribe(jobsCtx, "cdd.tier_changed", tierChangeHandler); err != nil {
			slog.Error("event bus subscribe", "topic", "cdd.tier_changed", "error", err)
			os.Exit(1)
		}
		slog.Info("event bus configured", "driver", cfg.EventBus)
	} else {
		slog.Warn("no PostgreSQL connection, event bus disabled (CDD tier-change propagation, Task 8, will not run)")
	}

	if runAPIJobs && deps.Whitelist != nil {
		batch.StartExpiryTicker(jobsCtx, deps.Whitelist, whitelistExpiryCheckInterval, func(entries []domain.WhitelistEntry) {
			for _, e := range entries {
				slog.Info("whitelist entry expiring soon", "id", e.ID, "customer_id", e.CustomerID, "valid_until", e.ValidUntil)
			}
		})
	}

	if runAPIJobs && (cfg.ScreeningImportEnabled || cfg.ScreeningRescreenEnabled) {
		var listStore screening.ListStore
		var failureTracker screening.FailureTracker
		if pool != nil {
			listStore = screening.NewPostgresListStore(pool)
			failureTracker = screening.NewPostgresFailureTracker(pool)
		} else {
			listStore = screening.NewMemoryListStore()
			failureTracker = screening.NewMemoryFailureTracker()
		}
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
			var listConsumer interface{ ReplaceScreeningLists([]screening.RawListData) }
			if consumer, ok := deps.Screening.(interface{ ReplaceScreeningLists([]screening.RawListData) }); ok {
				listConsumer = consumer
			}
			go screening.RunImportJobPeriodicallyWithConsumer(jobsCtx, cfg.ScreeningImportInterval, adapters, listStore, failureTracker, listConsumer)
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
			slog.Warn("MERLON_SCREENING_RESCREEN_ENABLED=true but native engine is unavailable, rescreening scheduler disabled")
		}
	}

	listenAddr := cfg.HTTPAddr
	if cfg.Mode == "worker" {
		listenAddr = cfg.WorkerHTTPAddr
	}
	srv := server.New(listenAddr, deps)

	if runAPIJobs && deps.Customers != nil && deps.Cases != nil {
		batch.StartEDDEscalationTicker(jobsCtx, batch.EDDEscalationDeps{
			Customers:  deps.Customers,
			Cases:      deps.Cases,
			Webhook:    srv.DispatchWebhook,
			Stage2Days: cfg.EDDStage2Days,
			Stage3Days: cfg.EDDStage3Days,
		}, eddEscalationCheckInterval)
		slog.Info("EDD escalation job enabled", "stage2_days", cfg.EDDStage2Days, "stage3_days", cfg.EDDStage3Days)
	}

	if runAPIJobs && deps.Retention != nil && deps.Audit != nil {
		targets := map[string]retention.PurgeFunc{}
		if pool != nil {
			purger := retention.NewPostgresPurger(pool)
			targets = map[string]retention.PurgeFunc{
				"customer_data":     purger.CustomerData,
				"transaction_data":  purger.Transactions,
				"alert_case_data":   purger.AlertCaseData,
				"cdd_score_history": purger.ScoreHistory,
				"audit_log":         purger.AuditLogs,
			}
		}
		purgeJob := &retention.PurgeJob{
			Retention: deps.Retention,
			Audit:     deps.Audit,
			Targets:   targets,
		}
		purgeScheduler := batch.NewScheduler(retentionPurgeSchedule, func(ctx context.Context, _ string) error {
			results, err := purgeJob.Run(ctx, time.Now().UTC())
			if err != nil {
				return err
			}
			for _, result := range results {
				slog.Info("retention purge completed", "category", result.Category,
					"logically_deleted", result.LogicallyDeleted, "physically_deleted", result.PhysicallyDeleted)
			}
			return nil
		})
		go purgeScheduler.Start(jobsCtx)
		slog.Info("retention purge scheduler started", "schedule", retentionPurgeSchedule, "targets", len(targets))
	}

	if cfg.UIDir != "" {
		srv.SetUIDir(cfg.UIDir)
		slog.Info("serving UI", "dir", cfg.UIDir)
	}

	httpServer := &http.Server{
		Addr:    listenAddr,
		Handler: srv.Handler(),
	}

	go func() {
		slog.Info("merlon-api starting", "env", cfg.Env, "mode", cfg.Mode, "addr", listenAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	recoveryCtx, cancelRecovery := context.WithCancel(context.Background())
	defer cancelRecovery()

	webhookRetryCtx, cancelWebhookRetry := context.WithCancel(context.Background())
	defer cancelWebhookRetry()
	if runAPIJobs && deps.Webhooks != nil {
		go srv.RunWebhookRetryWorker(webhookRetryCtx, webhookRetryCheckInterval)
		slog.Info("webhook retry worker started", "interval", webhookRetryCheckInterval)
	}

	if runWorkerJobs && deps.PendingEvaluations != nil && deps.Monitoring != nil {
		recoveryJob := batch.NewRecoveryJob(deps.PendingEvaluations, deps.Monitoring, deps.Alerts, deps.Transactions, deps.Customers)
		recoveryJob.ConfigDigests = deps.ConfigDigests
		go func() {
			if err := recoveryJob.Run(recoveryCtx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("pending evaluation recovery job stopped", "error", err)
			}
		}()
		slog.Info("pending evaluation recovery job started")
	}

	backtestCtx, cancelBacktest := context.WithCancel(context.Background())
	defer cancelBacktest()
	if runWorkerJobs && deps.BacktestJobs != nil && deps.Backtest != nil {
		for i := 0; i < cfg.WorkerConcurrency; i++ {
			worker := &backtestworker.Worker{Jobs: deps.BacktestJobs, Customers: deps.Customers, Transactions: deps.Transactions, Engine: deps.Backtest, Rules: deps.Rules}
			workerID := i + 1
			go func() {
				if err := worker.Run(backtestCtx, time.Second); err != nil && !errors.Is(err, context.Canceled) {
					slog.Error("backtest worker stopped", "worker_id", workerID, "error", err)
				}
			}()
		}
		slog.Info("durable backtest workers started", "concurrency", cfg.WorkerConcurrency)
	}

	tmBatchCtx, cancelTMBatch := context.WithCancel(context.Background())
	defer cancelTMBatch()
	if runWorkerJobs && deps.Monitoring != nil {
		tmScheduler := batch.NewScheduler(cfg.TMBatchSchedule, func(ctx context.Context, runID string) error {
			return batch.RunTMBatchEvaluation(ctx, batch.TMBatchEvaluationDeps{
				Runs:          batchRuns,
				Customers:     deps.Customers,
				Transactions:  deps.Transactions,
				Monitoring:    deps.Monitoring,
				Alerts:        deps.Alerts,
				Cases:         deps.Cases,
				ConfigDigests: deps.ConfigDigests,
			}, runID)
		})
		if cfg.TMBatchTimezone != "" {
			if loc, err := time.LoadLocation(cfg.TMBatchTimezone); err == nil {
				tmScheduler.Location = loc
			} else {
				slog.Warn("invalid MERLON_TM_BATCH_TIMEZONE, falling back to time.Local", "value", cfg.TMBatchTimezone, "error", err)
			}
		}

		// Resume a run left behind by a killed process immediately, rather
		// than waiting for the next scheduled time (the operational design §4.4「再起動時
		// は未処理分のみを再開」).
		if existing, err := batchRuns.GetLatestRunning(tmBatchCtx, batch.TMBatchEvaluationJobType); err == nil && existing != nil {
			slog.Info("resuming interrupted TM batch evaluation run", "batch_run_id", existing.ID)
			go func() {
				if _, err := tmScheduler.RunNow(tmBatchCtx); err != nil {
					slog.Error("TM batch evaluation resume failed", "error", err)
				}
			}()
		}

		go tmScheduler.Start(tmBatchCtx)
		slog.Info("TM batch evaluation scheduler started", "schedule", cfg.TMBatchSchedule)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	slog.Info("merlon-api shutting down")
	cancelRecovery()
	cancelTMBatch()
	cancelBacktest()
	cancelWebhookRetry()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	slog.Info("merlon-api stopped")
}
