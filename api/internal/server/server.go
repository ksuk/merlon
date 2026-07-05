package server

import (
	"context"
	"net/http"
	"time"

	"github.com/merlon-aml/merlon/api/internal/auth"
	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/engine"
	"github.com/merlon-aml/merlon/api/internal/screening"
)

const maxRequestBodyBytes = 1 << 20

// DBPinger reports whether the PostgreSQL connection pool is reachable. It
// is satisfied directly by *pgxpool.Pool, kept as a narrow interface here so
// /healthz/ready (Task 3, overview.md §4.4) can be tested without a real
// database.
type DBPinger interface {
	Ping(ctx context.Context) error
}

type Server struct {
	mux              *http.ServeMux
	addr             string
	customers        domain.CustomerRepository
	transactions     domain.TransactionRepository
	alerts           domain.AlertRepository
	scoring          engine.ScoringEngine
	monitoring       engine.MonitoringEngine
	screening        engine.ScreeningEngine
	backtest         engine.BacktestEngine
	audit            domain.AuditRepository
	cases            domain.CaseRepository
	apikeys          domain.APIKeyRepository
	webhooks         domain.WebhookRepository
	configEngine     engine.ConfigEngine
	engineHealth     engine.HealthChecker
	limiter          *rateLimiter
	bootstrapToken   string
	tokenIssuer      *auth.TokenIssuer
	denylist         auth.Denylist
	users            domain.UserRepository
	refreshTokens    domain.RefreshTokenRepository
	rules            domain.RuleRepository
	screeningResults domain.ScreeningResultRepository

	// screeningListStore/screeningFailureTracker/screeningListIDs back the
	// dashboard's list-freshness display (screening.md; Task 4). Nil until
	// Task 6 wires the import job's concrete instances into main.go.
	screeningListStore      screening.ListStore
	screeningFailureTracker screening.FailureTracker
	screeningListIDs        []string

	db DBPinger
}

type Deps struct {
	Customers        domain.CustomerRepository
	Transactions     domain.TransactionRepository
	Alerts           domain.AlertRepository
	Scoring          engine.ScoringEngine
	Monitoring       engine.MonitoringEngine
	Screening        engine.ScreeningEngine
	Backtest         engine.BacktestEngine
	Audit            domain.AuditRepository
	Cases            domain.CaseRepository
	APIKeys          domain.APIKeyRepository
	Webhooks         domain.WebhookRepository
	Config           engine.ConfigEngine
	EngineHealth     engine.HealthChecker
	RateLimit        int
	BootstrapToken   string
	TokenIssuer      *auth.TokenIssuer
	Denylist         auth.Denylist
	Users            domain.UserRepository
	RefreshTokens    domain.RefreshTokenRepository
	Rules            domain.RuleRepository
	ScreeningResults domain.ScreeningResultRepository

	ScreeningListStore      screening.ListStore
	ScreeningFailureTracker screening.FailureTracker
	ScreeningListIDs        []string

	DB DBPinger
}

