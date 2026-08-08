package server

import (
	"context"
	"github.com/ksuk/merlon/api/internal/apierr"
	"net/http"
	"net/netip"
	"time"

	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/events"
	"github.com/ksuk/merlon/api/internal/notify"
	"github.com/ksuk/merlon/api/internal/screening"
)

const maxRequestBodyBytes = 1 << 20

const (
	defaultRealtimeMonitorTimeout   = 30 * time.Second
	pendingReviewPersistenceTimeout = 5 * time.Second
)

// DBPinger reports whether the PostgreSQL connection pool is reachable. It
// is satisfied directly by *pgxpool.Pool, kept as a narrow interface here so
// /healthz/ready (Task 3, the operational design §4.4) can be tested without a real
// database.
type DBPinger interface {
	Ping(ctx context.Context) error
}

type Server struct {
	mux                      *http.ServeMux
	routeCount               int
	addr                     string
	customers                domain.CustomerRepository
	transactions             domain.TransactionRepository
	alerts                   domain.AlertRepository
	scoring                  engine.ScoringEngine
	monitoring               engine.MonitoringEngine
	screening                engine.ScreeningEngine
	backtest                 engine.BacktestEngine
	backtestJobs             domain.BacktestJobRepository
	audit                    domain.AuditRepository
	cases                    domain.CaseRepository
	caseAlertLifecycle       domain.CaseAlertLifecycleRepository
	apikeys                  domain.APIKeyRepository
	webhooks                 domain.WebhookRepository
	configEngine             engine.ConfigEngine
	engineHealth             engine.HealthChecker
	limiter                  *rateLimiter
	clientIPs                clientIPResolver
	bootstrapToken           string
	tokenIssuer              *auth.TokenIssuer
	denylist                 auth.Denylist
	users                    domain.UserRepository
	refreshTokens            domain.RefreshTokenRepository
	rules                    domain.RuleRepository
	whitelist                domain.WhitelistRepository
	whitelistMaxValidDaysCfg int
	// tmBaseCurrency is the interim PH9 invariant: aggregation only combines
	// normalized amounts in one configured currency. Full FX/asset semantics
	// remain a PH10 gate.
	tmBaseCurrency         string
	realtimeMonitorTimeout time.Duration
	screeningResults       domain.ScreeningResultRepository
	retention              domain.RetentionRepository
	accounts               domain.AccountRepository
	configDigests          map[string]string
	// demoDataEnabled reports whether this instance is seeded from the
	// synthetic demogen dataset (PH7 DD3): surfaced via
	// GET /api/v1/system features.demo_data so the UI can show a small
	// "synthetic demo data" indicator. Display-only; it does not gate any
	// functionality.
	demoDataEnabled bool

	// screeningListStore/screeningFailureTracker/screeningListIDs back the
	// dashboard's list-freshness display (the screening workflow; Task 4). Nil until
	// Task 6 wires the import job's concrete instances into main.go.
	screeningListStore      screening.ListStore
	screeningFailureTracker screening.FailureTracker
	screeningListIDs        []string

	db           DBPinger
	pendingEvals domain.PendingEvaluationRepository
	events       events.Bus

	// notifier/routingRules/publicURL back alert-created email notifications
	// (NOTIF-001/NOTIF-003, WS-8 Task 5). notifier is nil when no SMTP host
	// is configured, in which case notifyAlertCreated is a no-op.
	notifier     notify.Notifier
	routingRules []notify.RoutingRule
	publicURL    string
}

type Deps struct {
	Customers          domain.CustomerRepository
	Transactions       domain.TransactionRepository
	Alerts             domain.AlertRepository
	Scoring            engine.ScoringEngine
	Monitoring         engine.MonitoringEngine
	Screening          engine.ScreeningEngine
	Backtest           engine.BacktestEngine
	BacktestJobs       domain.BacktestJobRepository
	Audit              domain.AuditRepository
	Cases              domain.CaseRepository
	CaseAlertLifecycle domain.CaseAlertLifecycleRepository
	APIKeys            domain.APIKeyRepository
	Webhooks           domain.WebhookRepository
	Config             engine.ConfigEngine
	EngineHealth       engine.HealthChecker
	RateLimit          int
	TrustedProxyCIDRs  []netip.Prefix
	BootstrapToken     string
	TokenIssuer        *auth.TokenIssuer
	Denylist           auth.Denylist
	Users              domain.UserRepository
	RefreshTokens      domain.RefreshTokenRepository
	Rules              domain.RuleRepository
	Whitelist          domain.WhitelistRepository
	// WhitelistMaxValidDays overrides defaultWhitelistMaxValidDays (WL-002)
	// when positive; zero/negative falls back to the default.
	WhitelistMaxValidDays  int
	TMBaseCurrency         string
	RealtimeMonitorTimeout time.Duration
	ScreeningResults       domain.ScreeningResultRepository
	Retention              domain.RetentionRepository
	Accounts               domain.AccountRepository
	ConfigDigests          map[string]string
	// DemoDataEnabled is derived from seed provenance, and is true only when
	// the completed seed state identifies the demogen dataset (PH7 DD3).
	DemoDataEnabled bool

	ScreeningListStore      screening.ListStore
	ScreeningFailureTracker screening.FailureTracker
	ScreeningListIDs        []string

	DB                 DBPinger
	PendingEvaluations domain.PendingEvaluationRepository
	Events             events.Bus

	// Notifier/RoutingRules/PublicURL wire alert-created email notifications
	// (NOTIF-001/NOTIF-003, WS-8 Task 5). Notifier nil disables email
	// entirely (e.g. no SMTP host configured); RoutingRules nil falls back
	// to notify.DefaultRoutingRules() behavior only if the caller passes it
	// explicitly — main.go always resolves a concrete rule set before
	// constructing Deps.
	Notifier     notify.Notifier
	RoutingRules []notify.RoutingRule
	PublicURL    string
}

