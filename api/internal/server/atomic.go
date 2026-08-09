package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/requestid"
)

var errAtomicMutationUnavailable = errors.New("atomic mutation storage is not configured")

func (s *Server) runAtomic(ctx context.Context, fn func(domain.AtomicMutationRepositories) error) error {
	if s.atomic == nil {
		return errAtomicMutationUnavailable
	}
	return s.atomic.RunAtomic(ctx, fn)
}

func correlationID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if id := strings.TrimSpace(requestid.FromContext(r.Context())); id != "" {
		return id
	}
	if id := strings.TrimSpace(r.Header.Get("X-Request-ID")); id != "" {
		return id
	}
	id := generateID()
	r.Header.Set("X-Request-ID", id)
	return id
}

func prepareCaseEvent(r *http.Request, event *domain.CaseEvent) error {
	if event == nil || strings.TrimSpace(event.CaseID) == "" || strings.TrimSpace(event.EventType) == "" {
		return fmt.Errorf("case event case_id and event_type are required")
	}
	if event.ID == "" {
		event.ID = generateID()
	}
	if event.Actor == "" {
		event.Actor = resolveAuditUserID(r)
	}
	if event.CorrelationID == "" {
		event.CorrelationID = correlationID(r)
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	return nil
}

func appendRequiredCaseEvent(ctx context.Context, r *http.Request, repos domain.AtomicMutationRepositories, event *domain.CaseEvent) error {
	if repos.Investigation == nil || repos.Audit == nil {
		return errAtomicMutationUnavailable
	}
	if err := prepareCaseEvent(r, event); err != nil {
		return err
	}
	if err := repos.Investigation.AppendEvent(ctx, event); err != nil {
		return fmt.Errorf("append case event %s: %w", event.EventType, err)
	}
	action := "case_event_" + event.EventType
	switch event.EventType {
	case "created":
		action = "create"
	case "status_changed", "reopened", "str_filed":
		action = "update_status"
	case "note_added":
		action = "add_note"
	}
	if err := repos.Audit.Create(ctx, &domain.AuditEntry{
		UserID: resolveAuditUserID(r), Action: action,
		ResourceType: "cases", ResourceID: event.CaseID,
		Details: map[string]string{
			"event_id": event.ID, "event_type": event.EventType, "reason": event.Reason, "correlation_id": event.CorrelationID,
		},
		IPAddress: extractIP(r), UserAgent: r.UserAgent(), CreatedAt: event.CreatedAt,
	}); err != nil {
		return err
	}
	markAtomicAuditHandled(r)
	return nil
}

func appendRequiredAlertDecision(ctx context.Context, r *http.Request, repos domain.AtomicMutationRepositories, before, after *domain.Alert, rationale string) error {
	if repos.AlertDecisions == nil || repos.Audit == nil {
		return errAtomicMutationUnavailable
	}
	if before == nil || after == nil {
		return fmt.Errorf("alert decision requires before and after states")
	}
	event := &domain.AlertDecisionEvent{
		ID: generateID(), AlertID: after.ID, FromStatus: before.Status, ToStatus: after.Status,
		Outcome: string(after.Status), Rationale: strings.TrimSpace(rationale),
		Actor: resolveAuditUserID(r), CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	decisions, err := repos.AlertDecisions.ListDecisions(ctx, after.ID)
	if err != nil {
		return fmt.Errorf("load alert decision history: %w", err)
	}
	if len(decisions) > 0 {
		event.SupersedesID = decisions[len(decisions)-1].ID
	}
	if err := repos.AlertDecisions.CreateDecision(ctx, event); err != nil {
		return fmt.Errorf("append alert decision: %w", err)
	}
	correlation := correlationID(r)
	if err := repos.Audit.Create(ctx, &domain.AuditEntry{
		UserID: event.Actor, Action: "alert_decision", ResourceType: "alerts", ResourceID: after.ID,
		Details: map[string]string{
			"decision_event_id": event.ID, "from_status": string(before.Status), "to_status": string(after.Status),
			"rationale": event.Rationale, "correlation_id": correlation,
		},
		IPAddress: extractIP(r), UserAgent: r.UserAgent(), CreatedAt: event.CreatedAt,
	}); err != nil {
		return err
	}
	markAtomicAuditHandled(r)
	return nil
}

func appendRequiredReportAudit(ctx context.Context, r *http.Request, repos domain.AtomicMutationRepositories, action string, report *domain.STRReport, details map[string]string) error {
	if repos.Audit == nil || report == nil {
		return errAtomicMutationUnavailable
	}
	if details == nil {
		details = map[string]string{}
	}
	details["correlation_id"] = correlationID(r)
	resourceID := report.ID
	if action == "create_str" {
		// Preserve the durable report identity in the audit resource. The
		// historical route-level value ("str") is retained in details for
		// clients that used the old generic middleware record.
		details["route_resource_id"] = "str"
	}
	if err := repos.Audit.Create(ctx, &domain.AuditEntry{
		UserID: resolveAuditUserID(r), Action: action, ResourceType: "reports", ResourceID: resourceID,
		Details: details, IPAddress: extractIP(r), UserAgent: r.UserAgent(), CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}); err != nil {
		return err
	}
	markAtomicAuditHandled(r)
	return nil
}

func appendRequiredCustomerStatusAudit(ctx context.Context, r *http.Request, repos domain.AtomicMutationRepositories, customerID, oldStatus, newStatus, reason string, createdAt time.Time) error {
	if repos.Audit == nil {
		return errAtomicMutationUnavailable
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	if err := repos.Audit.Create(ctx, &domain.AuditEntry{
		UserID: resolveAuditUserID(r), Action: "customer_status_change", ResourceType: "customer", ResourceID: customerID,
		Details: map[string]string{
			"old_status": oldStatus, "new_status": newStatus, "reason": reason,
			"correlation_id": correlationID(r),
		},
		IPAddress: extractIP(r), UserAgent: r.UserAgent(), CreatedAt: createdAt,
	}); err != nil {
		return fmt.Errorf("append customer status audit: %w", err)
	}
	markAtomicAuditHandled(r)
	return nil
}

func appendRequiredMutationAudit(ctx context.Context, r *http.Request, repos domain.AtomicMutationRepositories, action, resourceType, resourceID string, details map[string]string, createdAt time.Time) error {
	if repos.Audit == nil {
		return errAtomicMutationUnavailable
	}
	if details == nil {
		details = map[string]string{}
	}
	details["correlation_id"] = correlationID(r)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	if err := repos.Audit.Create(ctx, &domain.AuditEntry{
		UserID: resolveAuditUserID(r), Action: action, ResourceType: resourceType, ResourceID: resourceID,
		Details: details, IPAddress: extractIP(r), UserAgent: r.UserAgent(), CreatedAt: createdAt,
	}); err != nil {
		return fmt.Errorf("append %s audit: %w", action, err)
	}
	markAtomicAuditHandled(r)
	return nil
}

func appendRequiredRelationshipHistory(ctx context.Context, r *http.Request, repos domain.AtomicMutationRepositories, relationship *domain.CaseRelationship, eventType, reason string, before, after map[string]any) error {
	history, ok := repos.Investigation.(domain.CaseRelationshipHistoryRepository)
	if !ok || relationship == nil {
		return errAtomicMutationUnavailable
	}
	event := &domain.CaseRelationshipEvent{
		ID: generateID(), RelationshipID: relationship.ID, CaseID: relationship.CaseID,
		RelatedCaseID: relationship.RelatedCaseID, EventType: eventType, Actor: resolveAuditUserID(r),
		Reason: strings.TrimSpace(reason), Before: before, After: after,
		CorrelationID: correlationID(r), CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := history.AppendRelationshipEvent(ctx, event); err != nil {
		return fmt.Errorf("append relationship history: %w", err)
	}
	return nil
}

func appendRequiredSTRReportHistory(ctx context.Context, r *http.Request, repos domain.AtomicMutationRepositories, report *domain.STRReport, eventType, reason string, before, after map[string]any) error {
	history, ok := repos.Reports.(domain.STRReportHistoryRepository)
	if !ok || report == nil {
		return errAtomicMutationUnavailable
	}
	event := &domain.STRReportEvent{
		ID: generateID(), ReportID: report.ID, EventType: eventType, Actor: resolveAuditUserID(r),
		Reason: strings.TrimSpace(reason), Before: before, After: after,
		CorrelationID: correlationID(r), CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := history.AppendReportEvent(ctx, event); err != nil {
		return fmt.Errorf("append STR report history: %w", err)
	}
	return nil
}

func markAtomicAuditHandled(r *http.Request) {
	if r == nil {
		return
	}
	if sink, ok := r.Context().Value(auditDetailsKey{}).(*auditDetailsSink); ok {
		sink.markHandledByRoute()
	}
}

func appendRequiredCaseChangeEvents(ctx context.Context, r *http.Request, repos domain.AtomicMutationRepositories, before map[string]any, after *domain.Case, req updateCaseRequest) error {
	if after == nil {
		return fmt.Errorf("case update state is nil")
	}
	state := caseEventState(*after)
	if req.prioritySource != "" {
		state["priority_source"] = req.prioritySource
		state["priority_policy_version"] = req.priorityPolicyVersion
		if req.priorityOverrideRationale != "" {
			state["priority_override_rationale"] = req.priorityOverrideRationale
		}
	}
	relatedAlerts := append([]string(nil), after.AlertIDs...)
	relatedCases := append([]string(nil), after.RelatedCaseIDs...)
	relatedReports := nonEmptyIDs(after.STRReportID)
	recorded := false
	record := func(eventType, reason string) error {
		recorded = true
		return appendRequiredCaseEvent(ctx, r, repos, &domain.CaseEvent{
			CaseID: after.ID, EventType: eventType, Reason: reason, Before: before, After: state,
			RelatedAlertIDs: relatedAlerts, RelatedCaseIDs: relatedCases, RelatedReportIDs: relatedReports,
		})
	}
	if caseStateChanged(before, state, "status") {
		eventType := "status_changed"
		if after.Status == domain.CaseStatusStrFiled {
			eventType = "str_filed"
		} else if after.Status == domain.CaseStatusReopened {
			eventType = "reopened"
		}
		if err := record(eventType, caseRationale(req)); err != nil {
			return err
		}
	}
	if caseStateChanged(before, state, "assigned_to") || caseStateChanged(before, state, "assigned_team") || caseStateChanged(before, state, "due_at") {
		if err := record("assignment_changed", caseChangeReason(req)); err != nil {
			return err
		}
	}
	if caseStateChanged(before, state, "priority") {
		if err := record("priority_changed", caseChangeReason(req)); err != nil {
			return err
		}
	}
	if caseStateChanged(before, state, "summary") {
		if err := record("summary_changed", caseChangeReason(req)); err != nil {
			return err
		}
	}
	if caseStateChanged(before, state, "investigation_disposition") || caseStateChanged(before, state, "str_candidate") || caseStateChanged(before, state, "disposition_rationale") {
		if err := record("disposition_recorded", caseRationale(req)); err != nil {
			return err
		}
	}
	if !recorded {
		return record("updated", caseChangeReason(req))
	}
	return nil
}

func writeAtomicMutationError(w http.ResponseWriter, err error) {
	if errors.Is(err, errAtomicMutationUnavailable) {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, err.Error())
		return
	}
	var conflict *domain.ErrConflict
	if errors.As(err, &conflict) {
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, err.Error())
		return
	}
	var notFound *domain.ErrNotFound
	if errors.As(err, &notFound) {
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
		return
	}
	writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
}
