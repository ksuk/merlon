package server

import (
	"encoding/json"
	"errors"
	"github.com/merlon-aml/merlon/api/internal/apierr"
	"net/http"
	"strconv"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

type CreateTransactionRequest struct {
	CustomerID          string                      `json:"customer_id"`
	ExternalID          string                      `json:"external_id"`
	Amount              float64                     `json:"amount"`
	Currency            string                      `json:"currency"`
	Direction           domain.TransactionDirection `json:"direction"`
	CounterpartyID      string                      `json:"counterparty_id"`
	CounterpartyCountry string                      `json:"counterparty_country"`
	Channel             string                      `json:"channel"`
	ExecutedAt          time.Time                   `json:"executed_at"`
}

func transactionCursor(t domain.Transaction) Cursor {
	return Cursor{CreatedAt: t.CreatedAt, ID: t.ID}
}

func (s *Server) handleListTransactions(w http.ResponseWriter, r *http.Request) {
	customerID := r.URL.Query().Get("customer_id")
	if customerID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer_id query parameter required")
		return
	}

	if r.URL.Query().Get("cursor") != "" {
		s.handleListTransactionsCursor(w, r, customerID)
		return
	}
	s.handleListTransactionsOffset(w, r, customerID)
}

// handleListTransactionsCursor serves api.md §1.1 cursor-based pagination.
func (s *Server) handleListTransactionsCursor(w http.ResponseWriter, r *http.Request, customerID string) {
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	txns, err := s.transactions.ListByCustomerCursor(r.Context(), customerID, pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	page, meta := BuildPaginationMeta(txns, pageReq.Limit, transactionCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

// handleListTransactionsOffset preserves the pre-existing offset/limit
// contract (api.md §1.2 dual-support / deprecation period) while still
// returning the additive {"data", "pagination"} envelope.
func (s *Server) handleListTransactionsOffset(w http.ResponseWriter, r *http.Request, customerID string) {
	offsetParam := r.URL.Query().Get("offset")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(offsetParam)
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	txns, err := s.transactions.ListByCustomer(r.Context(), customerID, limit+1, offset)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	if offsetParam != "" {
		setOffsetDeprecationHeaders(w)
	}

	page, meta := BuildPaginationMeta(txns, limit, transactionCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

func (s *Server) handleGetTransaction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := s.transactions.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleCreateTransaction(w http.ResponseWriter, r *http.Request) {
	var req CreateTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}

	if req.CustomerID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer_id required")
		return
	}
	if req.ExternalID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "external_id required")
		return
	}
	if req.Amount <= 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "amount must be positive")
		return
	}
	if !isValidDirection(req.Direction) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "direction must be one of: inbound, outbound, internal")
		return
	}

	// Verify customer exists
	_, err := s.customers.Get(r.Context(), req.CustomerID)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeNotFound, "customer not found: "+req.CustomerID)
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
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
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, t)
}

func isValidDirection(d domain.TransactionDirection) bool {
	switch d {
	case domain.DirectionInbound, domain.DirectionOutbound, domain.DirectionInternal:
		return true
	default:
		return false
	}
}
