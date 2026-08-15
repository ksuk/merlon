package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	adapterpkg "github.com/ksuk/merlon/api/internal/adapter"
	"github.com/ksuk/merlon/api/internal/auth"
	backtestworker "github.com/ksuk/merlon/api/internal/backtest"
	"github.com/ksuk/merlon/api/internal/batch"
	"github.com/ksuk/merlon/api/internal/casemgmt"
	"github.com/ksuk/merlon/api/internal/config"
	"github.com/ksuk/merlon/api/internal/crypto"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine/native"
	"github.com/ksuk/merlon/api/internal/events"
	"github.com/ksuk/merlon/api/internal/events/handlers"
	_ "github.com/ksuk/merlon/api/internal/events/nats"
	pgnotifypkg "github.com/ksuk/merlon/api/internal/events/pgnotify"
	"github.com/ksuk/merlon/api/internal/logging"
	"github.com/ksuk/merlon/api/internal/notify"
	"github.com/ksuk/merlon/api/internal/policy"
	"github.com/ksuk/merlon/api/internal/retention"
	"github.com/ksuk/merlon/api/internal/screening"
	"github.com/ksuk/merlon/api/internal/seed"
	"github.com/ksuk/merlon/api/internal/server"
	"github.com/ksuk/merlon/api/internal/store"
	inboundwebhook "github.com/ksuk/merlon/api/internal/webhook"
)

const whitelistExpiryCheckInterval = time.Hour

// webhookRetryCheckInterval governs how often the retry worker polls for
// deliveries whose next_attempt_at is due (the HTTP API contract §3.1). 30s matches the
// shortest possible backoff (attempt 1) so a due retry isn't delayed further
// than necessary.
const webhookRetryCheckInterval = 30 * time.Second

const eventOutboxCheckInterval = time.Second

const (
	httpReadHeaderTimeout = 10 * time.Second
	httpReadTimeout       = 30 * time.Second
	httpWriteTimeout      = 5 * time.Minute
	httpIdleTimeout       = 2 * time.Minute
)

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

// inboundPayloadCipher adapts the existing field-level key-ring encryptor to
// webhook.Service's string boundary.  A nil encryptor is intentionally not
// installed; the service then uses its memory-safe ephemeral cipher.
type inboundPayloadCipher struct{ encryptor *crypto.Encryptor }

func (c inboundPayloadCipher) Encrypt(plaintext string) (string, error) {
	return c.encryptor.Encrypt(plaintext)
}

func (c inboundPayloadCipher) Decrypt(ciphertext string) (string, error) {
	return c.encryptor.Decrypt(ciphertext)
}

