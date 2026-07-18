package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
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

	for _, c := range customers {
		// the data model §1.1.2: a closed customer's TM evaluation stops
		// entirely (existing records are kept for the retention period, but
		// no further scoring/alerting happens). This handler represents the
		// realtime "取引発生時" path, so frozen/dormant customers are still
		// evaluated here — only closed is excluded.
		if c.EffectiveStatus() == domain.CustomerStatusClosed {
			resp.Succeeded++
			resp.Results = append(resp.Results, batchMonitorResult{
				CustomerID:   c.ID,
				AlertsRaised: 0,
			})
			continue
		}

		txns, err := s.transactions.ListByCustomer(ctx, c.ID, maxBatchCustomers, 0)
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

		alerts, err := s.evaluateMonitoring(ctx, &c, txns, engine.EvaluationModeRealtime, nil)
		if err != nil {
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
				queued = s.queuePendingReviewFromIndex(ctx, &c, txns, err, pendingIndex)
			} else {
				queued = s.queuePendingReview(ctx, &c, txns, err)
			}
			if queued {
				resp.QueuedForReview++
				resp.Results = append(resp.Results, batchMonitorResult{
					CustomerID:    c.ID,
					PendingReview: true,
				})
				continue
			}
			resp.Failed++
			resp.Results = append(resp.Results, batchMonitorResult{
				CustomerID: c.ID,
				Error:      err.Error(),
			})
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