func New(addr string, deps Deps) *Server {
	s := &Server{
		mux:              http.NewServeMux(),
		addr:             addr,
		customers:        deps.Customers,
		transactions:     deps.Transactions,
		alerts:           deps.Alerts,
		scoring:          deps.Scoring,
		monitoring:       deps.Monitoring,
		screening:        deps.Screening,
		backtest:         deps.Backtest,
		audit:            deps.Audit,
		cases:            deps.Cases,
		apikeys:          deps.APIKeys,
		webhooks:         deps.Webhooks,
		configEngine:     deps.Config,
		engineHealth:     deps.EngineHealth,
		bootstrapToken:   deps.BootstrapToken,
		tokenIssuer:      deps.TokenIssuer,
		denylist:         deps.Denylist,
		users:            deps.Users,
		refreshTokens:    deps.RefreshTokens,
		rules:            deps.Rules,
		screeningResults: deps.ScreeningResults,

		screeningListStore:      deps.ScreeningListStore,
		screeningFailureTracker: deps.ScreeningFailureTracker,
		screeningListIDs:        deps.ScreeningListIDs,

		db: deps.DB,
	}
	if deps.RateLimit > 0 {
		s.limiter = newRateLimiter(deps.RateLimit, time.Minute)
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /healthz/live", s.handleHealthLive)
	s.mux.HandleFunc("GET /healthz/ready", s.handleHealthReady)
	s.mux.HandleFunc("GET /metrics", s.handleMetrics)

	// Initial setup (overview.md §4.5)
	s.mux.HandleFunc("POST /api/v1/setup", s.handleSetup)

	// Customers
	s.mux.HandleFunc("GET /api/v1/customers", s.handleListCustomers)
	s.mux.HandleFunc("GET /api/v1/customers/{id}", s.handleGetCustomer)
	s.mux.HandleFunc("POST /api/v1/customers", s.handleCreateCustomer)
	s.mux.HandleFunc("PUT /api/v1/customers/{id}", s.handleUpdateCustomer)
	s.mux.HandleFunc("GET /api/v1/customers/{id}/scores", s.handleGetScoreHistory)
	s.mux.HandleFunc("POST /api/v1/customers/{id}/score", s.handleScoreCustomer)
	s.mux.HandleFunc("POST /api/v1/customers/{id}/screen", s.handleScreenCustomer)

	// Screening (WS-7)
	s.mux.HandleFunc("POST /api/v1/screening/check", s.handleScreeningCheck)
	s.mux.HandleFunc("PATCH /api/v1/screening/results/{id}", s.handleUpdateScreeningResult)

	// Transactions
	s.mux.HandleFunc("GET /api/v1/transactions", s.handleListTransactions)
	s.mux.HandleFunc("GET /api/v1/transactions/{id}", s.handleGetTransaction)
	s.mux.HandleFunc("POST /api/v1/transactions", s.handleCreateTransaction)

	// Alerts
	s.mux.HandleFunc("GET /api/v1/alerts", s.handleListAlerts)
	s.mux.HandleFunc("GET /api/v1/alerts/{id}", s.handleGetAlert)
	s.mux.HandleFunc("PATCH /api/v1/alerts/{id}", s.handleUpdateAlertStatus)

	// Backtest
	s.mux.HandleFunc("POST /api/v1/backtest", s.handleRunBacktest)

	// Reports
	s.mux.HandleFunc("POST /api/v1/reports/str", s.handleCreateSTR)
	s.mux.HandleFunc("GET /api/v1/reports/str/export", s.handleExportSTR)

	// Cases
	s.mux.HandleFunc("POST /api/v1/cases", s.handleCreateCase)
	s.mux.HandleFunc("GET /api/v1/cases", s.handleListCases)
	s.mux.HandleFunc("GET /api/v1/cases/{id}", s.handleGetCase)
	s.mux.HandleFunc("PATCH /api/v1/cases/{id}", s.handleUpdateCase)
	s.mux.HandleFunc("POST /api/v1/cases/{id}/notes", s.handleAddCaseNote)

	// Dashboard
	s.mux.HandleFunc("GET /api/v1/dashboard", s.handleDashboard)

	// Batch
	s.mux.HandleFunc("POST /api/v1/batch/score", s.handleBatchScore)
	s.mux.HandleFunc("POST /api/v1/batch/monitor", s.handleBatchMonitor)

	// Webhooks
	s.mux.HandleFunc("POST /api/v1/webhooks", s.handleCreateWebhook)
	s.mux.HandleFunc("GET /api/v1/webhooks", s.handleListWebhooks)
	s.mux.HandleFunc("GET /api/v1/webhooks/{id}", s.handleGetWebhook)
	s.mux.HandleFunc("DELETE /api/v1/webhooks/{id}", s.handleDeleteWebhook)
	s.mux.HandleFunc("GET /api/v1/webhooks/{id}/deliveries", s.handleListWebhookDeliveries)

	// API Keys (admin only, requires admin API key or bootstrap token)
	s.mux.HandleFunc("POST /api/v1/admin/apikeys", s.handleCreateAPIKey)
	s.mux.HandleFunc("GET /api/v1/admin/apikeys", s.handleListAPIKeys)
	s.mux.HandleFunc("DELETE /api/v1/admin/apikeys/{id}", s.handleRevokeAPIKey)

	// Users (admin only)
	s.mux.HandleFunc("GET /api/v1/admin/users", s.handleListUsers)

	// Session (JWT login/logout/refresh/me)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	s.mux.HandleFunc("POST /api/v1/auth/refresh", s.handleRefresh)
	s.mux.HandleFunc("GET /api/v1/auth/me", s.handleMe)

	// Audit
	s.mux.HandleFunc("GET /api/v1/audit", s.handleListAuditLogs)

	// Config validation
	s.mux.HandleFunc("POST /api/v1/config/validate", s.handleValidateConfig)

	// Rules (api.md §1.4): reads are open to all roles, writes require
	// auth.PermRuleWrite (Admin only) on top of the coarse role check, since
	// hasPermission alone would let Analyst write like most other resources.
	s.mux.HandleFunc("GET /api/v1/rules", s.handleListRules)
	s.mux.HandleFunc("GET /api/v1/rules/{id}", s.handleGetRule)
	s.mux.HandleFunc("GET /api/v1/rules/{id}/export", s.handleExportRule)
	s.mux.Handle("POST /api/v1/rules", auth.RequirePermission(auth.PermRuleWrite)(http.HandlerFunc(s.handleCreateRule)))
	s.mux.Handle("PUT /api/v1/rules/{id}", auth.RequirePermission(auth.PermRuleWrite)(http.HandlerFunc(s.handleUpdateRule)))
	s.mux.Handle("POST /api/v1/rules/{id}/activate", auth.RequirePermission(auth.PermRuleWrite)(http.HandlerFunc(s.handleActivateRule)))
	s.mux.Handle("POST /api/v1/rules/{id}/deactivate", auth.RequirePermission(auth.PermRuleWrite)(http.HandlerFunc(s.handleDeactivateRule)))
	s.mux.Handle("POST /api/v1/rules/import", auth.RequirePermission(auth.PermRuleWrite)(http.HandlerFunc(s.handleImportRules)))

	// System info
	s.mux.HandleFunc("GET /api/v1/system/info", s.handleSystemInfo)

	// OpenAPI
	s.mux.HandleFunc("GET /api/v1/openapi.json", s.handleOpenAPI)
}

func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = s.auditMiddleware(h)
	h = s.authMiddleware(h)
	h = s.rateLimitMiddleware(h)
	h = requestBodyLimitMiddleware(h)
	h = s.metricsMiddleware(h)
	return h
}

func (s *Server) Start() error {
	return http.ListenAndServe(s.addr, s.Handler())
}

func requestBodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxRequestBodyBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}
