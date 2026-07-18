package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/transactionhistory"
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
	AccountID           *string                     `json:"account_id,omitempty"`
	Counterparty        *domain.Counterparty        `json:"counterparty,omitempty"`
	Metadata            map[string]any              `json:"metadata,omitempty"`
	// Future event times are accepted intentionally for upstream clock skew
	// and scheduled transactions. Realtime evaluation anchors its upper bound
	// at max(now, ExecutedAt), so the accepted transaction is never omitted.
	ExecutedAt time.Time `json:"executed_at"`
}

func isValidCounterpartyType(t domain.CounterpartyType) bool {
	switch t {
	case domain.CounterpartyTypeVASP, domain.CounterpartyTypeUnhostedWallet, domain.CounterpartyTypeUnknown:
		return true
	default:
		return false
	}
}

// isValidTravelRuleStatus intentionally accepts TravelRuleIncomplete: an
// incomplete travel-rule record must not block transaction creation or TM
// evaluation (Fail-Alert, the data model §1.3.1 — prefer evaluating with
// partial data over dropping the transaction).
func isValidTravelRuleStatus(s domain.TravelRuleStatus) bool {
	switch s {
	case domain.TravelRuleComplete, domain.TravelRuleIncomplete, domain.TravelRuleNotApplicable:
		return true
	default:
		return false
	}
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

// handleListTransactionsCursor serves the HTTP API contract §1.1 cursor-based pagination.
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
// contract (the HTTP API contract §1.2 dual-support / deprecation period) while still
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
	if req.Counterparty != nil {
		if !isValidCounterpartyType(req.Counterparty.CounterpartyType) {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "counterparty.counterparty_type must be one of: vasp, unhosted_wallet, unknown")
			return
		}
		if !isValidTravelRuleStatus(req.Counterparty.TravelRuleStatus) {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "counterparty.travel_rule_status must be one of: complete, incomplete, not_applicable")
			return
		}
	}

	// Verify customer exists
	customer, err := s.customers.Get(r.Context(), req.CustomerID)
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
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "currency required")
		return
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
		AccountID:           req.AccountID,
		Counterparty:        req.Counterparty,
		Metadata:            req.Metadata,
		ExecutedAt:          executedAt,
		CreatedAt:           now,
	}
	// Idempotency-Key (the HTTP API contract §4.1): a resend using an already-used key is
	// rejected with 409, independent of whether external_id also matches.
	if key := r.Header.Get("Idempotency-Key"); key != "" {
		t.IdempotencyKey = &key
	}

	if err := s.transactions.Create(r.Context(), t); err != nil {
		var conflict *domain.ErrConflict
		if errors.As(err, &conflict) {
			writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, conflict.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	// Transaction creation is accepted independently of engine availability,
	// but the realtime monitoring pass must run before the request is complete.
	// Any engine/history failure is fail-alerted into PENDING_REVIEW rather than
	// silently dropping the transaction.
	s.monitorCreatedTransaction(r.Context(), customer, t)

	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) monitorCreatedTransaction(ctx context.Context, customer *domain.Customer, created *domain.Transaction) {
	if s.monitoring == nil || s.alerts == nil || customer == nil || customer.EffectiveStatus() == domain.CustomerStatusClosed {
		return
	}

	monitorCtx, cancel := context.WithTimeout(ctx, s.realtimeMonitorTimeout)
	defer cancel()
	txns, err := s.listRealtimeTransactions(monitorCtx, customer.ID, created)
	if err != nil {
		_ = s.queuePendingReviewDurably(ctx, customer, []domain.Transaction{*created}, err)
		return
	}
	if len(txns) == 0 {
		txns = []domain.Transaction{*created}
	}
	if s.tmBaseCurrency != "" {
		for _, txn := range txns {
			if !strings.EqualFold(txn.Currency, s.tmBaseCurrency) {
				_ = s.queuePendingReviewDurably(ctx, customer, txns, fmt.Errorf("currency %s is not normalized to %s", txn.Currency, s.tmBaseCurrency))
				return
			}
		}
	}

	alerts, err := s.evaluateMonitoring(monitorCtx, customer, txns, engine.EvaluationModeRealtime, nil)
	if err != nil {
		_ = s.queuePendingReviewDurably(ctx, customer, txns, err)
		return
	}
	for _, alert := range alerts {
		alert.ID = generateID()
		now := time.Now()
		alert.CreatedAt, alert.UpdatedAt = now, now
		if alert.DetectedAt.IsZero() {
			alert.DetectedAt = now
		}
		windowStart := domain.DailyAggregationWindowStart(alert.DetectedAt)
		alert.AggregationWindowStart = &windowStart
		if _, err := s.applyWhitelistSuppression(ctx, &alert); err != nil {
			slog.WarnContext(ctx, "realtime alert suppression failed", "error", err)
			continue
		}
		createdAlert, _, err := s.alerts.CreateIfNotDuplicate(ctx, &alert)
		if err != nil || !createdAlert {
			continue
		}
		recordAlertCreated(&alert)
		s.consolidateAlertIntoCase(ctx, &alert)
		s.dispatchWebhook(ctx, domain.WebhookEventAlertCreated, alert)
		s.notifyAlertCreated(ctx, alert)
	}
}

func (s *Server) queuePendingReviewDurably(parent context.Context, customer *domain.Customer, txns []domain.Transaction, cause error) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), pendingReviewPersistenceTimeout)
	defer cancel()
	return s.queuePendingReview(ctx, customer, txns, cause)
}

