package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
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
		writeError(w, http.StatusServiceUnavailable, "scoring engine not configured")
		return
	}

	start := time.Now()

	var req batchScoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(req.CustomerIDs) > maxBatchCustomers {
		writeError(w, http.StatusBadRequest, "too many customer_ids (max 1000)")
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
			writeError(w, http.StatusInternalServerError, err.Error())
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
// (OPS-005, overview.md §4.4 Fail-Alert) when the monitoring engine call
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

	pe := &domain.PendingEvaluation{
		ID:             generateID(),
		CustomerID:     c.ID,
		TransactionIDs: txIDs,
		Status:         domain.PendingEvaluationStatusPendingReview,
		Reason:         "engine unavailable: " + cause.Error(),
	}

	if err := s.pendingEvals.Create(ctx, pe); err != nil {
		return false
	}
	return true
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
		writeError(w, http.StatusServiceUnavailable, "monitoring engine not configured")
		return
	}

	start := time.Now()

	var req batchMonitorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(req.CustomerIDs) > maxBatchCustomers {
		writeError(w, http.StatusBadRequest, "too many customer_ids (max 1000)")
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
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	resp := batchMonitorResponse{
		Total: len(customers),
	}

	for _, c := range customers {
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

		riskTier := domain.RiskTierLow
		if c.RiskTier != nil {
			riskTier = *c.RiskTier
		}
		alerts, err := s.monitoring.EvaluateTransactions(ctx, c.ID, riskTier, txns, nil)
		if err != nil {
			if s.queuePendingReview(ctx, &c, txns, err) {
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
			if _, err := s.applyWhitelistSuppression(ctx, &a); err != nil {
				continue
			}
			if err := s.alerts.Create(ctx, &a); err != nil {
				continue
			}
			recordAlertCreated(&a)
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
