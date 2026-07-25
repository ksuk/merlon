package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/transactionhistory"
)

const maxBatchCustomers = 1000

type batchScoreRequest struct {
	CustomerIDs []string `json:"customer_ids,omitempty"`
}

type batchScoreResult struct {
	CustomerID string  `json:"customer_id"`
	Score      float64 `json:"score"`
	RiskTier   string  `json:"risk_tier"`
	Error      string  `json:"error,omitempty"`
}

type batchScoreResponse struct {
	Total     int                `json:"total"`
	Succeeded int                `json:"succeeded"`
	Failed    int                `json:"failed"`
	Results   []batchScoreResult `json:"results"`
	Duration  string             `json:"duration"`
}

func (s *Server) handleBatchScore(w http.ResponseWriter, r *http.Request) {
	if s.scoring == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "scoring engine not configured")
		return
	}

	start := time.Now()

	var req batchScoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	if len(req.CustomerIDs) > maxBatchCustomers {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "too many customer_ids (max 1000)")
		return
	}

	ctx := r.Context()

	var customers []domain.Customer
	if len(req.CustomerIDs) > 0 {
		for _, id := range req.CustomerIDs {
			c, err := s.customers.Get(ctx, id)
			if err != nil {
				continue
			}
			customers = append(customers, *c)
		}
	} else {
		var err error
		customers, err = s.customers.List(ctx, maxBatchCustomers, 0)
		if err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
	}

	resp := batchScoreResponse{
		Total: len(customers),
	}

	for _, c := range customers {
		score, err := s.scoring.ScoreCustomer(ctx, &c, "default")
		if err != nil {
			resp.Failed++
			resp.Results = append(resp.Results, batchScoreResult{
				CustomerID: c.ID,
				Error:      err.Error(),
			})
			continue
		}

		c.RiskScore = &score.Score
		c.RiskTier = &score.Tier
		now := time.Now()
		c.LastScoredAt = &now
		s.customers.Update(ctx, &c)

		resp.Succeeded++
		resp.Results = append(resp.Results, batchScoreResult{
			CustomerID: c.ID,
			Score:      score.Score,
			RiskTier:   string(score.Tier),
		})
	}

	resp.Duration = time.Since(start).String()

	writeJSON(w, http.StatusOK, resp)
}

// queuePendingReview records a customer's transactions as PENDING_REVIEW
// (OPS-005, the operational design §4.4 Fail-Alert) when the monitoring engine call
// fails, so detection resumes automatically via the recovery job instead of
// being dropped. It returns false (leaving the caller to treat the call as
// a hard failure) if no PendingEvaluationRepository is configured or the
// queue write itself fails.
func (s *Server) queuePendingReview(ctx context.Context, c *domain.Customer, txns []domain.Transaction, cause error) bool {
	if s.pendingEvals == nil {
		return false
	}

	txIDs := make([]string, len(txns))
	for i, t := range txns {
		txIDs[i] = t.ID
	}
	if finder, ok := s.pendingEvals.(interface {
		ListPendingByCustomer(context.Context, string, domain.PendingEvaluationStatus) ([]domain.PendingEvaluation, error)
	}); ok {
		if existing, err := finder.ListPendingByCustomer(ctx, c.ID, domain.PendingEvaluationStatusPendingReview); err == nil {
			if pendingTransactionsOverlap(existing, txIDs) {
				return true // already queued; concurrent/replayed pass
			}
		}
	}
	return s.createPendingReview(ctx, c.ID, txIDs, cause)
}

func pendingTransactionsOverlap(existing []domain.PendingEvaluation, transactionIDs []string) bool {
	pendingIDs := make(map[string]struct{}, len(transactionIDs))
	for _, id := range transactionIDs {
		pendingIDs[id] = struct{}{}
	}
	for _, pe := range existing {
		for _, id := range pe.TransactionIDs {
			if _, found := pendingIDs[id]; found {
				return true
			}
		}
	}
	return false
}

func (s *Server) createPendingReview(ctx context.Context, customerID string, txIDs []string, cause error) bool {
	pe := &domain.PendingEvaluation{
		ID:             generateID(),
		CustomerID:     customerID,
		TransactionIDs: txIDs,
		Status:         domain.PendingEvaluationStatusPendingReview,
		Reason:         "engine unavailable: " + cause.Error(),
	}

	if err := s.pendingEvals.Create(ctx, pe); err != nil {
		return false
	}
	return true
}

