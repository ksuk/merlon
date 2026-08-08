package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ksuk/merlon/api/internal/apierr"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/casemgmt"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/metrics"
)

type UpdateAlertStatusRequest struct {
	Status     domain.AlertStatus `json:"status"`
	ResolvedBy string             `json:"resolved_by"`
	// Rationale is a pointer so an explicitly blank value can be rejected,
	// while omitted legacy requests remain readable during the API migration.
	Rationale    *string    `json:"rationale,omitempty"`
	Confirm      *bool      `json:"confirm,omitempty"`
	AssignedTo   *string    `json:"assigned_to,omitempty"`
	AssignedTeam *string    `json:"assigned_team,omitempty"`
	DueAt        *time.Time `json:"due_at,omitempty"`
	// ClearDueAt distinguishes an explicit clear from an omitted due_at field.
	ClearDueAt bool `json:"clear_due_at,omitempty"`
	// ExpectedUpdatedAt enables optimistic locking (the data model §3.9,
	// WS-11 Task 8): when set, the update is rejected with 409 if the
	// alert's stored updated_at no longer matches. Omitted entirely, the
	// update proceeds unconditionally (legacy callers).
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

func alertCursor(a domain.Alert) Cursor {
	return Cursor{CreatedAt: a.CreatedAt, ID: a.ID}
}

func alertRiskCursor(a domain.Alert) Cursor {
	return Cursor{CreatedAt: a.CreatedAt, ID: a.ID, Sort: "risk", Rank: domain.AlertSeverityRank(a.Severity)}
}

func alertQueueCursor(a domain.Alert) Cursor {
	return Cursor{CreatedAt: a.UpdatedAt, ID: a.ID, Sort: "risk", Rank: domain.AlertSeverityRank(a.Severity)}
}

// recordAlertCreated increments merlon_alerts_total (OPS-003, the operational design
// §4.4) for a single newly created alert. Call this exactly once per alert,
// right after its creation is confirmed, to avoid double-counting.
func recordAlertCreated(a *domain.Alert) {
	metrics.AlertsTotal.WithLabelValues(a.ScenarioID, string(a.Severity)).Inc()
}

// consolidateAlertIntoCase joins a into an existing open case for the same
// customer within casemgmt.DefaultConsolidationWindow, or creates a new one
// (the transaction-monitoring design「アラート統合ロジック」). Call this once per
// newly created alert, after it has been persisted. A failure here (e.g.
// the case store being unavailable) is logged and does not roll back or
// fail the alert creation itself; the alert still exists and can be
// consolidated later on retry/manual review (Fail-Alert: never lose an
// alert over a case-management side effect).
func (s *Server) consolidateAlertIntoCase(ctx context.Context, a *domain.Alert) {
	if s.cases == nil {
		return
	}
	priorityResolver := casemgmt.PriorityResolver(func(ctx context.Context, customerID string) (domain.CasePriority, error) {
		customer, err := s.customers.Get(ctx, customerID)
		if err != nil {
			return "", err
		}
		return s.priorityPolicy.PriorityFor(customer), nil
	})
	var err error
	if s.caseAlertLifecycle != nil {
		_, err = casemgmt.ConsolidateAlertWithLifecycleAndPriority(ctx, s.cases, s.caseAlertLifecycle, a, casemgmt.DefaultConsolidationWindow, priorityResolver)
	} else {
		_, err = casemgmt.ConsolidateAlertWithLifecycleAndPriority(ctx, s.cases, nil, a, casemgmt.DefaultConsolidationWindow, priorityResolver)
	}
	if err != nil {
		slog.Error("failed to consolidate alert into case",
			"alert_id", a.ID, "customer_id", a.CustomerID, "error", err)
	}
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	if hasAlertQueueFilter(r) {
		filter, parseErr := parseAlertQueueFilter(r)
		if parseErr != nil {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, parseErr.Error())
			return
		}
		if useCursorPagination(r) {
			filterFingerprint := queueFilterFingerprint(r)
			cursorRepo, ok := s.alerts.(domain.AlertQueueCursorRepository)
			if !ok {
				writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "alert queue cursor pagination is not configured")
				return
			}
			pageReq, err := ParsePageRequest(r)
			if err != nil {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
				return
			}
			if pageReq.Cursor != nil && pageReq.Cursor.Sort != "risk" {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "cursor sort does not match alert queue ordering")
				return
			}
			if err := bindQueueCursorFilter(pageReq.Cursor, filterFingerprint); err != nil {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
				return
			}
			alerts, err := cursorRepo.ListQueueCursor(r.Context(), filter, pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
			if err != nil {
				writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
				return
			}
			page, meta := BuildPaginationMeta(alerts, pageReq.Limit, alertQueueCursor)
			meta = addQueueCursorFilter(meta, filterFingerprint)
			writePaginatedJSON(w, http.StatusOK, page, meta)
			return
		}
		queueRepo, ok := s.alerts.(domain.AlertQueueRepository)
		if !ok {
			writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "alert queue filters are not configured")
			return
		}
		limit, offset := parseOffsetLimit(r, 20)
		alerts, err := queueRepo.ListQueue(r.Context(), filter, limit+1, offset)
		if err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		page, meta := BuildPaginationMeta(alerts, limit, alertQueueCursor)
		meta = addQueueCursorFilter(meta, queueFilterFingerprint(r))
		if r.URL.Query().Get("offset") != "" {
			setOffsetDeprecationHeaders(w)
		}
		writePaginatedJSON(w, http.StatusOK, page, meta)
		return
	}
	customerID := r.URL.Query().Get("customer_id")

	if useCursorPagination(r) {
		s.handleListAlertsCursor(w, r, customerID)
		return
	}
	s.handleListAlertsOffset(w, r, customerID)
}

