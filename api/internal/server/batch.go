package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

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
		customers, err = s.customers.List(ctx, 10000, 0)
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

type batchMonitorRequest struct {
	CustomerIDs []string `json:"customer_ids,omitempty"`
}

type batchMonitorResult struct {
	CustomerID   string `json:"customer_id"`
	AlertsRaised int    `json:"alerts_raised"`
	Error        string `json:"error,omitempty"`
}

type batchMonitorResponse struct {
	Total       int                  `json:"total"`
	Succeeded   int                  `json:"succeeded"`
	Failed      int                  `json:"failed"`
	AlertsTotal int                  `json:"alerts_total"`
	Results     []batchMonitorResult `json:"results"`
	Duration    string               `json:"duration"`
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
		customers, err = s.customers.List(ctx, 10000, 0)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	resp := batchMonitorResponse{
		Total: len(customers),
	}

	for _, c := range customers {
		txns, err := s.transactions.ListByCustomer(ctx, c.ID, 10000, 0)
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
			s.alerts.Create(ctx, &a)
			s.dispatchWebhook(ctx, domain.WebhookEventAlertCreated, a)
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