func loadPendingReviewIndex(ctx context.Context, repo domain.PendingEvaluationRepository, customers []domain.Customer) (map[string][]domain.PendingEvaluation, bool, error) {
	finder, ok := repo.(domain.PendingEvaluationBulkLookup)
	if !ok {
		return nil, false, nil
	}
	ids := make([]string, 0, len(customers))
	for _, customer := range customers {
		ids = append(ids, customer.ID)
	}
	records, err := finder.ListPendingByCustomers(ctx, ids, domain.PendingEvaluationStatusPendingReview)
	if err != nil {
		// The capability exists, so callers must not fall back to one SELECT
		// per failed customer. Return an empty index and keep fail-alert writes
		// enabled even when the preload itself is temporarily unavailable.
		return make(map[string][]domain.PendingEvaluation), true, err
	}
	index := make(map[string][]domain.PendingEvaluation, len(records))
	for _, pe := range records {
		index[pe.CustomerID] = append(index[pe.CustomerID], pe)
	}
	return index, true, nil
}

func (s *Server) queuePendingReviewFromIndex(ctx context.Context, c *domain.Customer, txns []domain.Transaction, cause error, index map[string][]domain.PendingEvaluation) bool {
	txIDs := make([]string, len(txns))
	for i, txn := range txns {
		txIDs[i] = txn.ID
	}
	if pendingTransactionsOverlap(index[c.ID], txIDs) {
		return true
	}
	if !s.createPendingReview(ctx, c.ID, txIDs, cause) {
		return false
	}
	index[c.ID] = append(index[c.ID], domain.PendingEvaluation{CustomerID: c.ID, TransactionIDs: txIDs, Status: domain.PendingEvaluationStatusPendingReview})
	return true
}

func (s *Server) evaluateMonitoring(ctx context.Context, c *domain.Customer, txns []domain.Transaction, mode engine.EvaluationMode, scenarioIDs []string) ([]domain.Alert, error) {
	tier := domain.RiskTierLow
	if c.RiskTier != nil {
		tier = *c.RiskTier
	}
	return engine.EvaluateCompat(ctx, s.monitoring, engine.MonitoringRequest{CustomerID: c.ID, CustomerType: c.CustomerType, RiskTier: tier, Transactions: txns, ScenarioIDs: scenarioIDs, Mode: mode, EvaluatedAt: time.Now().UTC(), ConfigDigests: copyStringMap(s.configDigests)})
}

type batchMonitorRequest struct {
	CustomerIDs []string `json:"customer_ids,omitempty"`
	// Mode selects the scenario set to evaluate. Absent means realtime, which
	// is what this endpoint has always done.
	Mode string `json:"mode,omitempty"`
}

// parseBatchMonitorMode maps the request's optional mode onto an engine
// evaluation mode.
//
// An initial-data backfill needs both passes. Scenarios declaring
// evaluation_mode: batch (content/_sample/tm_scenarios/
// dormant_account_reactivation.yaml, high_frequency_small_amount.yaml) are
// filtered out of a realtime evaluation by runsUnder in the native engine, so
// a realtime-only backfill completes without ever applying them.
//
// "both" is deliberately not accepted as a request mode: it is a scenario-side
// declaration, and the engine treats any non-batch request mode as realtime,
// so accepting it here would silently mean "realtime".
func parseBatchMonitorMode(raw string) (engine.EvaluationMode, error) {
	switch raw {
	case "", string(engine.EvaluationModeRealtime):
		return engine.EvaluationModeRealtime, nil
	case string(engine.EvaluationModeBatch):
		return engine.EvaluationModeBatch, nil
	default:
		return "", fmt.Errorf("mode must be %q or %q",
			engine.EvaluationModeRealtime, engine.EvaluationModeBatch)
	}
}

// evaluatesUnder reports whether a customer in this status is evaluated by a
// pass running in the given mode. the data model §1.1.2: a closed customer's
// TM evaluation stops entirely, and a dormant customer is evaluated only
// "取引発生時" — the realtime path — never on a batch pass. This mirrors
// evaluateCustomerBatch in api/internal/batch/scheduler.go so the scheduled
// job and this endpoint cannot drift apart. frozen customers are evaluated in
// both modes.
func evaluatesUnder(status domain.CustomerStatus, mode engine.EvaluationMode) bool {
	switch status {
	case domain.CustomerStatusClosed:
		return false
	case domain.CustomerStatusDormant:
		return mode != engine.EvaluationModeBatch
	default:
		return true
	}
}

