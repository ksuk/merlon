package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
)

// bulkCloseAlertsRequest filters alerts to close in bulk (the case-management workflow
// §アラートの一括処理: "フィルタ条件（シナリオID、期間、severity）で絞り込ん
// だアラートを一括で CLOSED にする"). Reason is required and recorded on
// every individual audit entry (not just once for the whole request).
type bulkCloseAlertsRequest struct {
	ScenarioID string               `json:"scenario_id,omitempty"`
	PeriodFrom *time.Time           `json:"period_from,omitempty"`
	PeriodTo   *time.Time           `json:"period_to,omitempty"`
	Severity   domain.AlertSeverity `json:"severity,omitempty"`
	Reason     string               `json:"reason"`
}

type bulkCloseAlertsResponse struct {
	ClosedCount int      `json:"closed_count"`
	AlertIDs    []string `json:"alert_ids"`
}

// handleBulkCloseAlerts implements POST /api/v1/alerts/bulk-close. Matched
// alerts already in a closed status are skipped (idempotent re-runs), and
// every alert actually closed gets its own audit log entry in addition to
// the single generic entry auditMiddleware writes for the request as a
// whole (the case-management workflow: "一括操作は個別アラートごとに監査ログを記録
// する" — the automatic per-request entry alone is not sufficient).
//
// Closed alerts are marked ClosedFalsePositive: a bulk close across a
// scenario/period/severity slice is the standard workflow for a
// known-benign pattern (e.g. recurring salary payments), which is a false
// positive determination, not a true positive one.
func (s *Server) handleBulkCloseAlertsLegacy(w http.ResponseWriter, r *http.Request) {
	if s.alerts == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "alerts not configured")
		return
	}

	var req bulkCloseAlertsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "reason is required")
		return
	}
	dispositionRepo, ok := s.alerts.(domain.AlertBulkDispositionRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "alert disposition storage is not configured")
		return
	}

	matched, err := s.alerts.ListByFilter(r.Context(), domain.AlertBulkFilter{
		ScenarioID: req.ScenarioID,
		PeriodFrom: req.PeriodFrom,
		PeriodTo:   req.PeriodTo,
		Severity:   req.Severity,
	})
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	userID := resolveAuditUserID(r)
	ip := extractIP(r)
	ua := r.UserAgent()

	var closedIDs []string
	for _, a := range matched {
		if isAlertClosed(a.Status) {
			continue
		}
		if err := dispositionRepo.CloseFalsePositiveWithRationale(r.Context(), a.ID, userID, strings.TrimSpace(req.Reason), &a.UpdatedAt); err != nil {
			slog.ErrorContext(r.Context(), "bulk close: update status failed", "alert_id", a.ID, "error", err)
			var conflict *domain.ErrConflict
			var invalid *domain.ErrInvalidStateTransition
			if errors.As(err, &conflict) || errors.As(err, &invalid) {
				writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, err.Error())
				return
			}
			var notFound *domain.ErrNotFound
			if errors.As(err, &notFound) {
				writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
				return
			}
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		closedIDs = append(closedIDs, a.ID)
		s.recordBulkAuditEntry(r, "bulk_close_alert", a.ID, map[string]string{"reason": req.Reason}, userID, ip, ua)
	}

	writeJSON(w, http.StatusOK, bulkCloseAlertsResponse{ClosedCount: len(closedIDs), AlertIDs: closedIDs})
}

