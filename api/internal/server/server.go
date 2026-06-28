package server

import (
	"net/http"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/engine"
)

type Server struct {
	mux          *http.ServeMux
	addr         string
	customers    domain.CustomerRepository
	transactions domain.TransactionRepository
	alerts       domain.AlertRepository
	scoring      engine.ScoringEngine
	monitoring   engine.MonitoringEngine
	screening    engine.ScreeningEngine
	backtest     engine.BacktestEngine
	audit        domain.AuditRepository
	cases        domain.CaseRepository
	apikeys      domain.APIKeyRepository
	limiter      *rateLimiter
}

type Deps struct {
	Customers    domain.CustomerRepository
	Transactions domain.TransactionRepository
	Alerts       domain.AlertRepository
	Scoring      engine.ScoringEngine
	Monitoring   engine.MonitoringEngine
	Screening    engine.ScreeningEngine
	Backtest     engine.BacktestEngine
	Audit        domain.AuditRepository
	Cases        domain.CaseRepository
	APIKeys      domain.APIKeyRepository
	RateLimit    int
}

func New(addr string, deps Deps) *Server {
	s := &Server{
		mux:          http.NewServeMux(),
		addr:         addr,
		customers:    deps.Customers,
		transactions: deps.Transactions,
		alerts:       deps.Alerts,
		scoring:      deps.Scoring,
		monitoring:   deps.Monitoring,
		screening:    deps.Screening,
		backtest:     deps.Backtest,
		audit:        deps.Audit,
		cases:        deps.Cases,
		apikeys:      deps.APIKeys,
	}
	if deps.RateLimit > 0 {
		s.limiter = newRateLimiter(deps.RateLimit, time.Minute)
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)

	// Customers
	s.mux.HandleFunc("GET /api/v1/customers", s.handleListCustomers)
	s.mux.HandleFunc("GET /api/v1/customers/{id}", s.handleGetCustomer)
	s.mux.HandleFunc("POST /api/v1/customers", s.handleCreateCustomer)
	s.mux.HandleFunc("PUT /api/v1/customers/{id}", s.handleUpdateCustomer)
	s.mux.HandleFunc("GET /api/v1/customers/{id}/scores", s.handleGetScoreHistory)
	s.mux.HandleFunc("POST /api/v1/customers/{id}/score", s.handleScoreCustomer)
	s.mux.HandleFunc("POST /api/v1/customers/{id}/screen", s.handleScreenCustomer)

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

	// API Keys (admin only, managed outside auth middleware)
	s.mux.HandleFunc("POST /api/v1/admin/apikeys", s.handleCreateAPIKey)
	s.mux.HandleFunc("GET /api/v1/admin/apikeys", s.handleListAPIKeys)
	s.mux.HandleFunc("DELETE /api/v1/admin/apikeys/{id}", s.handleRevokeAPIKey)

	// Audit
	s.mux.HandleFunc("GET /api/v1/audit", s.handleListAuditLogs)
}

func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = s.auditMiddleware(h)
	h = s.authMiddleware(h)
	h = s.rateLimitMiddleware(h)
	return h
}

func (s *Server) Start() error {
	return http.ListenAndServe(s.addr, s.Handler())
}
