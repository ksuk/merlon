package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ksuk/merlon/api/internal/apierr"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/screening"
)

// ScreeningCheckRequest is the body for POST /api/v1/screening/check, the
// explicit-request immediate rescreen trigger (the screening workflow 即時再照合契機
// "基幹からの明示的リクエスト").
type ScreeningCheckRequest struct {
	CustomerID string   `json:"customer_id"`
	ListIDs    []string `json:"list_ids"`
}

type screeningBatchResponse struct {
	Trigger  screening.TriggerType   `json:"trigger"`
	Outcomes []screeningBatchOutcome `json:"outcomes"`
}

type screeningBatchOutcome struct {
	CustomerID string `json:"customer_id"`
	Screened   bool   `json:"screened"`
	Skipped    bool   `json:"skipped"`
	SkipReason string `json:"skip_reason,omitempty"`
	Error      string `json:"error,omitempty"`
}

func screeningBatchResponseFrom(result screening.BatchResult) screeningBatchResponse {
	out := screeningBatchResponse{Trigger: result.Trigger, Outcomes: make([]screeningBatchOutcome, 0, len(result.Outcomes))}
	for _, outcome := range result.Outcomes {
		item := screeningBatchOutcome{CustomerID: outcome.CustomerID, Screened: outcome.Screened, Skipped: outcome.Skipped, SkipReason: outcome.SkipReason}
		if outcome.Err != nil {
			item.Error = outcome.Err.Error()
		}
		out.Outcomes = append(out.Outcomes, item)
	}
	return out
}

// persistScreeningRunAtomic commits the durable run/results and the required
// audit/outbox evidence together. The request is optional for the scheduler;
// when present it also prevents route middleware from appending a duplicate
// generic audit record.
func (s *Server) persistScreeningRunAtomic(ctx context.Context, request *http.Request, run *domain.ScreeningRun, results []domain.ScreeningResultRecord) error {
	if s.atomic == nil {
		return errAtomicMutationUnavailable
	}
	return s.atomic.RunAtomic(ctx, func(repos domain.AtomicMutationRepositories) error {
		workflow := repos.Wave3
		if workflow == nil {
			return errAtomicMutationUnavailable
		}
		if err := workflow.PersistScreeningRun(ctx, run, results); err != nil {
			return err
		}
		if repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		createdAt := time.Now().UTC()
		correlation := ""
		actor := run.Actor
		if request != nil {
			correlation = correlationID(request)
			actor = resolveAuditUserID(request)
		}
		action := "screening_run_completed"
		topic := "screening.run.completed"
		if run.Status == domain.ScreeningRunFailed || run.Status == domain.ScreeningRunPartial {
			action = "screening_run_failed"
			topic = "screening.run.failed"
		}
		if err := repos.Audit.Create(ctx, &domain.AuditEntry{UserID: actor, Action: action, ResourceType: "screening_runs", ResourceID: run.ID, Details: map[string]string{"customer_id": run.CustomerID, "result_count": fmt.Sprint(len(results)), "error": run.Error, "correlation_id": correlation}, CreatedAt: createdAt}); err != nil {
			return fmt.Errorf("append screening run audit: %w", err)
		}
		if s.eventOutbox != nil {
			if repos.EventOutbox == nil {
				return errAtomicMutationUnavailable
			}
			payload, err := json.Marshal(map[string]any{"run": run, "results": results})
			if err != nil {
				return err
			}
			if err := repos.EventOutbox.Enqueue(ctx, &domain.DurableEvent{ID: generateID(), Topic: topic, Payload: payload, ChainID: correlation, CreatedAt: createdAt}); err != nil {
				return fmt.Errorf("enqueue screening run event: %w", err)
			}
		}
		if request != nil {
			markAtomicAuditHandled(request)
		}
		return nil
	})
}

// PersistScreeningRun is the scheduler-facing durable write boundary. Keeping
// it on Server ensures scheduled and request-triggered screening use the same
// audit/outbox transaction contract.
func (s *Server) PersistScreeningRun(ctx context.Context, run *domain.ScreeningRun, results []domain.ScreeningResultRecord) error {
	return s.persistScreeningRunAtomic(ctx, nil, run, results)
}

func (s *Server) handleScreeningCheck(w http.ResponseWriter, r *http.Request) {
	if s.screening == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "screening engine not configured")
		return
	}

	var req ScreeningCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if req.CustomerID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer_id is required")
		return
	}

	deps := screening.SchedulerDeps{
		Customers:        s.customers,
		Screening:        s.screening,
		Results:          s.screeningResults,
		Workflow:         s.wave3,
		ConfigDigests:    s.configDigests,
		Actor:            resolveAuditUserID(r),
		ListIDs:          req.ListIDs,
		TargetCustomerID: req.CustomerID,
	}
	if s.wave3 != nil {
		deps.PersistWorkflow = func(ctx context.Context, run *domain.ScreeningRun, results []domain.ScreeningResultRecord) error {
			return s.persistScreeningRunAtomic(ctx, r, run, results)
		}
	}

	result, err := screening.RunRescreeningBatch(r.Context(), deps, screening.TriggerAPIRequest)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	for _, outcome := range result.Outcomes {
		if outcome.Err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeEngineError, "screening run failed: "+outcome.Err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, screeningBatchResponseFrom(result))
}