// handleListAlertsCursor serves the HTTP API contract §1.1 cursor-based pagination.
func (s *Server) handleListAlertsCursor(w http.ResponseWriter, r *http.Request, customerID string) {
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	fetchLimit := pageReq.Limit + 1
	after := toDomainCursor(pageReq.Cursor)
	riskSorted := r.URL.Query().Get("sort") == "risk"
	if pageReq.Cursor != nil && ((riskSorted && pageReq.Cursor.Sort != "risk") || (!riskSorted && pageReq.Cursor.Sort == "risk")) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "cursor sort does not match request sort")
		return
	}

	var alerts []domain.Alert
	if customerID != "" {
		if riskRepo, ok := s.alerts.(domain.AlertRiskSortRepository); riskSorted && ok {
			alerts, err = riskRepo.ListByCustomerRiskCursor(r.Context(), customerID, fetchLimit, after)
		} else {
			alerts, err = s.alerts.ListByCustomerCursor(r.Context(), customerID, fetchLimit, after)
		}
	} else {
		if riskRepo, ok := s.alerts.(domain.AlertRiskSortRepository); riskSorted && ok {
			alerts, err = riskRepo.ListOpenByRiskCursor(r.Context(), fetchLimit, after)
		} else {
			alerts, err = s.alerts.ListOpenByCursor(r.Context(), fetchLimit, after)
		}
	}
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	cursorOf := alertCursor
	if riskSorted {
		cursorOf = alertRiskCursor
	}
	page, meta := BuildPaginationMeta(alerts, pageReq.Limit, cursorOf)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

