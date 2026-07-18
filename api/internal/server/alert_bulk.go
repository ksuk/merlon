package server

import (
	"encoding/json"
	"errors"
	"github.com/ksuk/merlon/api/internal/apierr"
	"log/slog"
	"net/http"
	"strings"
	"time"

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
func (s *Server) handleBulkCloseAlerts(w http.ResponseWriter, r *http.Request) {
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
		if err := s.alerts.UpdateStatus(r.Context(), a.ID, domain.AlertStatusClosedFalsePositive, userID); err != nil {
			slog.ErrorContext(r.Context(), "bulk close: update status failed", "alert_id", a.ID, "error", err)
			continue
		}
		closedIDs = append(closedIDs, a.ID)
		s.recordBulkAuditEntry(r, "bulk_close_alert", a.ID, map[string]string{"reason": req.Reason}, userID, ip, ua)
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
func (s *Server) handleBulkCaseAssignment(w http.ResponseWriter, r *http.Request) {
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
		summary := req.Summary
		if summary == "" {
			summary = "Bulk case assignment"
		}
		now := time.Now()
		targetCase = &domain.Case{
			ID:         generateID(),
			CustomerID: req.CustomerID,
			Status:     domain.CaseStatusNew,
			Priority:   domain.CasePriorityMedium,
			Summary:    summary,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := s.cases.Create(r.Context(), targetCase); err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		created = true
	}

	userID := resolveAuditUserID(r)
	ip := extractIP(r)
	ua := r.UserAgent()

	existingIDs := make(map[string]bool, len(targetCase.AlertIDs))
	for _, id := range targetCase.AlertIDs {
		existingIDs[id] = true
	}

	for _, alertID := range req.AlertIDs {
		if _, err := s.alerts.Get(r.Context(), alertID); err != nil {
			continue // skip unknown alerts rather than failing the whole batch
		}
		if !existingIDs[alertID] {
			targetCase.AlertIDs = append(targetCase.AlertIDs, alertID)
			existingIDs[alertID] = true
		}
		s.recordBulkAuditEntry(r, "bulk_case_assignment", alertID, map[string]string{"case_id": targetCase.ID}, userID, ip, ua)
	}

	targetCase.UpdatedAt = time.Now()
	if err := s.cases.Update(r.Context(), targetCase); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, bulkCaseAssignmentResponse{CaseID: targetCase.ID, Created: created})
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