// UpdateScreeningResultRequest is the body for
// PATCH /api/v1/screening/results/{id}, the screening hit investigation
// workflow transition (the screening workflow §スクリーニングヒット後の調査ワークフロー).
type UpdateScreeningResultRequest struct {
	Status              domain.ScreeningResultStatus `json:"status"`
	FalsePositiveReason string                       `json:"false_positive_reason,omitempty"`
	ReviewedBy          string                       `json:"reviewed_by,omitempty"`
	Rationale           string                       `json:"rationale,omitempty"`
	ExpectedVersion     int                          `json:"expected_version,omitempty"`
}

func (s *Server) handleUpdateScreeningResult(w http.ResponseWriter, r *http.Request) {
	if s.screeningResults == nil && s.wave3 == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "screening result store not configured")
		return
	}

	id := r.PathValue("id")
	var req UpdateScreeningResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}

	if workflow := s.wave3; workflow != nil {
		if req.ExpectedVersion <= 0 {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "expected_version is required")
			return
		}
		rationale := strings.TrimSpace(firstNonEmpty(req.Rationale, req.FalsePositiveReason))
		if rationale == "" {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "rationale is required")
			return
		}
		var outcome *domain.ScreeningReviewOutcome
		mutate := func(repos domain.AtomicMutationRepositories) error {
			wf, ok := repos.Wave3.(domain.ScreeningWorkflowRepository)
			if !ok {
				wf, ok = workflow.(domain.ScreeningWorkflowRepository)
			}
			if !ok {
				return errAtomicMutationUnavailable
			}
			var err error
			outcome, err = wf.ReviewScreeningResult(r.Context(), id, req.Status, rationale, resolveAuditUserID(r), req.ExpectedVersion)
			if err != nil {
				return err
			}
			if repos.Audit == nil {
				return errAtomicMutationUnavailable
			}
			createdAt := time.Now().UTC()
			if err := repos.Audit.Create(r.Context(), &domain.AuditEntry{UserID: resolveAuditUserID(r), Action: "screening_review", ResourceType: "screening_results", ResourceID: id, Details: map[string]string{"status": string(req.Status), "rationale": rationale, "correlation_id": correlationID(r)}, CreatedAt: createdAt}); err != nil {
				return fmt.Errorf("append screening review audit: %w", err)
			}
			if s.eventOutbox != nil {
				if repos.EventOutbox == nil {
					return errAtomicMutationUnavailable
				}
				payload, err := json.Marshal(outcome)
				if err != nil {
					return err
				}
				if err := repos.EventOutbox.Enqueue(r.Context(), &domain.DurableEvent{ID: generateID(), Topic: "screening.result.reviewed", Payload: payload, ChainID: correlationID(r), CreatedAt: createdAt}); err != nil {
					return fmt.Errorf("enqueue screening review event: %w", err)
				}
			}
			markAtomicAuditHandled(r)
			return nil
		}
		var err error
		if s.atomic != nil {
			err = s.runAtomic(r.Context(), mutate)
		} else {
			err = mutate(domain.AtomicMutationRepositories{Wave3: workflow, Audit: s.audit, EventOutbox: s.eventOutbox})
		}
		if err != nil {
			var notFound *domain.ErrNotFound
			var conflict *domain.ErrConflict
			switch {
			case errors.As(err, &notFound):
				writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			case errors.As(err, &conflict):
				writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, err.Error())
			default:
				writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, outcome)
		return
	}

	record, err := s.screeningResults.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	if err := record.ApplyStatusTransition(req.Status, req.FalsePositiveReason); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	now := time.Now()
	record.ReviewedBy = resolveAuditUserID(r)
	record.ReviewedAt = &now

	if err := s.screeningResults.Update(r.Context(), record); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	if record.Status == domain.ScreeningResultStatusTruePositive {
		s.onScreeningTruePositive(r.Context(), record, now)
	}

	writeJSON(w, http.StatusOK, record)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// onScreeningTruePositive auto-creates a case and notifies the core system
// via webhook when a screening hit is confirmed a true positive
// (the screening workflow "自動的にケース管理にケースを生成し（severity = CRITICAL）、該当顧客の
// 取引を即時凍結の判断を基幹に通知する（Webhook screening_true_positive イベント）").
func (s *Server) onScreeningTruePositive(ctx context.Context, record *domain.ScreeningResultRecord, now time.Time) {
	if s.cases != nil {
		c := &domain.Case{
			ID:         generateID(),
			CustomerID: record.CustomerID,
			Status:     domain.CaseStatusNew,
			Priority:   domain.CasePriorityCritical,
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
