package server

import (
	"net/http"

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
}

type Deps struct {
	Customers    domain.CustomerRepository
	Transactions domain.TransactionRepository
	Alerts       domain.AlertRepository
	Scoring      engine.ScoringEngine
	Monitoring   engine.MonitoringEngine
	Screening    engine.ScreeningEngine
	Backtest     engine.BacktestEngine
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
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) Start() error {
	return http.ListenAndServe(s.addr, s.mux)
}