func New(addr string, deps Deps) *Server {
	s := &Server{
		mux:                      http.NewServeMux(),
		addr:                     addr,
		customers:                deps.Customers,
		transactions:             deps.Transactions,
		alerts:                   deps.Alerts,
		scoring:                  deps.Scoring,
		monitoring:               deps.Monitoring,
		screening:                deps.Screening,
		backtest:                 deps.Backtest,
		backtestJobs:             deps.BacktestJobs,
		audit:                    deps.Audit,
		cases:                    deps.Cases,
		caseAlertLifecycle:       deps.CaseAlertLifecycle,
		apikeys:                  deps.APIKeys,
		webhooks:                 deps.Webhooks,
		configEngine:             deps.Config,
		engineHealth:             deps.EngineHealth,
		clientIPs:                newClientIPResolver(deps.TrustedProxyCIDRs),
		bootstrapToken:           deps.BootstrapToken,
		tokenIssuer:              deps.TokenIssuer,
		denylist:                 deps.Denylist,
		users:                    deps.Users,
		refreshTokens:            deps.RefreshTokens,
		rules:                    deps.Rules,
		whitelist:                deps.Whitelist,
		whitelistMaxValidDaysCfg: deps.WhitelistMaxValidDays,
		tmBaseCurrency:           deps.TMBaseCurrency,
		realtimeMonitorTimeout:   deps.RealtimeMonitorTimeout,
		screeningResults:         deps.ScreeningResults,
		retention:                deps.Retention,
		accounts:                 deps.Accounts,
		configDigests:            deps.ConfigDigests,
		demoDataEnabled:          deps.DemoDataEnabled,

		screeningListStore:      deps.ScreeningListStore,
		screeningFailureTracker: deps.ScreeningFailureTracker,
		screeningListIDs:        deps.ScreeningListIDs,

		db:           deps.DB,
		pendingEvals: deps.PendingEvaluations,
		events:       deps.Events,

		notifier:     deps.Notifier,
		routingRules: deps.RoutingRules,
		publicURL:    deps.PublicURL,
	}
	if s.realtimeMonitorTimeout <= 0 {
		s.realtimeMonitorTimeout = defaultRealtimeMonitorTimeout
	}
	if deps.RateLimit > 0 {
		s.limiter = newRateLimiter(deps.RateLimit, time.Minute)
	}
	s.routes()
	return s
}

// route and routeHandler register an API route and count it. Registration goes
// through them rather than through s.mux directly so GET /api/v1/system/info
// can report the real size of the API surface; it previously reported a literal
// that had drifted to roughly half the true count. The SPA file routes
// (see spa.go) register on s.mux directly and are deliberately not counted —
// they serve the UI rather than forming part of the API contract.
func (s *Server) route(pattern string, handler http.HandlerFunc) {
	s.mux.HandleFunc(pattern, handler)
	s.routeCount++
}

func (s *Server) routeHandler(pattern string, handler http.Handler) {
	s.mux.Handle(pattern, handler)
	s.routeCount++
}