func runAdapterSyncPeriodically(ctx context.Context, service *adapterpkg.SyncService, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	for {
		if _, err := service.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("adapter sync failed", "error", err)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// startEventSubscription waits until the transport has completed its initial
// subscription handshake. Subscribe remains blocking after that point, so
// callers receive the returned channel to observe a later disconnect.
func startEventSubscription(ctx context.Context, bus events.Bus, topic string, handler func(events.Event)) (<-chan error, error) {
	readyBus, ok := bus.(events.ReadyBus)
	if !ok {
		return nil, errors.New("event bus does not support subscription readiness")
	}
	ready := make(chan struct{})
	errs := make(chan error, 1)
	var readyOnce sync.Once

	go func() {
		errs <- readyBus.SubscribeReady(ctx, topic, handler, func() {
			readyOnce.Do(func() { close(ready) })
		})
	}()

	select {
	case <-ready:
		return errs, nil
	case err := <-errs:
		if err == nil {
			return nil, errors.New("event subscription exited before initialization completed")
		}
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func seedReposFromDeps(deps server.Deps, state domain.SeedStateRepository) seed.Repos {
	return seed.Repos{
		Customers:        deps.Customers,
		Transactions:     deps.Transactions,
		Alerts:           deps.Alerts,
		Cases:            deps.Cases,
		Audit:            deps.Audit,
		Accounts:         deps.Accounts,
		ScreeningResults: deps.ScreeningResults,
		Rules:            deps.Rules,
		State:            state,
	}
}

func runSeed(ctx context.Context, pool *pgxpool.Pool, encryptor *crypto.Encryptor, deps server.Deps) (seed.Result, error) {
	if pool == nil {
		return seed.Run(ctx, seedReposFromDeps(deps, store.NewMemorySeedStateRepo()))
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return seed.Result{}, fmt.Errorf("seed: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := seed.Run(ctx, seed.Repos{
		Customers:        store.NewPgCustomerRepo(tx, encryptor),
		Transactions:     store.NewPgTransactionRepo(tx),
		Alerts:           store.NewPgAlertRepo(tx),
		Cases:            store.NewPgCaseRepo(tx),
		Audit:            store.NewPgAuditRepo(tx),
		Accounts:         store.NewPgAccountRepo(tx),
		ScreeningResults: store.NewPgScreeningResultRepo(tx),
		Rules:            store.NewPostgresRuleRepo(tx),
		State:            store.NewPgSeedStateRepo(tx),
	})
	if err != nil {
		return seed.Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return seed.Result{}, fmt.Errorf("seed: commit transaction: %w", err)
	}
	return result, nil
}

func main() {
	slog.SetDefault(logging.NewLogger(os.Stdout))

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		slog.Error("config validation", "error", err)
		os.Exit(1)
	}
	var configuredAdapter adapterpkg.Adapter
	var configuredAdapterConfig *adapterpkg.AdapterConfig
	if cfg.AdapterConfigPath != "" {
		adapterConfig, adapterErr := adapterpkg.LoadAdapterConfig(cfg.AdapterConfigPath)
		if adapterErr == nil {
			adapterErr = adapterConfig.ValidateSync()
		}
		if adapterErr != nil {
			slog.Error("adapter configuration is unusable", "path", cfg.AdapterConfigPath, "error", adapterErr)
			os.Exit(1)
		}
		configuredAdapterConfig = adapterConfig
		configuredAdapter, adapterErr = adapterpkg.NewRESTAdapter(adapterConfig, adapterpkg.SecurityConfig{BlockPrivateIPRanges: cfg.Env == "production"})
		if adapterErr != nil {
			slog.Error("adapter initialization", "error", adapterErr)
			os.Exit(1)
		}
		slog.Info("adapter configuration loaded", "path", cfg.AdapterConfigPath, "interval", adapterConfig.Sync.Interval, "page_size", adapterConfig.Sync.PageSize)
	}
	runAPIJobs := cfg.Mode == "api" || cfg.Mode == "all"
	runWorkerJobs := cfg.Mode == "worker" || cfg.Mode == "all"

	deps := server.Deps{}
	deps.Adapter = configuredAdapter
	deps.ConfigDigests = make(map[string]string)
	deps.EDDStage2Days = cfg.EDDStage2Days
	deps.EDDStage3Days = cfg.EDDStage3Days
	priorityPolicy, err := casemgmt.LoadPriorityPolicy(cfg.CasePriorityPath)
	if err != nil {
		slog.Error("case priority policy", "error", err)
		os.Exit(1)
	}
	deps.CasePriorityPolicy = priorityPolicy

	// The Wave 3 policy documents (ADR-0016). A policy the operator meant to
	// apply but that cannot be parsed is a configuration error: exiting is
	// safer than scoring customers under rules nobody chose.
	policies, err := policy.Load(policy.Paths{
		KYCRequiredFields:  cfg.KYCRequiredFieldsPath,
		EDD:                cfg.EDDPolicyPath,
		CDDRuleSelection:   cfg.CDDRuleSelectionPath,
		CDDReview:          cfg.CDDReviewPolicyPath,
		TravelRule:         cfg.TravelRulePolicyPath,
		ScreeningReadiness: cfg.ScreeningReadinessPath,
		SLA:                cfg.SLAPolicyPath,
	})
	if err != nil {
		slog.Error("policy", "error", err)
		os.Exit(1)
	}
	deps.Policies = policies
	// The EDD policy file is the single source for the stage schedule. The
	// legacy environment variables still parse so an existing deployment
	// starts, but a non-default value that the file overrides is announced
	// rather than silently ignored.
	if stage2, ok := policies.EDD().StageDays("stage2"); ok && cfg.EDDStage2Days != stage2 {
		slog.Warn("MERLON_EDD_STAGE2_DAYS is superseded by the EDD policy file",
			"env", cfg.EDDStage2Days, "policy", stage2, "path", cfg.EDDPolicyPath)
	}
	if stage3, ok := policies.EDD().StageDays("stage3"); ok && cfg.EDDStage3Days != stage3 {
		slog.Warn("MERLON_EDD_STAGE3_DAYS is superseded by the EDD policy file",
			"env", cfg.EDDStage3Days, "policy", stage3, "path", cfg.EDDPolicyPath)
	}
	for name, digest := range policies.Digests() {
		deps.ConfigDigests[name] = digest
	}

	for name, path := range map[string]string{
		"application":       cfg.ConfigPath,
		"adapter":           cfg.AdapterConfigPath,
		"country_risk":      cfg.CountryRiskPath,
		"tm_scenarios":      os.Getenv("MERLON_TM_SCENARIOS_PATH"),
		"screening_lists":   os.Getenv("MERLON_SCREENING_LISTS_PATH"),
		"case_priority":     cfg.CasePriorityPath,
		"cdd_review_policy": cfg.CDDReviewPolicyPath,
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
	deps.OperatorTeams = append([]string(nil), cfg.OperatorTeams...)

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
		deps.Reports = store.NewPgSTRReportRepo(pool)
		deps.Audit = store.NewPgAuditRepo(pool)
		deps.Cases = store.NewPgCaseRepo(pool)
		deps.CaseAlertLifecycle = store.NewPgCaseAlertLifecycleRepo(pool)
		deps.CaseInvestigation = store.NewPgCaseInvestigationRepo(pool)
		deps.AlertDecisions = store.NewPgAlertDecisionRepo(pool)
		deps.Atomic = store.NewPgAtomicMutationRepo(pool)
		deps.EventOutbox = store.NewPgEventOutboxRepo(pool)
		deps.Webhooks = store.NewPgWebhookRepo(pool, encryptor)
		inboundConfig := inboundwebhook.Config{
			Repository: store.NewPgInboundWebhookRepo(pool), Secret: []byte(cfg.InboundWebhookSecret),
		}
		if encryptor != nil {
			inboundConfig.Cipher = inboundPayloadCipher{encryptor: encryptor}
		}
		deps.InboundWebhooks = inboundwebhook.NewServiceWithConfig(inboundConfig)
		deps.Whitelist = store.NewPostgresWhitelistRepo(pool)
		deps.ScreeningResults = store.NewPgScreeningResultRepo(pool)
		deps.PendingEvaluations = store.NewPgPendingEvaluationRepo(pool)
		deps.Retention = store.NewPgRetentionRepo(pool)
		deps.Accounts = store.NewPgAccountRepo(pool)
		deps.Rules = store.NewPostgresRuleRepo(pool)
		deps.DB = pool
		batchRuns = store.NewPgBatchRunRepo(pool)
		deps.BacktestJobs = store.NewPgBacktestJobRepo(pool)
		deps.Wave3 = store.NewPgWave3Repo(pool)
		slog.Info("database connected", "backend", "postgresql")
	} else {
		memCustomers := store.NewMemoryCustomerRepo()
		memAlerts := store.NewMemoryAlertRepo()
		memCases := store.NewMemoryCaseRepo()
		deps.Customers = memCustomers
		deps.Transactions = store.NewMemoryTransactionRepo()
		deps.Alerts = memAlerts
		deps.Reports = store.NewMemorySTRReportRepo()
		deps.Audit = store.NewMemoryAuditRepo()
		deps.Cases = memCases
		deps.CaseAlertLifecycle = store.NewMemoryCaseAlertLifecycleRepo(memCases, memAlerts)
		deps.CaseInvestigation = store.NewMemoryCaseInvestigationRepo()
		deps.AlertDecisions = store.NewMemoryAlertDecisionRepo()
		deps.EventOutbox = store.NewMemoryEventOutboxRepo()
		memoryWave3 := store.NewMemoryWave3Repo()
		memoryPending := store.NewMemoryPendingEvaluationRepo()
		memoryBatch := store.NewMemoryBatchRunRepo()
		memoryBacktest := store.NewMemoryBacktestJobRepo()
		deps.BacktestJobs = memoryBacktest
		memoryAtomic, atomicErr := store.NewMemoryAtomicMutationRepo(domain.AtomicMutationRepositories{
			Customers: deps.Customers, Transactions: deps.Transactions, Alerts: deps.Alerts,
			Reports: deps.Reports, Audit: deps.Audit, Cases: deps.Cases,
			CaseAlertLifecycle: deps.CaseAlertLifecycle, Investigation: deps.CaseInvestigation,
			AlertDecisions: deps.AlertDecisions, EventOutbox: deps.EventOutbox,
			IdentityHistory: memoryWave3, Wave3: memoryWave3,
			PendingEvaluations: memoryPending, BatchRuns: memoryBatch, BacktestJobs: memoryBacktest,
		})
		if atomicErr != nil {
			slog.Error("memory atomic mutation repository initialization failed", "error", atomicErr)
			os.Exit(1)
		}
		deps.Atomic = memoryAtomic
		deps.Webhooks = store.NewMemoryWebhookRepo()
		deps.InboundWebhooks = inboundwebhook.NewServiceWithConfig(inboundwebhook.Config{
			Repository: store.NewMemoryInboundWebhookRepo(), Secret: []byte(cfg.InboundWebhookSecret),
		})
		deps.Whitelist = store.NewMemoryWhitelistRepo()
		deps.ScreeningResults = store.NewMemoryScreeningResultRepo()
		deps.PendingEvaluations = memoryPending
		deps.Retention = store.NewMemoryRetentionRepo()
		deps.Accounts = store.NewMemoryAccountRepo(memCustomers)
		deps.Rules = store.NewMemoryRuleRepo()
		batchRuns = memoryBatch
		deps.BacktestJobs = memoryBacktest
		deps.Wave3 = memoryWave3
		slog.Info("using in-memory store (set MERLON_DATABASE_URL for PostgreSQL)")
	}
	deps.BatchRuns = batchRuns

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
	deps.TrustedProxyCIDRs = cfg.TrustedProxyCIDRs
	deps.WhitelistMaxValidDays = cfg.WhitelistMaxValidDays
	deps.TMBaseCurrency = cfg.TMBaseCurrency
	deps.RealtimeMonitorTimeout = cfg.RealtimeMonitorTimeout
	if len(cfg.TrustedProxyCIDRs) > 0 {
		slog.Info("trusted reverse proxies configured", "cidr_count", len(cfg.TrustedProxyCIDRs))
	} else if cfg.Env == "production" {
		slog.Warn("no trusted reverse proxies configured; forwarded client IPs will be ignored")
	}
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
		deps.TMContract = nativeEngine.TMContract()
		slog.Info("native Go engine loaded", "tm_digest", deps.ConfigDigests["tm_scenarios"])
	} else {
		slog.Warn("native Go engine unavailable", "error", nativeErr)
	}

	if cfg.Seed {
		seedResult, err := runSeed(context.Background(), pool, encryptor, deps)
		if err != nil {
			slog.Error("seed failed", "error", err)
			os.Exit(1)
		}
		deps.DemoDataEnabled = seedResult.DemoDataEnabled()
	}

	jobsCtx, cancelJobs := context.WithCancel(context.Background())
	defer cancelJobs()

	// Event bus (Task 6/7, EVENT_BUS driver selection): pg_notify requires a
	// real PostgreSQL connection, so it's wired in every PostgreSQL deployment
	// mode. API processes must consume the outbox too: otherwise an API-only
	// deployment would commit durable event intents that no process publishes.
	// The in-memory dev mode has no event bus.
	if pool != nil && (runAPIJobs || runWorkerJobs) {
		bus, err := events.NewBus(events.Config{Driver: cfg.EventBus, Pool: pool})
		if err != nil {
			slog.Error("event bus", "error", err)
			os.Exit(1)
		}
		deps.Events = bus
		if pgBus, ok := bus.(*pgnotifypkg.Bus); ok && deps.EventOutbox != nil {
			pgBus.Requery = func(ctx context.Context, topic string, afterSeq int64) ([]events.Event, error) {
				durable, err := deps.EventOutbox.ListAfter(ctx, topic, afterSeq, 0)
				if err != nil {
					return nil, err
				}
				out := make([]events.Event, 0, len(durable))
				for _, item := range durable {
					out = append(out, events.Event{
						ID: item.ID, Topic: item.Topic, Payload: item.Payload,
						SequenceNum: item.SequenceNum, ChainID: item.ChainID,
						ChainHopCount: item.ChainHopCount, CreatedAt: item.CreatedAt,
					})
				}
				return out, nil
			}
		}
		go server.RunEventOutboxWorker(jobsCtx, deps.EventOutbox, bus, eventOutboxCheckInterval)
		tierChangeHandler := handlers.NewTierChangeHandler(deps.Transactions, deps.Monitoring, deps.Alerts, deps.Cases, deps.CaseAlertLifecycle)
		subscriptionErrors, err := startEventSubscription(jobsCtx, bus, "cdd.tier_changed", tierChangeHandler)
		if err != nil {
			slog.Error("event bus subscription initialization failed", "topic", "cdd.tier_changed", "error", err)
			os.Exit(1)
		}
		go func() {
			if err := <-subscriptionErrors; err != nil && jobsCtx.Err() == nil {
				slog.Error("event bus subscribe ended", "topic", "cdd.tier_changed", "error", err)
			}
		}()
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
	listenAddr := cfg.HTTPAddr
	if cfg.Mode == "worker" {
		listenAddr = cfg.WorkerHTTPAddr
	}
	var srv *server.Server

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
		// Construct the server after the source directory dependencies are
		// attached so dashboard reads and scheduled screening share the same
		// composition.
		srv = server.New(listenAddr, deps)

		if cfg.ScreeningImportEnabled {
			fetcher := screening.NewDefaultHTTPFetcher(30 * time.Second)
			adapters := map[string]screening.ListAdapter{
				"ofac_sdn":     &screening.OFACAdapter{ListID: "ofac_sdn", URL: cfg.ScreeningOFACURL, Fetcher: fetcher},
				"eu_sanctions": &screening.EUAdapter{ListID: "eu_sanctions", URL: cfg.ScreeningEUURL, Fetcher: fetcher},
				"un_sc":        &screening.UNAdapter{ListID: "un_sc", URL: cfg.ScreeningUNURL, Fetcher: fetcher},
				"mof_japan":    &screening.MOFAdapter{ListID: "mof_japan", URL: cfg.ScreeningMOFURL, Fetcher: fetcher},
				"pep_provider": &screening.PEPAdapter{ListID: "pep_provider", URL: cfg.ScreeningPEPURL, Fetcher: fetcher},
			}
			var listConsumer screening.ListConsumer
			if consumer, ok := deps.Screening.(screening.ListConsumer); ok {
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
				Workflow:  deps.Wave3,
				PersistWorkflow: func(ctx context.Context, run *domain.ScreeningRun, results []domain.ScreeningResultRecord) error {
					return srv.PersistScreeningRun(ctx, run, results)
				},
				ConfigDigests: deps.ConfigDigests,
				Actor:         "system:screening-scheduler",
				ListIDs:       screeningListIDs,
			})
			go scheduler.RunPeriodic(jobsCtx, cfg.ScreeningCheckInterval)
			slog.Info("screening rescreening scheduler enabled", "check_interval", cfg.ScreeningCheckInterval)
		} else if cfg.ScreeningRescreenEnabled {
			slog.Warn("MERLON_SCREENING_RESCREEN_ENABLED=true but native engine is unavailable, rescreening scheduler disabled")
		}
	}

	if srv == nil {
		srv = server.New(listenAddr, deps)
	}
	if deps.InboundWebhooks != nil {
		deps.InboundWebhooks.SetHandler(srv.InboundRecordHandler())
		if len(cfg.InboundWebhookSecret) == 0 {
			slog.Warn("MERLON_INBOUND_WEBHOOK_SECRET is not set; inbound webhook endpoints will reject events")
		}
	}
	if runAPIJobs && configuredAdapter != nil && configuredAdapterConfig != nil {
		var checkpoints adapterpkg.CheckpointRepository
		if pool != nil {
			checkpoints = store.NewPgAdapterCheckpointRepo(pool)
		} else {
			checkpoints = adapterpkg.NewMemoryCheckpointRepository()
		}
		go runAdapterSyncPeriodically(jobsCtx, &adapterpkg.SyncService{AdapterID: "core", Config: configuredAdapterConfig, Adapter: configuredAdapter, Deps: adapterpkg.SyncDependencies{Customers: deps.Customers, Transactions: deps.Transactions, Accounts: deps.Accounts, Checkpoints: checkpoints}, Owner: "merlon-api"}, configuredAdapterConfig.Sync.Interval)
		slog.Info("adapter sync enabled", "interval", configuredAdapterConfig.Sync.Interval)
	}
	if runAPIJobs {
		go srv.ResumeManualBatchRuns(jobsCtx)
		slog.Info("manual batch recovery check enabled")
	}

	if runAPIJobs && deps.Customers != nil && deps.Cases != nil {
		batch.StartEDDEscalationTicker(jobsCtx, batch.EDDEscalationDeps{
			Customers:  deps.Customers,
			Cases:      deps.Cases,
			Webhook:    srv.DispatchWebhook,
			Policy:     policies.EDD(),
			Stage2Days: cfg.EDDStage2Days,
			Stage3Days: cfg.EDDStage3Days,
		}, eddEscalationCheckInterval)
		slog.Info("EDD escalation job enabled", "policy_version", policies.EDD().Version())
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
				// Wave 3 evidence streams. Without these two, pending
				// evaluations and backtest job history grew without bound and
				// no policy governed them.
				"pending_evaluation_data": purger.PendingEvaluationData,
				"backtest_data":           purger.BacktestData,
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
		Addr:              listenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		WriteTimeout:      httpWriteTimeout,
		IdleTimeout:       httpIdleTimeout,
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
	if runAPIJobs && deps.InboundWebhooks != nil {
		go func() {
			if err := deps.InboundWebhooks.RunWorker(webhookRetryCtx, inboundwebhook.DefaultRetryInterval); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("inbound webhook worker stopped", "error", err)
			}
		}()
		slog.Info("inbound webhook worker started", "interval", inboundwebhook.DefaultRetryInterval)
	}

	if runWorkerJobs && deps.PendingEvaluations != nil && deps.Monitoring != nil {
		recoveryJob := batch.NewRecoveryJob(deps.PendingEvaluations, deps.Monitoring, deps.Alerts, deps.Transactions, deps.Customers)
		recoveryJob.ConfigDigests = deps.ConfigDigests
		recoveryJob.SetPersistence(deps.Atomic, deps.Audit, deps.EventOutbox)
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
				CaseLifecycle: deps.CaseAlertLifecycle,
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