// nonBaseCurrencyTxn returns the first transaction whose currency is not the
// configured TM base currency (the PH9 aggregation invariant, see
// Server.tmBaseCurrency). The engine sums nominal amounts, so a mixed-currency
// snapshot would be compared against base-currency thresholds and produce a
// detection result that is simply wrong. Both the realtime ingest path and the
// batch pass fail-alert on it rather than evaluating it.
func (s *Server) nonBaseCurrencyTxn(txns []domain.Transaction) (domain.Transaction, bool) {
	if s.tmBaseCurrency == "" {
		return domain.Transaction{}, false
	}
	for _, txn := range txns {
		if !strings.EqualFold(txn.Currency, s.tmBaseCurrency) {
			return txn, true
		}
	}
	return domain.Transaction{}, false
}

func (s *Server) errNonBaseCurrency(txn domain.Transaction) error {
	return fmt.Errorf("currency %s is not normalized to %s", txn.Currency, s.tmBaseCurrency)
}

// monitoringEventHorizon is how far past the snapshot instant the event-time
// window reaches. transactionhistory.Query requires an upper bound, but this
// endpoint has none to give: the ingestion cutoff is what makes the snapshot
// deterministic, and a transaction carrying a future executed_at is still part
// of the customer's history the engine must see.
const monitoringEventHorizon = 100 * 365 * 24 * time.Hour

// snapshotMonitoringHistory returns every transaction ingested for a customer
// through snapshotAt, in event-time order.
//
// It replaces a single ListByCustomer(..., maxBatchCustomers, 0), which
// returned only the newest 1000 rows ordered by executed_at DESC. Anything
// older was dropped without a word, which invalidates every long-window and
// dormancy scenario for an active customer and makes a history backfill report
// success over data it never looked at.
//
// transactionhistory.ListCustomerTransactionsAsOf is the same helper the
// scheduled TM batch job uses (snapshotCustomerTransactions in
// api/internal/batch/scheduler.go); it pages exhaustively via the keyset
// history capability, or by offset on repositories without it.
func (s *Server) snapshotMonitoringHistory(ctx context.Context, customerID string, snapshotAt time.Time) ([]domain.Transaction, error) {
	snapshotAt = snapshotAt.UTC()
	return transactionhistory.ListCustomerTransactionsAsOf(ctx, s.transactions, customerID, transactionhistory.Query{
		From:           time.Time{},
		To:             snapshotAt.Add(monitoringEventHorizon),
		CreatedThrough: snapshotAt,
	})
}

type batchMonitorResult struct {
	CustomerID    string `json:"customer_id"`
	AlertsRaised  int    `json:"alerts_raised"`
	PendingReview bool   `json:"pending_review,omitempty"`
	Error         string `json:"error,omitempty"`
}

type batchMonitorResponse struct {
	Total           int                  `json:"total"`
	Succeeded       int                  `json:"succeeded"`
	Failed          int                  `json:"failed"`
	QueuedForReview int                  `json:"queued_for_review"`
	AlertsTotal     int                  `json:"alerts_total"`
	Results         []batchMonitorResult `json:"results"`
	Duration        string               `json:"duration"`
}