func (s *Server) routes() {
	s.route("GET /healthz", s.handleHealth)
	s.route("GET /healthz/live", s.handleHealthLive)
	s.route("GET /healthz/ready", s.handleHealthReady)
	s.route("GET /metrics", s.handleMetrics)

	// Initial setup (the operational design §4.5)
	s.route("POST /api/v1/setup", s.handleSetup)

	// Customers
	s.route("GET /api/v1/customers", s.handleListCustomers)
	s.route("GET /api/v1/customers/{id}", s.handleGetCustomer)
	s.route("POST /api/v1/customers", s.handleCreateCustomer)
	s.route("PUT /api/v1/customers/{id}", s.handleUpdateCustomer)
	s.route("GET /api/v1/customers/{id}/scores", s.handleGetScoreHistory)
	s.route("POST /api/v1/customers/{id}/score", s.handleScoreCustomer)
	s.route("POST /api/v1/customers/{id}/screen", s.handleScreenCustomer)

	// Screening (WS-7)
	s.route("POST /api/v1/screening/check", s.handleScreeningCheck)
	s.route("PATCH /api/v1/screening/results/{id}", s.handleUpdateScreeningResult)

	// Accounts (joint accounts, the data model §1.1.3)
	s.route("POST /api/v1/accounts", s.handleCreateAccount)
	s.route("GET /api/v1/accounts/{id}", s.handleGetAccount)
	s.route("POST /api/v1/accounts/{id}/customers", s.handleAddAccountCustomer)
	s.route("GET /api/v1/accounts/{id}/customers", s.handleListAccountCustomers)

	// Transactions
	s.route("GET /api/v1/transactions", s.handleListTransactions)
	s.route("GET /api/v1/transactions/{id}", s.handleGetTransaction)
	s.route("POST /api/v1/transactions", s.handleCreateTransaction)

	// Alerts
	s.route("GET /api/v1/alerts", s.handleListAlerts)
	s.route("POST /api/v1/alerts/bulk-close", s.handleBulkCloseAlerts)
	s.route("POST /api/v1/alerts/bulk-case", s.handleBulkCaseAssignment)
	s.route("GET /api/v1/alerts/{id}", s.handleGetAlert)
	s.route("PATCH /api/v1/alerts/{id}", s.handleUpdateAlertStatus)

	// Backtest
	s.route("POST /api/v1/backtest", s.handleRunBacktest)
	s.route("POST /api/v1/backtests", s.handleCreateBacktestJob)
	s.route("GET /api/v1/backtests", s.handleListBacktestJobs)
	s.route("GET /api/v1/backtests/{id}", s.handleGetBacktestJob)
	s.route("POST /api/v1/backtests/{id}/cancel", s.handleCancelBacktestJob)
	s.route("GET /api/v1/backtests/{id}/affected-customers", s.handleBacktestAffectedCustomers)

	// Reports
	s.route("POST /api/v1/reports/str", s.handleCreateSTR)
	s.route("GET /api/v1/reports/str/export", s.handleExportSTR)

	// Cases
	s.route("POST /api/v1/cases", s.handleCreateCase)
	s.route("GET /api/v1/cases", s.handleListCases)
	s.route("GET /api/v1/cases/{id}", s.handleGetCase)
	s.route("PATCH /api/v1/cases/{id}", s.handleUpdateCase)
	s.route("POST /api/v1/cases/{id}/notes", s.handleAddCaseNote)
	s.route("GET /api/v1/cases/{id}/related", s.handleGetRelatedCases)
	s.route("POST /api/v1/cases/{id}/related", s.handleAddRelatedCase)

	// Dashboard
	s.route("GET /api/v1/dashboard", s.handleDashboard)

	// Batch
	s.route("POST /api/v1/batch/score", s.handleBatchScore)
	s.route("POST /api/v1/batch/monitor", s.handleBatchMonitor)

	// Inbound webhooks (core system notifications, the data model §1.1.2)
	s.route("POST /api/v1/webhooks/inbound/customer-status", s.handleCustomerStatusWebhook)

	// Webhooks
	s.route("POST /api/v1/webhooks", s.handleCreateWebhook)
	s.route("GET /api/v1/webhooks", s.handleListWebhooks)
	s.route("GET /api/v1/webhooks/{id}", s.handleGetWebhook)
	s.route("DELETE /api/v1/webhooks/{id}", s.handleDeleteWebhook)
	s.route("GET /api/v1/webhooks/{id}/deliveries", s.handleListWebhookDeliveries)
	s.route("GET /api/v1/webhooks/dlq", s.handleListDLQEntries)
	s.route("POST /api/v1/webhooks/dlq/{id}/reprocess", s.handleReprocessDLQEntry)

	// API Keys (admin only, requires admin API key or bootstrap token)
	s.route("POST /api/v1/admin/apikeys", s.handleCreateAPIKey)
	s.route("GET /api/v1/admin/apikeys", s.handleListAPIKeys)
	s.route("DELETE /api/v1/admin/apikeys/{id}", s.handleRevokeAPIKey)

	// Users (admin only)
	s.route("GET /api/v1/admin/users", s.handleListUsers)

	// Retention policies (admin only via /api/v1/admin/ prefix gate,
	// the audit design RET-001/RET-002). Update additionally enforces a
	// positive period and any optional deployment-defined minimum.
	s.route("GET /api/v1/admin/retention-policies", s.handleListRetentionPolicies)
	s.route("PUT /api/v1/admin/retention-policies/{category}", s.handleUpdateRetentionPolicy)

	// Session (JWT login/logout/refresh/me)
	s.route("POST /api/v1/auth/login", s.handleLogin)
	s.route("POST /api/v1/auth/logout", s.handleLogout)
	s.route("POST /api/v1/auth/refresh", s.handleRefresh)
	s.route("GET /api/v1/auth/me", s.handleMe)

	// Audit (ALD-001/002 listing stays open like other list endpoints;
	// ALD-004/005 export requires auth.PermAuditRead since it extracts the
	// full filtered result set in one response, a higher-risk action than
	// browsing a page at a time).
	s.route("GET /api/v1/audit", s.handleListAuditLogs)
	s.routeHandler("GET /api/v1/audit/export", auth.RequirePermission(auth.PermAuditRead)(http.HandlerFunc(s.handleExportAuditLogs)))

	// Config validation
	s.route("POST /api/v1/config/validate", s.handleValidateConfig)

	// Rules (the HTTP API contract §1.4): reads are open to all roles, writes require
	// auth.PermRuleWrite (Admin only) on top of the coarse role check, since
	// hasPermission alone would let Analyst write like most other resources.
	s.route("GET /api/v1/rules", s.handleListRules)
	s.route("GET /api/v1/rules/{id}", s.handleGetRule)
	s.route("GET /api/v1/rules/{id}/export", s.handleExportRule)
	s.routeHandler("POST /api/v1/rules", auth.RequirePermission(auth.PermRuleWrite)(http.HandlerFunc(s.handleCreateRule)))
	s.routeHandler("PUT /api/v1/rules/{id}", auth.RequirePermission(auth.PermRuleWrite)(http.HandlerFunc(s.handleUpdateRule)))
	s.routeHandler("POST /api/v1/rules/{id}/activate", auth.RequirePermission(auth.PermRuleWrite)(http.HandlerFunc(s.handleActivateRule)))
	s.routeHandler("POST /api/v1/rules/{id}/deactivate", auth.RequirePermission(auth.PermRuleWrite)(http.HandlerFunc(s.handleDeactivateRule)))
	s.routeHandler("POST /api/v1/rules/import", auth.RequirePermission(auth.PermRuleWrite)(http.HandlerFunc(s.handleImportRules)))

	// Whitelist (whitelist.md §1, §3.1): reads are open to all roles; request
	// and revoke require auth.PermWhitelistRequest, approve requires the
	// stricter auth.PermWhitelistApprove (segregation of duties, WL-003).
	s.route("GET /api/v1/whitelist", s.handleListWhitelistEntries)
	s.route("GET /api/v1/whitelist/{id}", s.handleGetWhitelistEntry)
	s.routeHandler("POST /api/v1/whitelist", auth.RequirePermission(auth.PermWhitelistRequest)(http.HandlerFunc(s.handleCreateWhitelistEntry)))
	s.routeHandler("POST /api/v1/whitelist/{id}/approve", auth.RequirePermission(auth.PermWhitelistApprove)(http.HandlerFunc(s.handleApproveWhitelistEntry)))
	s.routeHandler("POST /api/v1/whitelist/{id}/revoke", auth.RequirePermission(auth.PermWhitelistRequest)(http.HandlerFunc(s.handleRevokeWhitelistEntry)))
	// Reviews decide whether to keep suppressing an active entry (renew) or
	// lapse it (expire), the same authority level as approval, so this shares
	// auth.PermWhitelistApprove rather than the request-level permission.
	s.routeHandler("POST /api/v1/whitelist/{id}/reviews", auth.RequirePermission(auth.PermWhitelistApprove)(http.HandlerFunc(s.handleCreateWhitelistReview)))

	// System info
	s.route("GET /api/v1/system/info", s.handleSystemInfo)
	s.route("GET /api/v1/system/config-digests", s.handleConfigDigests)

	// OpenAPI
	s.route("GET /api/v1/openapi.json", s.handleOpenAPI)
}

func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = s.auditMiddleware(h)
	h = s.authMiddleware(h)
	h = s.rateLimitMiddleware(h)
	h = s.clientIPMiddleware(h)
	h = requestBodyLimitMiddleware(h)
	h = s.metricsMiddleware(h)
	h = requestIDMiddleware(h)
	return h
}

func (s *Server) Start() error {
	return http.ListenAndServe(s.addr, s.Handler())
}

func requestBodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxRequestBodyBytes {
			writeErrorCode(w, http.StatusRequestEntityTooLarge, apierr.CodePayloadTooLarge, "request body too large")
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}