func (s *Server) handleBulkCloseAlerts(w http.ResponseWriter, r *http.Request) {
	if s.alerts == nil || s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "alerts are not configured")
		return
	}
	var req bulkCloseAlertsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "reason is required")
		return
	}
	var closedIDs []string
	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Alerts == nil || repos.AlertDecisions == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		dispositionRepo, ok := repos.Alerts.(domain.AlertBulkDispositionRepository)
		if !ok {
			return errAtomicMutationUnavailable
		}
		matched, err := repos.Alerts.ListByFilter(r.Context(), domain.AlertBulkFilter{ScenarioID: req.ScenarioID, PeriodFrom: req.PeriodFrom, PeriodTo: req.PeriodTo, Severity: req.Severity})
		if err != nil {
			return err
		}
		for _, candidate := range matched {
			current, err := repos.Alerts.Get(r.Context(), candidate.ID)
			if err != nil {
				return err
			}
			if isAlertClosed(current.Status) {
				continue
			}
			if err := dispositionRepo.CloseFalsePositiveWithRationale(r.Context(), current.ID, resolveAuditUserID(r), req.Reason, &current.UpdatedAt); err != nil {
				return err
			}
			after, err := repos.Alerts.Get(r.Context(), current.ID)
			if err != nil {
				return err
			}
			if err := appendRequiredAlertDecision(r.Context(), r, repos, current, after, req.Reason); err != nil {
				return err
			}
			if err := repos.Audit.Create(r.Context(), &domain.AuditEntry{
				UserID: resolveAuditUserID(r), Action: "bulk_close_alert", ResourceType: "alert", ResourceID: current.ID,
				Details:   map[string]string{"reason": req.Reason, "correlation_id": correlationID(r)},
				IPAddress: extractIP(r), UserAgent: r.UserAgent(), CreatedAt: time.Now().UTC(),
			}); err != nil {
				return err
			}
			markAtomicAuditHandled(r)
			closedIDs = append(closedIDs, current.ID)
		}
		return nil
	}); err != nil {
		writeAtomicMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bulkCloseAlertsResponse{ClosedCount: len(closedIDs), AlertIDs: closedIDs})
}

func isAlertClosed(status domain.AlertStatus) bool {
	return status == domain.AlertStatusClosedTruePositive || status == domain.AlertStatusClosedFalsePositive
}

// bulkCaseAssignmentRequest either adds AlertIDs to an existing case
// (CaseID set) or bundles them into a newly created case (CaseID empty,
// CustomerID required) — the case-management workflow §アラートの一括処理: "選択した
// 複数アラートを既存ケースに追加、または新規ケースとしてまとめる".
type bulkCaseAssignmentRequest struct {
	AlertIDs   []string `json:"alert_ids"`
	CaseID     string   `json:"case_id,omitempty"`
	CustomerID string   `json:"customer_id,omitempty"`
	Summary    string   `json:"summary,omitempty"`
}

type bulkCaseAssignmentResponse struct {
	CaseID  string `json:"case_id"`
	Created bool   `json:"created"`
}

// handleBulkCaseAssignment implements POST /api/v1/alerts/bulk-case. Like
// handleBulkCloseAlerts, every alert added to the case gets its own audit
// entry (the case-management workflow's per-alert traceability requirement).
func (s *Server) handleBulkCaseAssignmentLegacy(w http.ResponseWriter, r *http.Request) {
	if s.alerts == nil || s.cases == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case management not configured")
		return
	}

	var req bulkCaseAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if len(req.AlertIDs) == 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "alert_ids is required")
		return
	}
	seenRequestIDs := make(map[string]struct{}, len(req.AlertIDs))
	for _, alertID := range req.AlertIDs {
		if _, duplicate := seenRequestIDs[alertID]; duplicate {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "alert_ids must not contain duplicates")
			return
		}
		seenRequestIDs[alertID] = struct{}{}
	}
	if s.caseAlertLifecycle == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, errCaseAlertLifecycleUnavailable.Error())
		return
	}

	var targetCase *domain.Case
	created := false

	if req.CaseID != "" {
		c, err := s.cases.Get(r.Context(), req.CaseID)
		if err != nil {
			var nf *domain.ErrNotFound
			if errors.As(err, &nf) {
				writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, "case not found")
				return
			}
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		targetCase = c
	} else {
		if req.CustomerID == "" {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer_id is required when case_id is not provided")
			return
		}
		if _, err := s.customers.Get(r.Context(), req.CustomerID); err != nil {
			var nf *domain.ErrNotFound
			if errors.As(err, &nf) {
				writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, "customer not found")
				return
			}
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		summary := req.Summary
		if summary == "" {
			summary = "Bulk case assignment"
		}
		now := time.Now()
		targetCase = &domain.Case{
			ID:         generateID(),
			CustomerID: req.CustomerID,
			AlertIDs:   append([]string(nil), req.AlertIDs...),
			Status:     domain.CaseStatusNew,
			Priority:   domain.CasePriorityMedium,
			Summary:    summary,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := s.caseAlertLifecycle.CreateCaseWithAlerts(r.Context(), targetCase); err != nil {
			writeCaseLinkError(w, err)
			return
		}
		created = true
	}

	userID := resolveAuditUserID(r)
	ip := extractIP(r)
	ua := r.UserAgent()

	if !created {
		updated, err := s.caseAlertLifecycle.AppendAlerts(r.Context(), targetCase.ID, targetCase.UpdatedAt, req.AlertIDs)
		if err != nil {
			writeCaseLinkError(w, err)
			return
		}
		targetCase = updated
	}

	for _, alertID := range req.AlertIDs {
		s.recordBulkAuditEntry(r, "bulk_case_assignment", alertID, map[string]string{"case_id": targetCase.ID}, userID, ip, ua)
	}

	writeJSON(w, http.StatusOK, bulkCaseAssignmentResponse{CaseID: targetCase.ID, Created: created})
}