func (s *Server) handleBatchMonitor(w http.ResponseWriter, r *http.Request) {
	if s.monitoring == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "monitoring engine not configured")
		return
	}

	start := time.Now()

	var req batchMonitorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	if len(req.CustomerIDs) > maxBatchCustomers {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "too many customer_ids (max 1000)")
		return
	}

	mode, err := parseBatchMonitorMode(req.Mode)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	ctx := r.Context()

	var customers []domain.Customer
	if len(req.CustomerIDs) > 0 {
		for _, id := range req.CustomerIDs {
			c, err := s.customers.Get(ctx, id)
			if err != nil {
				continue
			}
			customers = append(customers, *c)
		}
	} else {
		var err error
		customers, err = s.customers.List(ctx, maxBatchCustomers, 0)
		if err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
	}

	resp := batchMonitorResponse{
		Total: len(customers),
	}

	// reviewRunID labels any AnnotateBatchReviewed call made during this
	// handler invocation (Task4/Task7 dedup routing); it is not persisted to
	// batch_runs since this HTTP-triggered pass isn't tracked there (unlike
	// the scheduled batch.RunTMBatchEvaluation job).
	reviewRunID := generateID()
	var pendingIndex map[string][]domain.PendingEvaluation
	bulkPendingLoaded := false
	bulkPendingAvailable := false

	// failAlert routes a customer whose evaluation could not be trusted into
	// PENDING_REVIEW, and counts it as a hard failure only when that write is
	// unavailable (OPS-005, the operational design §4.4 Fail-Alert).
	failAlert := func(c *domain.Customer, txns []domain.Transaction, cause error) {
		if !bulkPendingLoaded {
			var preloadErr error
			pendingIndex, bulkPendingAvailable, preloadErr = loadPendingReviewIndex(ctx, s.pendingEvals, customers)
			if preloadErr != nil {
				slog.WarnContext(ctx, "bulk pending-review preload failed; continuing without per-customer reads", "error", preloadErr)
			}
			bulkPendingLoaded = true
		}
		queued := false
		if bulkPendingAvailable {
			queued = s.queuePendingReviewFromIndex(ctx, c, txns, cause, pendingIndex)
		} else {
			queued = s.queuePendingReview(ctx, c, txns, cause)
		}
		if queued {
			resp.QueuedForReview++
			resp.Results = append(resp.Results, batchMonitorResult{
				CustomerID:    c.ID,
				PendingReview: true,
			})
			return
		}
		resp.Failed++
		resp.Results = append(resp.Results, batchMonitorResult{
			CustomerID: c.ID,
			Error:      cause.Error(),
		})
	}

	for _, c := range customers {
		if !evaluatesUnder(c.EffectiveStatus(), mode) {
			resp.Succeeded++
			resp.Results = append(resp.Results, batchMonitorResult{
				CustomerID:   c.ID,
				AlertsRaised: 0,
			})
			continue
		}

		txns, err := s.snapshotMonitoringHistory(ctx, c.ID, start)
		if err != nil {
			resp.Failed++
			resp.Results = append(resp.Results, batchMonitorResult{
				CustomerID: c.ID,
				Error:      err.Error(),
			})
			continue
		}

		if len(txns) == 0 {
			resp.Succeeded++
			resp.Results = append(resp.Results, batchMonitorResult{
				CustomerID:   c.ID,
				AlertsRaised: 0,
			})
			continue
		}

		if offending, ok := s.nonBaseCurrencyTxn(txns); ok {
			// Same treatment the realtime ingest path gives a mixed-currency
			// snapshot (monitorCreatedTransaction): the aggregation cannot be
			// trusted, so route it to review instead of reporting a clean pass.
			failAlert(&c, txns, s.errNonBaseCurrency(offending))
			continue
		}

		alerts, err := s.evaluateMonitoring(ctx, &c, txns, mode, nil)
		if err != nil {
			failAlert(&c, txns, err)
			continue
		}

		for _, a := range alerts {
			a.ID = generateID()
			now := time.Now()
			a.CreatedAt = now
			a.UpdatedAt = now
			if a.DetectedAt.IsZero() {
				a.DetectedAt = now
			}
			windowStart := domain.DailyAggregationWindowStart(a.DetectedAt)
			a.AggregationWindowStart = &windowStart
			if _, err := s.applyWhitelistSuppression(ctx, &a); err != nil {
				continue
			}
			created, existing, err := s.alerts.CreateIfNotDuplicate(ctx, &a)
			if err != nil {
				continue
			}
			if !created {
				if existing != nil {
					_ = s.alerts.AnnotateBatchReviewed(ctx, existing.ID, reviewRunID)
				}
				continue
			}
			recordAlertCreated(&a)
			s.consolidateAlertIntoCase(ctx, &a)
			s.dispatchWebhook(ctx, domain.WebhookEventAlertCreated, a)
			s.notifyAlertCreated(ctx, a)
		}

		resp.Succeeded++
		resp.AlertsTotal += len(alerts)
		resp.Results = append(resp.Results, batchMonitorResult{
			CustomerID:   c.ID,
			AlertsRaised: len(alerts),
		})
	}

	resp.Duration = time.Since(start).String()

	writeJSON(w, http.StatusOK, resp)
}