// handleListAlertsOffset preserves the pre-existing offset/limit contract
// (the HTTP API contract §1.2 dual-support / deprecation period) while still returning the
// additive {"data", "pagination"} envelope.
func (s *Server) handleListAlertsOffset(w http.ResponseWriter, r *http.Request, customerID string) {
	offsetParam := r.URL.Query().Get("offset")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(offsetParam)
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	var (
		alerts []domain.Alert
		err    error
	)
	riskSorted := r.URL.Query().Get("sort") == "risk"

	if customerID != "" {
		if riskRepo, ok := s.alerts.(domain.AlertRiskSortRepository); riskSorted && ok {
			alerts, err = riskRepo.ListByCustomerRisk(r.Context(), customerID, limit+1, offset)
		} else {
			alerts, err = s.alerts.ListByCustomer(r.Context(), customerID, limit+1, offset)
		}
	} else {
		if riskRepo, ok := s.alerts.(domain.AlertRiskSortRepository); riskSorted && ok {
			alerts, err = riskRepo.ListOpenByRisk(r.Context(), limit+1, offset)
		} else {
			alerts, err = s.alerts.ListOpen(r.Context(), limit+1, offset)
		}
	}

	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	if offsetParam != "" {
		setOffsetDeprecationHeaders(w)
	}

	cursorOf := alertCursor
	if riskSorted {
		cursorOf = alertRiskCursor
	}
	page, meta := BuildPaginationMeta(alerts, limit, cursorOf)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

func (s *Server) handleGetAlert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.alerts.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleUpdateAlertStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req UpdateAlertStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if req.AssignedTo != nil {
		if err := s.validateKnownOperator(r, *req.AssignedTo); err != nil {
			var validation *operatorAssignmentValidationError
			if errors.As(err, &validation) {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, validation.Error())
				return
			}
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
	}
	if req.AssignedTeam != nil {
		if err := s.validateKnownTeam(r, *req.AssignedTeam); err != nil {
			var validation *operatorAssignmentValidationError
			if errors.As(err, &validation) {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, validation.Error())
				return
			}
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
	}

	if req.Status == "" && req.AssignedTo == nil && req.AssignedTeam == nil && req.DueAt == nil && !req.ClearDueAt {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "status required")
		return
	}

	current, err := s.alerts.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if req.Status != "" && !domain.ValidAlertStatusTransition(current.Status, req.Status) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeInvalidStateTransition, "invalid alert status transition")
		return
	}
	if req.Confirm != nil && !*req.Confirm {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "confirmation is required for a status decision")
		return
	}
	if domain.IsAlertTerminal(req.Status) && (req.Rationale == nil || strings.TrimSpace(*req.Rationale) == "") {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "rationale is required for terminal alert status")
		return
	}
	if domain.IsAlertTerminal(req.Status) && (req.Confirm == nil || !*req.Confirm) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "confirmation is required for terminal alert status")
		return
	}
	if domain.IsAlertTerminal(current.Status) && req.Status == domain.AlertStatusInvestigating {
		if req.Rationale == nil || strings.TrimSpace(*req.Rationale) == "" {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "rationale is required to reopen an alert")
			return
		}
		if req.Confirm == nil || !*req.Confirm {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "confirmation is required to reopen an alert")
			return
		}
	}
	if domain.IsAlertTerminal(req.Status) && strings.TrimSpace(req.ResolvedBy) == "" {
		// Keep the legacy request field for compatibility, but never make the
		// UI invent an actor. A blank value is completed from the authenticated
		// request principal (or the explicit anonymous development principal).
		req.ResolvedBy = resolveAuditUserID(r)
	}

	if s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, errAtomicMutationUnavailable.Error())
		return
	}
	var a *domain.Alert
	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Alerts == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		currentInTx, err := repos.Alerts.Get(r.Context(), id)
		if err != nil {
			return err
		}
		if req.Status != "" {
			if req.Rationale != nil && strings.TrimSpace(*req.Rationale) != "" {
				dispositionRepo, ok := repos.Alerts.(domain.AlertDispositionRepository)
				if !ok {
					return errAtomicMutationUnavailable
				}
				if err := dispositionRepo.UpdateStatusWithRationale(r.Context(), id, req.Status, req.ResolvedBy, strings.TrimSpace(*req.Rationale), req.ExpectedUpdatedAt); err != nil {
					return err
				}
			} else if req.ExpectedUpdatedAt != nil {
				if err := repos.Alerts.UpdateStatusIfUnmodified(r.Context(), id, req.Status, req.ResolvedBy, *req.ExpectedUpdatedAt); err != nil {
					return err
				}
			} else if err := repos.Alerts.UpdateStatus(r.Context(), id, req.Status, req.ResolvedBy); err != nil {
				return err
			}
		}
		currentAfterStatus, err := repos.Alerts.Get(r.Context(), id)
		if err != nil {
			return err
		}
		if req.AssignedTo != nil || req.AssignedTeam != nil || req.DueAt != nil || req.ClearDueAt {
			queueRepo, ok := repos.Alerts.(domain.AlertQueueMutationRepository)
			if !ok {
				return errAtomicMutationUnavailable
			}
			assignedTo, assignedTeam := req.AssignedTo, req.AssignedTeam
			if assignedTo == nil {
				value := currentAfterStatus.AssignedTo
				assignedTo = &value
			}
			if assignedTeam == nil {
				value := currentAfterStatus.AssignedTeam
				assignedTeam = &value
			}
			dueAt := req.DueAt
			if dueAt == nil && !req.ClearDueAt {
				dueAt = currentAfterStatus.DueAt
			}
			expectedQueueUpdatedAt := req.ExpectedUpdatedAt
			if req.Status != "" {
				expectedQueueUpdatedAt = &currentAfterStatus.UpdatedAt
			}
			if err := queueRepo.UpdateQueue(r.Context(), id, assignedTo, assignedTeam, dueAt, expectedQueueUpdatedAt); err != nil {
				return err
			}
		}
		a, err = repos.Alerts.Get(r.Context(), id)
		if err != nil {
			return err
		}
		if req.Status != "" {
			rationale := ""
			if req.Rationale != nil {
				rationale = strings.TrimSpace(*req.Rationale)
			}
			if rationale == "" && domain.IsAlertTerminal(req.Status) {
				rationale = strings.TrimSpace(req.ResolvedBy)
			}
			if err := appendRequiredAlertDecision(r.Context(), r, repos, currentInTx, a, rationale); err != nil {
				return err
			}
		}
		if req.AssignedTo != nil || req.AssignedTeam != nil || req.DueAt != nil || req.ClearDueAt {
			beforeJSON, _ := json.Marshal(map[string]any{"assigned_to": currentInTx.AssignedTo, "assigned_team": currentInTx.AssignedTeam, "due_at": currentInTx.DueAt})
			afterJSON, _ := json.Marshal(map[string]any{"assigned_to": a.AssignedTo, "assigned_team": a.AssignedTeam, "due_at": a.DueAt})
			setAuditDetail(r, "assignment_before", string(beforeJSON))
			setAuditDetail(r, "assignment_after", string(afterJSON))
		}
		if err := repos.Audit.Create(r.Context(), &domain.AuditEntry{
			UserID: resolveAuditUserID(r), Action: "alert_update", ResourceType: "alerts", ResourceID: id,
			Details:   map[string]string{"correlation_id": correlationID(r), "status": string(a.Status)},
			IPAddress: extractIP(r), UserAgent: r.UserAgent(), CreatedAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		markAtomicAuditHandled(r)
		return nil
	}); err != nil {
		writeAlertStatusError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func writeAlertStatusError(w http.ResponseWriter, err error) {
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
	var invalid *domain.ErrInvalidStateTransition
	if errors.As(err, &invalid) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeInvalidStateTransition, invalid.Error())
		return
	}
	writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
}
