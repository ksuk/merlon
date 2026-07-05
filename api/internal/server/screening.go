package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/screening"
)

// ScreeningCheckRequest is the body for POST /api/v1/screening/check, the
// explicit-request immediate rescreen trigger (screening.md 即時再照合契機
// "基幹からの明示的リクエスト").
type ScreeningCheckRequest struct {
	CustomerID string   `json:"customer_id"`
	ListIDs    []string `json:"list_ids"`
}

func (s *Server) handleScreeningCheck(w http.ResponseWriter, r *http.Request) {
	if s.screening == nil {
		writeError(w, http.StatusServiceUnavailable, "screening engine not configured")
		return
	}

	var req ScreeningCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.CustomerID == "" {
		writeError(w, http.StatusBadRequest, "customer_id is required")
		return
	}

	deps := screening.SchedulerDeps{
		Customers:        s.customers,
		Screening:        s.screening,
		Results:          s.screeningResults,
		ListIDs:          req.ListIDs,
		TargetCustomerID: req.CustomerID,
	}

	result, err := screening.RunRescreeningBatch(r.Context(), deps, screening.TriggerAPIRequest)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// UpdateScreeningResultRequest is the body for
// PATCH /api/v1/screening/results/{id}, the screening hit investigation
// workflow transition (screening.md §スクリーニングヒット後の調査ワークフロー).
type UpdateScreeningResultRequest struct {
	Status              domain.ScreeningResultStatus `json:"status"`
	FalsePositiveReason string                       `json:"false_positive_reason,omitempty"`
	ReviewedBy          string                       `json:"reviewed_by,omitempty"`
}

func (s *Server) handleUpdateScreeningResult(w http.ResponseWriter, r *http.Request) {
	if s.screeningResults == nil {
		writeError(w, http.StatusServiceUnavailable, "screening result store not configured")
		return
	}

	id := r.PathValue("id")
	record, err := s.screeningResults.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req UpdateScreeningResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if err := record.ApplyStatusTransition(req.Status, req.FalsePositiveReason); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	now := time.Now()
	if req.ReviewedBy != "" {
		record.ReviewedBy = req.ReviewedBy
	}
	record.ReviewedAt = &now

	if err := s.screeningResults.Update(r.Context(), record); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if record.Status == domain.ScreeningResultStatusTruePositive {
		s.onScreeningTruePositive(r.Context(), record, now)
	}

	writeJSON(w, http.StatusOK, record)
}

// onScreeningTruePositive auto-creates a case and notifies the core system
// via webhook when a screening hit is confirmed a true positive
// (screening.md "自動的にケース管理にケースを生成し（severity = CRITICAL）、該当顧客の
// 取引を即時凍結の判断を基幹に通知する（Webhook screening_true_positive イベント）").
func (s *Server) onScreeningTruePositive(ctx context.Context, record *domain.ScreeningResultRecord, now time.Time) {
	if s.cases != nil {
		// TODO(WS-7): domain.CasePriority has no CRITICAL level yet; use the
		// highest available priority (High) until CasePriority gains one.
		c := &domain.Case{
			ID:         generateID(),
			CustomerID: record.CustomerID,
			Status:     domain.CaseStatusNew,
			Priority:   domain.CasePriorityHigh,
			Summary:    fmt.Sprintf("Screening true positive: %s matched %s (%s)", record.MatchedName, record.ListID, record.ListType),
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := s.cases.Create(ctx, c); err != nil {
			slog.Error("failed to auto-create case for true-positive screening hit",
				"screening_result_id", record.ID, "customer_id", record.CustomerID, "error", err)
		} else {
			adjustCasesOpenGauge("", c.Status)
		}
	}

	s.dispatchWebhook(ctx, domain.WebhookEventScreeningTruePositive, record)
}
