package server

import (
	"net/http"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

type Server struct {
	mux          *http.ServeMux
	addr         string
	customers    domain.CustomerRepository
	transactions domain.TransactionRepository
	alerts       domain.AlertRepository
}

type Deps struct {
	Customers    domain.CustomerRepository
	Transactions domain.TransactionRepository
	Alerts       domain.AlertRepository
}

func New(addr string, deps Deps) *Server {
	s := &Server{
		mux:          http.NewServeMux(),
		addr:         addr,
		customers:    deps.Customers,
		transactions: deps.Transactions,
		alerts:       deps.Alerts,
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

	// Transactions
	s.mux.HandleFunc("GET /api/v1/transactions", s.handleListTransactions)
	s.mux.HandleFunc("GET /api/v1/transactions/{id}", s.handleGetTransaction)
	s.mux.HandleFunc("POST /api/v1/transactions", s.handleCreateTransaction)

	// Alerts
	s.mux.HandleFunc("GET /api/v1/alerts", s.handleListAlerts)
	s.mux.HandleFunc("GET /api/v1/alerts/{id}", s.handleGetAlert)
	s.mux.HandleFunc("PATCH /api/v1/alerts/{id}", s.handleUpdateAlertStatus)
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) Start() error {
	return http.ListenAndServe(s.addr, s.mux)
}