func (s *Server) listRealtimeTransactions(ctx context.Context, customerID string, created *domain.Transaction) ([]domain.Transaction, error) {
	anchor := time.Now().UTC()
	if created.ExecutedAt.After(anchor) {
		anchor = created.ExecutedAt
	}
	to := anchor.Add(time.Nanosecond)
	provider, ok := s.monitoring.(engine.RealtimeHistoryWindowProvider)
	if !ok {
		return s.listRealtimeTransactionRange(ctx, customerID, time.Time{}, to, created.CreatedAt)
	}
	window, bounded := provider.RealtimeHistoryWindow()
	if !bounded {
		return s.listRealtimeTransactionRange(ctx, customerID, time.Time{}, to, created.CreatedAt)
	}
	if window < 0 {
		return nil, fmt.Errorf("realtime history window must not be negative")
	}

	currentFrom := anchor.Add(-window)
	eventFrom := created.ExecutedAt.Add(-window)
	eventTo := created.ExecutedAt.Add(window).Add(time.Nanosecond)
	if eventTo.After(to) {
		eventTo = to
	}
	// The late-arriving event window and current window usually overlap. Use
	// one query in that case; otherwise fetch the two bounded ranges and merge
	// them so a backdated event cannot force a scan across the intervening years.
	if !eventTo.Before(currentFrom) {
		from := currentFrom
		if eventFrom.Before(from) {
			from = eventFrom
		}
		return s.listRealtimeTransactionRange(ctx, customerID, from, to, created.CreatedAt)
	}
	eventTxns, err := s.listRealtimeTransactionRange(ctx, customerID, eventFrom, eventTo, created.CreatedAt)
	if err != nil {
		return nil, err
	}
	currentTxns, err := s.listRealtimeTransactionRange(ctx, customerID, currentFrom, to, created.CreatedAt)
	if err != nil {
		return nil, err
	}
	return mergeRealtimeTransactions(eventTxns, currentTxns), nil
}

func (s *Server) listRealtimeTransactionRange(ctx context.Context, customerID string, from, to, createdThrough time.Time) ([]domain.Transaction, error) {
	return transactionhistory.ListCustomerTransactionsAsOf(ctx, s.transactions, customerID, transactionhistory.Query{
		From:           from,
		To:             to,
		CreatedThrough: createdThrough,
	})
}

func mergeRealtimeTransactions(groups ...[]domain.Transaction) []domain.Transaction {
	byID := make(map[string]domain.Transaction)
	for _, group := range groups {
		for _, txn := range group {
			byID[txn.ID] = txn
		}
	}
	merged := make([]domain.Transaction, 0, len(byID))
	for _, txn := range byID {
		merged = append(merged, txn)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].ExecutedAt.Equal(merged[j].ExecutedAt) {
			return merged[i].ID < merged[j].ID
		}
		return merged[i].ExecutedAt.Before(merged[j].ExecutedAt)
	})
	return merged
}

func isValidDirection(d domain.TransactionDirection) bool {
	switch d {
	case domain.DirectionInbound, domain.DirectionOutbound, domain.DirectionInternal:
		return true
	default:
		return false
	}
}
