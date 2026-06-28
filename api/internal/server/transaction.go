package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

type CreateTransactionRequest struct {
	CustomerID          string                   `json:"customer_id"`
	ExternalID          string                   `json:"external_id"`
	Amount              float64                  `json:"amount"`
	Currency            string                   `json:"currency"`
	Direction           domain.TransactionDirection `json:"direction"`
	CounterpartyID      string                   `json:"counterparty_id"`
	CounterpartyCountry string                   `json:"counterparty_country"`
	Channel             string                   `json:"channel"`
	ExecutedAt          time.Time                `json:"executed_at"`
}

func (s *Server) handleListTransactions(w http.ResponseWriter, r *http.Request) {
	customerID := r.URL.Query().Get("customer_id")
	if customerID == "" {
		writeError(w, http.StatusBadRequest, "customer_id query parameter required")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	txns, err := s.transactions.ListByCustomer(r.Context(), customerID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if txns == nil {
		txns = []domain.Transaction{}
	}

	writeJSON(w, http.StatusOK, txns)
}

func (s *Server) handleGetTransaction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := s.transactions.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleCreateTransaction(w http.ResponseWriter, r *http.Request) {
	var req CreateTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.CustomerID == "" {
		writeError(w, http.StatusBadRequest, "customer_id required")
		return
	}
	if req.ExternalID == "" {
		writeError(w, http.StatusBadRequest, "external_id required")
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be positive")
		return
	}

	// Verify customer exists
	_, err := s.customers.Get(r.Context(), req.CustomerID)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusBadRequest, "customer not found: "+req.CustomerID)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	currency := req.Currency
	if currency == "" {
		currency = "JPY"
	}

	now := time.Now()
	executedAt := req.ExecutedAt
	if executedAt.IsZero() {
		executedAt = now
	}

	t := &domain.Transaction{
		ID:                  generateID(),
		CustomerID:          req.CustomerID,
		ExternalID:          req.ExternalID,
		Amount:              req.Amount,
		Currency:            currency,
		Direction:           req.Direction,
		CounterpartyID:      req.CounterpartyID,
		CounterpartyCountry: req.CounterpartyCountry,
		Channel:             req.Channel,
		ExecutedAt:          executedAt,
		CreatedAt:           now,
	}

	if err := s.transactions.Create(r.Context(), t); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, t)
}