func (s *Server) handleBulkCaseAssignment(w http.ResponseWriter, r *http.Request) {
	if s.alerts == nil || s.cases == nil || s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case management not configured")
		return
	}
	var req bulkCaseAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if len(req.AlertIDs) == 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "alert_ids is required")
		return
	}
	seen := make(map[string]struct{}, len(req.AlertIDs))
	for i, alertID := range req.AlertIDs {
		req.AlertIDs[i] = domain.CanonicalIdentifier(strings.TrimSpace(alertID))
		if req.AlertIDs[i] == "" {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "alert_ids must not contain empty values")
			return
		}
		if _, exists := seen[req.AlertIDs[i]]; exists {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "alert_ids must not contain duplicates")
			return
		}
		seen[req.AlertIDs[i]] = struct{}{}
	}
	if req.CaseID == "" && req.CustomerID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer_id is required when case_id is not provided")
		return
	}
	var target *domain.Case
	created := false
	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Alerts == nil || repos.Cases == nil || repos.CaseAlertLifecycle == nil || repos.Investigation == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if req.CaseID != "" {
			var err error
			target, err = repos.Cases.Get(r.Context(), req.CaseID)
			if err != nil {
				return err
			}
			updated, err := repos.CaseAlertLifecycle.AppendAlerts(r.Context(), target.ID, target.UpdatedAt, req.AlertIDs)
			if err != nil {
				return err
			}
			target = updated
		} else {
			if _, err := repos.Customers.Get(r.Context(), req.CustomerID); err != nil {
				return err
			}
			summary := strings.TrimSpace(req.Summary)
			if summary == "" {
				summary = "Bulk case assignment"
			}
			now := time.Now().UTC()
			target = &domain.Case{ID: generateID(), CustomerID: req.CustomerID, AlertIDs: append([]string(nil), req.AlertIDs...), Status: domain.CaseStatusNew, Priority: domain.CasePriorityMedium, Summary: summary, CreatedAt: now, UpdatedAt: now}
			if err := repos.CaseAlertLifecycle.CreateCaseWithAlerts(r.Context(), target); err != nil {
				return err
			}
			created = true
			if err := appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{CaseID: target.ID, EventType: "created", After: caseEventState(*target), RelatedAlertIDs: append([]string(nil), target.AlertIDs...)}); err != nil {
				return err
			}
		}
		for _, alertID := range req.AlertIDs {
			if err := repos.Audit.Create(r.Context(), &domain.AuditEntry{UserID: resolveAuditUserID(r), Action: "bulk_case_assignment", ResourceType: "alert", ResourceID: alertID, Details: map[string]string{"case_id": target.ID, "correlation_id": correlationID(r)}, IPAddress: extractIP(r), UserAgent: r.UserAgent(), CreatedAt: time.Now().UTC()}); err != nil {
				return err
			}
			markAtomicAuditHandled(r)
		}
		if !created {
			return appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{CaseID: target.ID, EventType: "alerts_added", Reason: "bulk case assignment", RelatedAlertIDs: append([]string(nil), req.AlertIDs...), After: caseEventState(*target)})
		}
		return nil
	}); err != nil {
		writeCaseLinkError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bulkCaseAssignmentResponse{CaseID: target.ID, Created: created})
}

// recordBulkAuditEntry writes one audit log entry for a single alert
// affected by a bulk operation (the case-management workflow: per-alert traceability
// even within a bulk request).
func (s *Server) recordBulkAuditEntry(r *http.Request, action, alertID string, details map[string]string, userID, ip, userAgent string) {
	if s.audit == nil {
		return
	}
	entry := &domain.AuditEntry{
		UserID:       userID,
		Action:       action,
		ResourceType: "alert",
		ResourceID:   alertID,
		Details:      details,
		IPAddress:    ip,
		UserAgent:    userAgent,
		CreatedAt:    time.Now(),
	}
	if err := s.audit.Create(r.Context(), entry); err != nil {
		slog.ErrorContext(r.Context(), "bulk operation audit write failed", "action", action, "alert_id", alertID, "error", err)
	}
}
