package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ksuk/merlon/api/internal/apierr"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/metrics"
)

// openCaseStatuses are the sub-statuses merlon_cases_open (OPS-003,
// the operational design §4.4) tracks. "closed" and "str_filed" are deliberately absent:
// neither is an open case, so they are only ever decremented from, never
// counted.
var openCaseStatuses = map[domain.CaseStatus]bool{
	domain.CaseStatusOpen:          true,
	domain.CaseStatusNew:           true,
	domain.CaseStatusInvestigating: true,
	domain.CaseStatusEscalated:     true,
	domain.CaseStatusReopened:      true,
}

var errCaseAlertLifecycleUnavailable = errors.New("case/alert lifecycle repository is not configured")

func validCasePriority(priority domain.CasePriority) bool {
	switch priority {
	case domain.CasePriorityLow, domain.CasePriorityMedium, domain.CasePriorityHigh, domain.CasePriorityCritical:
		return true
	default:
		return false
	}
}

// adjustCasesOpenGauge updates merlon_cases_open for a case status
// transition. oldStatus is "" for a newly created case. Call this exactly
// once per confirmed transition to avoid double-counting.
func adjustCasesOpenGauge(oldStatus, newStatus domain.CaseStatus) {
	if oldStatus != "" && openCaseStatuses[oldStatus] {
		metrics.CasesOpen.WithLabelValues(string(oldStatus)).Dec()
	}
	if openCaseStatuses[newStatus] {
		metrics.CasesOpen.WithLabelValues(string(newStatus)).Inc()
	}
}

type createCaseRequest struct {
	CustomerID        string              `json:"customer_id"`
	AlertIDs          []string            `json:"alert_ids"`
	Priority          domain.CasePriority `json:"priority"`
	Summary           string              `json:"summary"`
	AssignedTo        string              `json:"assigned_to"`
	AssignedTeam      string              `json:"assigned_team"`
	DueAt             *time.Time          `json:"due_at"`
	STRCandidate      *bool               `json:"str_candidate,omitempty"`
	PriorityRationale string              `json:"priority_rationale,omitempty"`
}

type updateCaseRequest struct {
	Status            domain.CaseStatus    `json:"status,omitempty"`
	Priority          *domain.CasePriority `json:"priority,omitempty"`
	PriorityRationale string               `json:"priority_rationale,omitempty"`
	AssignedTo        *string              `json:"assigned_to,omitempty"`
	AssignedTeam      *string              `json:"assigned_team,omitempty"`
	DueAt             *time.Time           `json:"due_at,omitempty"`
	ClearDueAt        bool                 `json:"clear_due_at,omitempty"`
	Summary           string               `json:"summary,omitempty"`
	// Reason is required when Status is CaseStatusReopened (the case-management workflow
	// "再オープン時は理由（テキスト、必須）を記録する").
	Reason string `json:"reason,omitempty"`
	// Rationale is the durable decision evidence for closure. A nil pointer
	// preserves legacy clients during the contract window; an explicitly empty
	// value is rejected.
	Rationale                *string `json:"rationale,omitempty"`
	Confirm                  *bool   `json:"confirm,omitempty"`
	STRReportID              string  `json:"str_report_id,omitempty"`
	STRFilingChannel         string  `json:"filing_channel,omitempty"`
	STRDestination           string  `json:"destination,omitempty"`
	STRExternalReference     string  `json:"external_reference,omitempty"`
	InvestigationDisposition string  `json:"investigation_disposition,omitempty"`
	STRCandidate             *bool   `json:"str_candidate,omitempty"`
	// ExpectedUpdatedAt enables optimistic locking (the data model §3.9,
	// WS-11 Task 8): when set, the update is rejected with 409 if the
	// case's stored updated_at no longer matches. Omitted entirely, the
	// update proceeds unconditionally (legacy callers, internal batch
	// jobs).
	ExpectedUpdatedAt         *time.Time `json:"expected_updated_at,omitempty"`
	prioritySource            string
	priorityPolicyVersion     string
	priorityOverrideRationale string
}

type addNoteRequest struct {
	Author  string `json:"author"`
	Content string `json:"content"`
}

func (s *Server) handleCreateCase(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil || s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case management not configured")
		return
	}

	var req createCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	if strings.TrimSpace(req.AssignedTo) != "" {
		if err := s.validateKnownOperator(r, req.AssignedTo); err != nil {
			var validation *operatorAssignmentValidationError
			if errors.As(err, &validation) {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, validation.Error())
				return
			}
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
	}
	if strings.TrimSpace(req.AssignedTeam) != "" {
		if err := s.validateKnownTeam(r, req.AssignedTeam); err != nil {
			var validation *operatorAssignmentValidationError
			if errors.As(err, &validation) {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, validation.Error())
				return
			}
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
	}

	if req.CustomerID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer_id is required")
		return
	}
	if strings.TrimSpace(req.Summary) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "summary is required")
		return
	}

	customer, err := s.customers.Get(r.Context(), req.CustomerID)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, "customer not found")
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	derivedPriority := s.priorityPolicy.PriorityFor(customer)
	priority := req.Priority
	prioritySource := "cdd"
	if priority == "" {
		priority = derivedPriority
	} else if priority != derivedPriority {
		prioritySource = "manual_override"
		if strings.TrimSpace(req.PriorityRationale) == "" {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "priority_rationale is required when priority overrides CDD-derived priority")
			return
		}
		if role, ok := auth.RoleFromContext(r.Context()); ok && role == domain.RoleViewer {
			writeErrorCode(w, http.StatusForbidden, apierr.CodeForbidden, "manual priority override requires analyst role or above")
			return
		}
	}
	if !validCasePriority(priority) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "priority must be low, medium, high, or critical")
		return
	}

	c := &domain.Case{
		ID:           generateID(),
		CustomerID:   req.CustomerID,
		AlertIDs:     req.AlertIDs,
		Status:       domain.CaseStatusNew,
		Priority:     priority,
		AssignedTo:   req.AssignedTo,
		AssignedTeam: req.AssignedTeam,
		DueAt:        req.DueAt,
		STRCandidate: req.STRCandidate != nil && *req.STRCandidate,
		Summary:      strings.TrimSpace(req.Summary),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if len(c.AlertIDs) > 0 {
		seen := make(map[string]struct{}, len(c.AlertIDs))
		for i, alertID := range c.AlertIDs {
			canonicalAlertID := domain.CanonicalUUID(alertID)
			c.AlertIDs[i] = canonicalAlertID
			if _, duplicate := seen[canonicalAlertID]; duplicate {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "alert_ids must not contain duplicates")
				return
			}
			seen[canonicalAlertID] = struct{}{}
		}
	}
	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Cases == nil || repos.Investigation == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if len(c.AlertIDs) > 0 {
			if repos.CaseAlertLifecycle == nil {
				return errCaseAlertLifecycleUnavailable
			}
			if err := repos.CaseAlertLifecycle.CreateCaseWithAlerts(r.Context(), c); err != nil {
				return err
			}
		} else if err := repos.Cases.Create(r.Context(), c); err != nil {
			return err
		}
		after := caseEventState(*c)
		after["priority_source"] = prioritySource
		after["priority_policy_version"] = s.priorityPolicy.Version()
		if prioritySource == "manual_override" {
			after["priority_override_rationale"] = strings.TrimSpace(req.PriorityRationale)
		}
		return appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{
			CaseID: c.ID, EventType: "created", Reason: strings.TrimSpace(req.PriorityRationale), After: after, RelatedAlertIDs: append([]string(nil), c.AlertIDs...),
		})
	}); err != nil {
		writeCaseLinkError(w, err)
		return
	}
	adjustCasesOpenGauge("", c.Status)

	s.dispatchWebhook(r.Context(), domain.WebhookEventCaseCreated, c)

	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleGetCase(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case management not configured")
		return
	}

	id := r.PathValue("id")
	c, err := s.cases.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, c)
}

func caseCursor(c domain.Case) Cursor {
	return Cursor{CreatedAt: c.CreatedAt, ID: c.ID}
}

func caseRiskCursor(c domain.Case) Cursor {
	return Cursor{CreatedAt: c.CreatedAt, ID: c.ID, Sort: "risk", Rank: domain.CasePriorityRank(c.Priority)}
}

func caseQueueCursor(c domain.Case) Cursor {
	return Cursor{CreatedAt: c.UpdatedAt, ID: c.ID, Sort: "risk", Rank: domain.CasePriorityRank(c.Priority)}
}

func (s *Server) handleListCases(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case management not configured")
		return
	}
	if hasCaseQueueFilter(r) {
		filter, parseErr := parseCaseQueueFilter(r)
		if parseErr != nil {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, parseErr.Error())
			return
		}
		if useCursorPagination(r) {
			filterFingerprint := queueFilterFingerprint(r)
			cursorRepo, ok := s.cases.(domain.CaseQueueCursorRepository)
			if !ok {
				writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case queue cursor pagination is not configured")
				return
			}
			pageReq, err := ParsePageRequest(r)
			if err != nil {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
				return
			}
			if pageReq.Cursor != nil && pageReq.Cursor.Sort != "risk" {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "cursor sort does not match case queue ordering")
				return
			}
			if err := bindQueueCursorFilter(pageReq.Cursor, filterFingerprint); err != nil {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
				return
			}
			cases, err := cursorRepo.ListQueueCursor(r.Context(), filter, pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
			if err != nil {
				writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
				return
			}
			page, meta := BuildPaginationMeta(cases, pageReq.Limit, caseQueueCursor)
			meta = addQueueCursorFilter(meta, filterFingerprint)
			writePaginatedJSON(w, http.StatusOK, page, meta)
			return
		}
		queueRepo, ok := s.cases.(domain.CaseQueueRepository)
		if !ok {
			writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case queue filters are not configured")
			return
		}
		limit, offset := parseOffsetLimit(r, 50)
		cases, err := queueRepo.ListQueue(r.Context(), filter, limit+1, offset)
		if err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		page, meta := BuildPaginationMeta(cases, limit, caseQueueCursor)
		meta = addQueueCursorFilter(meta, queueFilterFingerprint(r))
		if r.URL.Query().Get("offset") != "" {
			setOffsetDeprecationHeaders(w)
		}
		writePaginatedJSON(w, http.StatusOK, page, meta)
		return
	}

	customerID := r.URL.Query().Get("customer_id")
	if customerID != "" {
		if useCursorPagination(r) {
			s.handleListCasesCursor(w, r, customerID)
		} else {
			s.handleListCasesOffset(w, r, customerID)
		}
		return
	}

	if useCursorPagination(r) {
		s.handleListCasesCursor(w, r)
		return
	}
	s.handleListCasesOffset(w, r)
}

// handleListCasesCursor serves the HTTP API contract §1.1 cursor-based pagination.
func (s *Server) handleListCasesCursor(w http.ResponseWriter, r *http.Request, customerIDs ...string) {
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	var cases []domain.Case
	riskSorted := r.URL.Query().Get("sort") == "risk"
	if pageReq.Cursor != nil && ((riskSorted && pageReq.Cursor.Sort != "risk") || (!riskSorted && pageReq.Cursor.Sort == "risk")) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "cursor sort does not match request sort")
		return
	}
	if len(customerIDs) > 0 && customerIDs[0] != "" {
		if riskRepo, ok := s.cases.(domain.CaseRiskSortRepository); riskSorted && ok {
			cases, err = riskRepo.ListByCustomerRiskCursor(r.Context(), customerIDs[0], pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
		} else {
			cases, err = s.cases.ListByCustomerCursor(r.Context(), customerIDs[0], pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
		}
	} else {
		if riskRepo, ok := s.cases.(domain.CaseRiskSortRepository); riskSorted && ok {
			cases, err = riskRepo.ListOpenByRiskCursor(r.Context(), pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
		} else {
			cases, err = s.cases.ListOpenByCursor(r.Context(), pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
		}
	}
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	cursorOf := caseCursor
	if riskSorted {
		cursorOf = caseRiskCursor
	}
	page, meta := BuildPaginationMeta(cases, pageReq.Limit, cursorOf)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

// handleListCasesOffset preserves the pre-existing offset/limit contract
// (the HTTP API contract §1.2 dual-support / deprecation period) while still returning the
// additive {"data", "pagination"} envelope.
func (s *Server) handleListCasesOffset(w http.ResponseWriter, r *http.Request, customerIDs ...string) {
	offsetParam := r.URL.Query().Get("offset")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(offsetParam)
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var cases []domain.Case
	var err error
	riskSorted := r.URL.Query().Get("sort") == "risk"
	if len(customerIDs) > 0 && customerIDs[0] != "" {
		if riskRepo, ok := s.cases.(domain.CaseRiskSortRepository); riskSorted && ok {
			cases, err = riskRepo.ListByCustomerRiskOffset(r.Context(), customerIDs[0], limit+1, offset)
		} else {
			cases, err = s.cases.ListByCustomerOffset(r.Context(), customerIDs[0], limit+1, offset)
		}
	} else {
		if riskRepo, ok := s.cases.(domain.CaseRiskSortRepository); riskSorted && ok {
			cases, err = riskRepo.ListOpenByRisk(r.Context(), limit+1, offset)
		} else {
			cases, err = s.cases.ListOpen(r.Context(), limit+1, offset)
		}
	}
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	if offsetParam != "" {
		setOffsetDeprecationHeaders(w)
	}

	cursorOf := caseCursor
	if riskSorted {
		cursorOf = caseRiskCursor
	}
	page, meta := BuildPaginationMeta(cases, limit, cursorOf)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

func (s *Server) handleUpdateCase(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case management not configured")
		return
	}

	id := r.PathValue("id")

	c, err := s.cases.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	var req updateCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
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

	oldStatus := c.Status
	beforeState := caseEventState(*c)
	expectedCaseUpdatedAt := c.UpdatedAt
	var linkedAlertTransitions []domain.AlertStatusTransition
	if req.Status != "" {
		if !domain.ValidCaseStatusTransition(oldStatus, req.Status) {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeInvalidStateTransition, "invalid case status transition")
			return
		}
		if req.Confirm != nil && !*req.Confirm {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "confirmation is required for a status decision")
			return
		}
		if domain.IsCaseTerminal(req.Status) && (req.Rationale == nil || strings.TrimSpace(*req.Rationale) == "") {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "rationale is required for a terminal case disposition")
			return
		}
		if domain.IsCaseTerminal(req.Status) && (req.Confirm == nil || !*req.Confirm) {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "confirmation is required for a terminal case disposition")
			return
		}
		if req.Status == domain.CaseStatusStrFiled {
			if err := s.validateCaseSTRFiling(r.Context(), c, req); err != nil {
				writeCaseUpdateError(w, err)
				return
			}
		}

		var err error
		linkedAlertTransitions, err = s.prepareCaseAlertTransitions(r.Context(), c, req.Status)
		if err != nil {
			var notFound *domain.ErrNotFound
			if errors.As(err, &notFound) {
				writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
				return
			}
			var conflict *domain.ErrConflict
			if errors.As(err, &conflict) {
				writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, conflict.Error())
				return
			}
			if errors.Is(err, errCaseAlertLifecycleUnavailable) {
				writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, err.Error())
				return
			}
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}

		if req.Status == domain.CaseStatusReopened {
			if strings.TrimSpace(req.Reason) == "" {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "reason is required to reopen a case")
				return
			}
			// Reopen requires Analyst or above (the case-management workflow "再オープン
			// 権限は Analyst 以上"). WS-1's auth package is available, but this
			// endpoint serves every status transition, not just reopen, so we
			// gate inline rather than via auth.RequirePermission at the route
			// level; a missing role (auth not configured, e.g. dev mode) is
			// treated as unrestricted.
			if role, ok := auth.RoleFromContext(r.Context()); ok && role == domain.RoleViewer {
				writeErrorCode(w, http.StatusForbidden, apierr.CodeForbidden, "reopen requires analyst role or above")
				return
			}
			c.ReopenReason = strings.TrimSpace(req.Reason)
		}
		if req.Status == domain.CaseStatusClosed {
			c.DispositionRationale = caseRationale(req)
			c.InvestigationDisposition = "closed"
		}
		if req.Status == domain.CaseStatusStrFiled {
			for i := range linkedAlertTransitions {
				if linkedAlertTransitions[i].From != linkedAlertTransitions[i].To {
					linkedAlertTransitions[i].Rationale = "STR report " + strings.TrimSpace(req.STRReportID) + " filed"
				}
			}
			c.STRReportID = domain.CanonicalIdentifier(req.STRReportID)
			now := time.Now().UTC().Truncate(time.Microsecond)
			c.STRFiledAt = &now
			c.STRFiledBy = resolveAuditUserID(r)
			c.STRFilingChannel = strings.TrimSpace(req.STRFilingChannel)
			c.STRDestination = strings.TrimSpace(req.STRDestination)
			c.STRExternalReference = strings.TrimSpace(req.STRExternalReference)
			c.InvestigationDisposition = "str_filed"
			c.STRCandidate = false
			c.DispositionRationale = caseRationale(req)
		}

		c.Status = req.Status
		if domain.IsCaseTerminal(req.Status) {
			now := time.Now().UTC().Truncate(time.Microsecond)
			c.ClosedAt = &now
		} else {
			c.ClosedAt = nil
		}
	}
	if req.AssignedTo != nil {
		c.AssignedTo = strings.TrimSpace(*req.AssignedTo)
	}
	if req.Priority != nil {
		if !validCasePriority(*req.Priority) {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "priority must be low, medium, high, or critical")
			return
		}
		customer, err := s.customers.Get(r.Context(), c.CustomerID)
		if err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		derivedPriority := s.priorityPolicy.PriorityFor(customer)
		reason := strings.TrimSpace(req.PriorityRationale)
		if reason == "" && req.Rationale != nil {
			reason = strings.TrimSpace(*req.Rationale)
		}
		if reason == "" {
			reason = strings.TrimSpace(req.Reason)
		}
		if *req.Priority != derivedPriority && strings.TrimSpace(reason) == "" {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "priority_rationale is required when priority overrides CDD-derived priority")
			return
		}
		if *req.Priority != derivedPriority {
			if role, ok := auth.RoleFromContext(r.Context()); ok && role == domain.RoleViewer {
				writeErrorCode(w, http.StatusForbidden, apierr.CodeForbidden, "manual priority override requires analyst role or above")
				return
			}
			req.prioritySource = "manual_override"
			req.priorityOverrideRationale = reason
		} else {
			req.prioritySource = "cdd"
		}
		req.priorityPolicyVersion = s.priorityPolicy.Version()
		c.Priority = *req.Priority
	}
	if req.AssignedTeam != nil {
		c.AssignedTeam = strings.TrimSpace(*req.AssignedTeam)
	}
	if req.ClearDueAt {
		c.DueAt = nil
	} else if req.DueAt != nil {
		c.DueAt = req.DueAt
	}
	if req.Summary != "" {
		if strings.TrimSpace(req.Summary) == "" {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "summary must not be blank")
			return
		}
		c.Summary = strings.TrimSpace(req.Summary)
	}
	if req.InvestigationDisposition != "" {
		c.InvestigationDisposition = strings.TrimSpace(req.InvestigationDisposition)
	}
	if req.STRCandidate != nil {
		c.STRCandidate = *req.STRCandidate
	}
	if req.InvestigationDisposition != "" || req.STRCandidate != nil {
		c.DispositionRationale = caseRationale(req)
	}

	if s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, errAtomicMutationUnavailable.Error())
		return
	}
	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Cases == nil || repos.Investigation == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if req.Status == domain.CaseStatusStrFiled {
			if err := validateCaseSTRFilingWithReportRepo(r.Context(), repos.Reports, c, req); err != nil {
				return err
			}
		}
		if req.Status != "" && len(c.AlertIDs) > 0 {
			if repos.CaseAlertLifecycle == nil {
				return errCaseAlertLifecycleUnavailable
			}
			if err := repos.CaseAlertLifecycle.UpdateCaseAndAlerts(r.Context(), c, caseExpectedUpdatedAt(req.ExpectedUpdatedAt, expectedCaseUpdatedAt), linkedAlertTransitions); err != nil {
				return err
			}
		} else if req.ExpectedUpdatedAt != nil {
			if err := repos.Cases.UpdateIfUnmodified(r.Context(), c, *req.ExpectedUpdatedAt); err != nil {
				return err
			}
		} else if err := repos.Cases.Update(r.Context(), c); err != nil {
			return err
		}
		if err := appendRequiredCaseChangeEvents(r.Context(), r, repos, beforeState, c, req); err != nil {
			return err
		}
		for _, transition := range linkedAlertTransitions {
			if transition.From == transition.To || repos.Alerts == nil {
				continue
			}
			afterAlert, err := repos.Alerts.Get(r.Context(), transition.ID)
			if err != nil {
				return err
			}
			beforeAlert := &domain.Alert{ID: transition.ID, Status: transition.From, CustomerID: c.CustomerID}
			rationale := transition.Rationale
			if strings.TrimSpace(rationale) == "" {
				rationale = caseRationale(req)
			}
			if err := appendRequiredAlertDecision(r.Context(), r, repos, beforeAlert, afterAlert, rationale); err != nil {
				return err
			}
			if err := appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{
				CaseID: c.ID, EventType: "alert_decision", Reason: rationale,
				Before: alertEventState(beforeAlert), After: alertEventState(afterAlert),
				RelatedAlertIDs: []string{afterAlert.ID}, RelatedReportIDs: nonEmptyIDs(c.STRReportID),
			}); err != nil {
				return err
			}
		}
		if err := repos.Audit.Create(r.Context(), &domain.AuditEntry{
			UserID: resolveAuditUserID(r), Action: "case_update", ResourceType: "cases", ResourceID: c.ID,
			Details:   map[string]string{"correlation_id": correlationID(r), "status": string(c.Status)},
			IPAddress: extractIP(r), UserAgent: r.UserAgent(), CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		}); err != nil {
			return err
		}
		markAtomicAuditHandled(r)
		return nil
	}); err != nil {
		writeCaseUpdateError(w, err)
		return
	}
	if req.Status != "" {
		adjustCasesOpenGauge(oldStatus, c.Status)
	}
	if req.Status == domain.CaseStatusStrFiled {
		setAuditDetail(r, "str_report_id", c.STRReportID)
	}

	event := domain.WebhookEventCaseUpdated
	if c.Status == domain.CaseStatusClosed {
		event = domain.WebhookEventCaseClosed
	}
	s.dispatchWebhook(r.Context(), event, c)

	writeJSON(w, http.StatusOK, c)
}

func caseExpectedUpdatedAt(expected *time.Time, observed time.Time) time.Time {
	if expected != nil {
		return *expected
	}
	return observed
}

// prepareCaseAlertTransitions checks the compatibility between a target case
// status and all linked alerts. A case may not become terminal while any
// linked alert is still unresolved. Moving a case into investigation or
// escalation may advance linked active alerts, and that advancement is later
// committed with the case in one transaction.
func (s *Server) prepareCaseAlertTransitions(ctx context.Context, c *domain.Case, target domain.CaseStatus) ([]domain.AlertStatusTransition, error) {
	if len(c.AlertIDs) == 0 {
		return nil, nil
	}
	if s.alerts == nil {
		return nil, errCaseAlertLifecycleUnavailable
	}

	var transitions []domain.AlertStatusTransition
	for _, alertID := range c.AlertIDs {
		a, err := s.alerts.Get(ctx, alertID)
		if err != nil {
			return nil, err
		}

		if domain.IsCaseTerminal(target) && target != domain.CaseStatusStrFiled {
			if !domain.IsAlertTerminal(a.Status) {
				return nil, &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "cannot close case while linked alert " + a.ID + " is unresolved"}
			}
			transitions = append(transitions, domain.AlertStatusTransition{ID: a.ID, From: a.Status, To: a.Status, ExpectedUpdatedAt: a.UpdatedAt})
			continue
		}
		if domain.IsAlertTerminal(a.Status) {
			// A terminal alert is immutable evidence. It remains linked when a
			// closed case is reopened and through any later reinvestigation.
			transitions = append(transitions, domain.AlertStatusTransition{ID: a.ID, From: a.Status, To: a.Status, ExpectedUpdatedAt: a.UpdatedAt})
			continue
		}
		if !domain.IsAlertUnresolved(a.Status) {
			return nil, &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "linked alert " + a.ID + " has an unsupported state"}
		}

		switch target {
		case domain.CaseStatusStrFiled:
			// Filing is a positive disposition. Active linked alerts are closed
			// true-positive in the same case/alert transaction; terminal alerts
			// remain immutable history.
			transitions = append(transitions, domain.AlertStatusTransition{
				ID: a.ID, From: a.Status, To: domain.AlertStatusClosedTruePositive,
				ResolvedBy: "case-filing", ExpectedUpdatedAt: a.UpdatedAt,
			})
		case domain.CaseStatusInvestigating:
			if a.Status == domain.AlertStatusOpen {
				transitions = append(transitions, domain.AlertStatusTransition{
					ID: a.ID, From: a.Status, To: domain.AlertStatusInvestigating, ExpectedUpdatedAt: a.UpdatedAt,
				})
			} else {
				transitions = append(transitions, domain.AlertStatusTransition{ID: a.ID, From: a.Status, To: a.Status, ExpectedUpdatedAt: a.UpdatedAt})
			}
		case domain.CaseStatusEscalated:
			if a.Status == domain.AlertStatusOpen || a.Status == domain.AlertStatusInvestigating {
				transitions = append(transitions, domain.AlertStatusTransition{
					ID: a.ID, From: a.Status, To: domain.AlertStatusEscalated, ExpectedUpdatedAt: a.UpdatedAt,
				})
			} else {
				transitions = append(transitions, domain.AlertStatusTransition{ID: a.ID, From: a.Status, To: a.Status, ExpectedUpdatedAt: a.UpdatedAt})
			}
		default:
			if domain.IsCaseUnresolved(target) {
				transitions = append(transitions, domain.AlertStatusTransition{ID: a.ID, From: a.Status, To: a.Status, ExpectedUpdatedAt: a.UpdatedAt})
			}
		}
	}
	return transitions, nil
}

func writeCaseUpdateError(w http.ResponseWriter, err error) {
	if errors.Is(err, errAtomicMutationUnavailable) || errors.Is(err, errCaseAlertLifecycleUnavailable) {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, err.Error())
		return
	}
	var validation *caseWorkflowValidationError
	if errors.As(err, &validation) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, validation.Error())
		return
	}
	var conflict *domain.ErrConflict
	if errors.As(err, &conflict) {
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, conflict.Error())
		return
	}
	var notFound *domain.ErrNotFound
	if errors.As(err, &notFound) {
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, notFound.Error())
		return
	}
	var invalid *domain.ErrInvalidStateTransition
	if errors.As(err, &invalid) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeInvalidStateTransition, invalid.Error())
		return
	}
	writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
}

func writeCaseLinkError(w http.ResponseWriter, err error) {
	if errors.Is(err, errAtomicMutationUnavailable) {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, err.Error())
		return
	}
	if errors.Is(err, errCaseAlertLifecycleUnavailable) {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, err.Error())
		return
	}
	var conflict *domain.ErrConflict
	if errors.As(err, &conflict) {
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, conflict.Error())
		return
	}
	var notFound *domain.ErrNotFound
	if errors.As(err, &notFound) {
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, notFound.Error())
		return
	}
	writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
}

func (s *Server) handleAddCaseNote(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil || s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case management not configured")
		return
	}

	caseID := r.PathValue("id")

	_, err := s.cases.Get(r.Context(), caseID)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	var req addNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	if req.Content == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "content is required")
		return
	}

	author := resolveAuditUserID(r)
	if author == "anonymous" && req.Author != "" {
		author = req.Author
	}

	note := &domain.CaseNote{
		ID:        generateID(),
		Author:    author,
		Content:   req.Content,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}

	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Cases == nil || repos.Investigation == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if err := repos.Cases.AddNote(r.Context(), caseID, note); err != nil {
			return err
		}
		return appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{CaseID: caseID, EventType: "note_added", Reason: "case note added", After: map[string]any{"note_id": note.ID, "author": note.Author, "content": note.Content}})
	}); err != nil {
		writeCaseFileError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, note)
}

// relatedCase pairs a case with how it was linked to the case under
// inspection, so the UI can distinguish auto-discovered history from
// manual links (the case-management workflow "関連ケースは customer_id で自動抽出し、
// 追加の手動リンクも可能とする").
type relatedCase struct {
	Case         domain.Case              `json:"case"`
	LinkType     string                   `json:"link_type"`
	Relationship *domain.CaseRelationship `json:"relationship,omitempty"`
}

// handleGetRelatedCases serves GET /api/v1/cases/{id}/related: the same
// customer's other cases (auto-discovered) plus any manually linked cases
// recorded in related_case_ids.
func (s *Server) handleGetRelatedCases(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case management not configured")
		return
	}

	id := r.PathValue("id")
	c, err := s.cases.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	sameCustomer, err := s.cases.ListByCustomer(r.Context(), c.CustomerID)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	seen := map[string]bool{id: true}
	manualRelationships := map[string]domain.CaseRelationship{}
	if s.caseInvestigation != nil {
		relationships, relErr := s.caseInvestigation.ListRelationships(r.Context(), id, r.URL.Query().Get("include_inactive") == "true")
		if relErr != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, relErr.Error())
			return
		}
		for _, relationship := range relationships {
			manualRelationships[relationship.RelatedCaseID] = relationship
		}
	}
	related := []relatedCase{}
	for _, other := range sameCustomer {
		if seen[other.ID] {
			continue
		}
		seen[other.ID] = true
		relationship, manuallyLinked := manualRelationships[other.ID]
		linkType := "auto"
		if manuallyLinked {
			linkType = "manual"
		}
		related = append(related, relatedCase{Case: other, LinkType: linkType, Relationship: relationshipPointer(relationship, manuallyLinked)})
	}
	for _, relatedID := range c.RelatedCaseIDs {
		if seen[relatedID] {
			continue
		}
		seen[relatedID] = true
		other, err := s.cases.Get(r.Context(), relatedID)
		if err != nil {
			continue
		}
		relationship, hasRelationship := manualRelationships[relatedID]
		related = append(related, relatedCase{Case: *other, LinkType: "manual", Relationship: relationshipPointer(relationship, hasRelationship)})
	}

	writeJSON(w, http.StatusOK, related)
}

type addRelatedCaseRequest struct {
	RelatedCaseID     string     `json:"related_case_id"`
	RelationshipType  string     `json:"relationship_type"`
	Rationale         string     `json:"rationale"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

func relationshipPointer(value domain.CaseRelationship, ok bool) *domain.CaseRelationship {
	if !ok {
		return nil
	}
	return &value
}

// handleAddRelatedCase serves POST /api/v1/cases/{id}/related: record a
// manual link from the case under inspection to related_case_id
// (the case-management workflow "追加の手動リンクも可能とする"). The link is
// one-directional (only the target case's related_case_ids is updated).
func (s *Server) handleAddRelatedCaseLegacy(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case management not configured")
		return
	}

	id := r.PathValue("id")
	c, err := s.cases.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	var req addRelatedCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	if req.RelatedCaseID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "related_case_id is required")
		return
	}
	if domain.SameIdentifier(req.RelatedCaseID, id) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "a case cannot be linked to itself")
		return
	}

	relatedCase, err := s.cases.Get(r.Context(), req.RelatedCaseID)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, "related case not found")
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	// New case links are work links, not historical annotations: both cases
	// must belong to the same customer and remain in an active workflow state.
	// Terminal cases stay visible in the investigation file, but cannot be
	// introduced as a new relationship.
	if !domain.SameIdentifier(relatedCase.CustomerID, c.CustomerID) {
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, "related case belongs to a different customer")
		return
	}
	if !domain.IsCaseUnresolved(c.Status) || !domain.IsCaseUnresolved(relatedCase.Status) {
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, "new case links require both cases to be active")
		return
	}

	for _, existing := range c.RelatedCaseIDs {
		if domain.SameIdentifier(existing, req.RelatedCaseID) {
			writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, "related case is already linked")
			return
		}
	}
	if strings.TrimSpace(req.RelationshipType) == "" {
		req.RelationshipType = "related"
	}
	if strings.TrimSpace(req.Rationale) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "rationale is required")
		return
	}
	c.RelatedCaseIDs = append(c.RelatedCaseIDs, req.RelatedCaseID)
	expectedUpdatedAt := c.UpdatedAt
	if req.ExpectedUpdatedAt != nil {
		expectedUpdatedAt = *req.ExpectedUpdatedAt
	}

	if s.caseInvestigation != nil {
		relationship := &domain.CaseRelationship{ID: generateID(), CaseID: id, RelatedCaseID: req.RelatedCaseID, RelationshipType: strings.TrimSpace(req.RelationshipType), Rationale: strings.TrimSpace(req.Rationale), CreatedBy: resolveAuditUserID(r), CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Active: true, Source: "manual"}
		if err := s.caseInvestigation.AddRelationship(r.Context(), relationship); err != nil {
			writeCaseUpdateError(w, err)
			return
		}
		if err := s.cases.UpdateIfUnmodified(r.Context(), c, expectedUpdatedAt); err != nil {
			// Keep the append-only relationship history truthful if the legacy
			// related_case_ids projection cannot be updated.
			writeCaseUpdateError(w, err)
			return
		}
	} else if err := s.cases.UpdateIfUnmodified(r.Context(), c, expectedUpdatedAt); err != nil {
		writeCaseUpdateError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, c)
}

// handleAddRelatedCase applies the relationship row, case projection, and
// required timeline/audit append in one mutation boundary. The memory and
// PostgreSQL adapters both implement that boundary; no compensating delete
// is used when one required append fails.
func (s *Server) handleAddRelatedCase(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil || s.atomic == nil || s.caseInvestigation == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case management is not configured")
		return
	}
	id := r.PathValue("id")
	var req addRelatedCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	req.RelatedCaseID = strings.TrimSpace(req.RelatedCaseID)
	if req.RelatedCaseID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "related_case_id is required")
		return
	}
	if domain.SameIdentifier(req.RelatedCaseID, id) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "a case cannot be linked to itself")
		return
	}
	if strings.TrimSpace(req.Rationale) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "rationale is required")
		return
	}
	if strings.TrimSpace(req.RelationshipType) == "" {
		req.RelationshipType = "related"
	}

	var updated *domain.Case
	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Cases == nil || repos.Investigation == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		current, err := repos.Cases.Get(r.Context(), id)
		if err != nil {
			return err
		}
		related, err := repos.Cases.Get(r.Context(), req.RelatedCaseID)
		if err != nil {
			return err
		}
		if !domain.SameIdentifier(current.CustomerID, related.CustomerID) {
			return &domain.ErrConflict{Entity: "case_relationship", ID: req.RelatedCaseID, Reason: "related case belongs to a different customer"}
		}
		if !domain.IsCaseUnresolved(current.Status) || !domain.IsCaseUnresolved(related.Status) {
			return &domain.ErrConflict{Entity: "case_relationship", ID: req.RelatedCaseID, Reason: "new case links require both cases to be active"}
		}
		for _, existing := range current.RelatedCaseIDs {
			if domain.SameIdentifier(existing, req.RelatedCaseID) {
				return &domain.ErrConflict{Entity: "case_relationship", ID: req.RelatedCaseID, Reason: "related case is already linked"}
			}
		}
		current.RelatedCaseIDs = append(current.RelatedCaseIDs, domain.CanonicalIdentifier(req.RelatedCaseID))
		expected := current.UpdatedAt
		if req.ExpectedUpdatedAt != nil {
			expected = *req.ExpectedUpdatedAt
		}
		relationship := &domain.CaseRelationship{
			ID: generateID(), CaseID: id, RelatedCaseID: req.RelatedCaseID,
			RelationshipType: strings.TrimSpace(req.RelationshipType), Rationale: strings.TrimSpace(req.Rationale),
			CreatedBy: resolveAuditUserID(r), CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Active: true, Source: "manual",
		}
		if err := repos.Investigation.AddRelationship(r.Context(), relationship); err != nil {
			return err
		}
		if err := repos.Cases.UpdateIfUnmodified(r.Context(), current, expected); err != nil {
			return err
		}
		updated = current
		if err := appendRequiredRelationshipHistory(r.Context(), r, repos, relationship, "added", "relationship added", nil, relationshipEventState(*relationship)); err != nil {
			return err
		}
		return appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{
			CaseID: id, EventType: "related_case_added", Reason: relationship.Rationale,
			RelatedCaseIDs: []string{relationship.RelatedCaseID},
			After:          map[string]any{"relationship_id": relationship.ID, "relationship_type": relationship.RelationshipType, "source": "manual"},
		})
	}); err != nil {
		writeCaseUpdateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

type relationshipCorrectionRequest struct {
	RelationshipType string `json:"relationship_type"`
	Rationale        string `json:"rationale"`
}

func (s *Server) handleRemoveRelatedCaseLegacy(w http.ResponseWriter, r *http.Request) {
	if s.caseInvestigation == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case investigation is not configured")
		return
	}
	relationshipID := r.PathValue("relationship")
	var req struct {
		Reason            string     `json:"reason"`
		ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "reason is required")
		return
	}
	caseRecord, err := s.cases.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeCaseUpdateError(w, err)
		return
	}
	relationships, err := s.caseInvestigation.ListRelationships(r.Context(), r.PathValue("id"), true)
	if err != nil {
		writeCaseUpdateError(w, err)
		return
	}
	var relatedID string
	var currentRelationship *domain.CaseRelationship
	for _, relationship := range relationships {
		if domain.SameIdentifier(relationship.ID, relationshipID) {
			relatedID = relationship.RelatedCaseID
			copy := relationship
			currentRelationship = &copy
			break
		}
	}
	if relatedID == "" {
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, "relationship not found")
		return
	}
	beforeCase := *caseRecord
	beforeCase.RelatedCaseIDs = append([]string(nil), caseRecord.RelatedCaseIDs...)
	filtered := make([]string, 0, len(caseRecord.RelatedCaseIDs))
	for _, id := range caseRecord.RelatedCaseIDs {
		if id != relatedID {
			filtered = append(filtered, id)
		}
	}
	updatedCase := *caseRecord
	updatedCase.RelatedCaseIDs = filtered
	expectedUpdatedAt := caseRecord.UpdatedAt
	if req.ExpectedUpdatedAt != nil {
		expectedUpdatedAt = *req.ExpectedUpdatedAt
	}
	if err := s.cases.UpdateIfUnmodified(r.Context(), &updatedCase, expectedUpdatedAt); err != nil {
		writeCaseUpdateError(w, err)
		return
	}
	projectionUpdatedAt := updatedCase.UpdatedAt
	if err := s.caseInvestigation.RemoveRelationship(r.Context(), relationshipID, resolveAuditUserID(r), req.Reason); err != nil {
		// The legacy related_case_ids column is a projection of the append-only
		// relationship record. Restore it if retiring the durable relationship
		// fails, so a failed request cannot leave the two representations split.
		_ = s.cases.UpdateIfUnmodified(r.Context(), &beforeCase, projectionUpdatedAt)
		writeCaseUpdateError(w, err)
		return
	}
	removedRelationship := *currentRelationship
	removedRelationship.Active = false
	removedRelationship.RemovedBy = resolveAuditUserID(r)
	removedAt := time.Now().UTC().Truncate(time.Microsecond)
	removedRelationship.RemovedAt = &removedAt
	removedRelationship.RemovalReason = strings.TrimSpace(req.Reason)
	writeJSON(w, http.StatusOK, map[string]any{"relationship_id": relationshipID, "active": false})
}

func (s *Server) handleRemoveRelatedCase(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil || s.caseInvestigation == nil || s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case investigation is not configured")
		return
	}
	relationshipID := r.PathValue("relationship")
	var req struct {
		Reason            string     `json:"reason"`
		ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "reason is required")
		return
	}
	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Cases == nil || repos.Investigation == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		caseRecord, err := repos.Cases.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		relationships, err := repos.Investigation.ListRelationships(r.Context(), caseRecord.ID, true)
		if err != nil {
			return err
		}
		var current *domain.CaseRelationship
		for i := range relationships {
			if domain.SameIdentifier(relationships[i].ID, relationshipID) {
				copy := relationships[i]
				current = &copy
				break
			}
		}
		if current == nil {
			return &domain.ErrNotFound{Entity: "case_relationship", ID: relationshipID}
		}
		updated := *caseRecord
		updated.RelatedCaseIDs = make([]string, 0, len(caseRecord.RelatedCaseIDs))
		for _, linkedID := range caseRecord.RelatedCaseIDs {
			if !domain.SameIdentifier(linkedID, current.RelatedCaseID) {
				updated.RelatedCaseIDs = append(updated.RelatedCaseIDs, linkedID)
			}
		}
		expected := caseRecord.UpdatedAt
		if req.ExpectedUpdatedAt != nil {
			expected = *req.ExpectedUpdatedAt
		}
		if err := repos.Cases.UpdateIfUnmodified(r.Context(), &updated, expected); err != nil {
			return err
		}
		if err := repos.Investigation.RemoveRelationship(r.Context(), relationshipID, resolveAuditUserID(r), req.Reason); err != nil {
			return err
		}
		removed := *current
		removed.Active = false
		removed.RemovedBy = resolveAuditUserID(r)
		removedAt := time.Now().UTC().Truncate(time.Microsecond)
		removed.RemovedAt = &removedAt
		removed.RemovalReason = req.Reason
		if err := appendRequiredRelationshipHistory(r.Context(), r, repos, current, "removed", req.Reason, relationshipEventState(*current), relationshipEventState(removed)); err != nil {
			return err
		}
		return appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{
			CaseID: caseRecord.ID, EventType: "related_case_removed", Reason: req.Reason,
			Before: relationshipEventState(*current), After: relationshipEventState(removed),
		})
	}); err != nil {
		writeCaseUpdateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"relationship_id": relationshipID, "active": false})
}

func (s *Server) handleCorrectRelatedCaseLegacy(w http.ResponseWriter, r *http.Request) {
	if s.caseInvestigation == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case investigation is not configured")
		return
	}
	relationshipID := r.PathValue("relationship")
	var req relationshipCorrectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Rationale) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "rationale is required")
		return
	}
	relationships, err := s.caseInvestigation.ListRelationships(r.Context(), r.PathValue("id"), true)
	if err != nil {
		writeCaseUpdateError(w, err)
		return
	}
	var current *domain.CaseRelationship
	for i := range relationships {
		if domain.SameIdentifier(relationships[i].ID, relationshipID) {
			current = &relationships[i]
			break
		}
	}
	if current == nil {
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, "relationship not found")
		return
	}
	if !current.Active {
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, "relationship is already inactive")
		return
	}
	caseRecord, err := s.cases.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeCaseUpdateError(w, err)
		return
	}
	relatedCase, err := s.cases.Get(r.Context(), current.RelatedCaseID)
	if err != nil {
		writeCaseUpdateError(w, err)
		return
	}
	if !domain.SameIdentifier(caseRecord.CustomerID, relatedCase.CustomerID) {
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, "related case belongs to a different customer")
		return
	}
	if !domain.IsCaseUnresolved(caseRecord.Status) || !domain.IsCaseUnresolved(relatedCase.Status) {
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, "new case links require both cases to be active")
		return
	}
	if strings.TrimSpace(req.RelationshipType) == "" {
		req.RelationshipType = current.RelationshipType
	}
	newRelationship := &domain.CaseRelationship{ID: generateID(), CaseID: current.CaseID, RelatedCaseID: current.RelatedCaseID, RelationshipType: strings.TrimSpace(req.RelationshipType), Rationale: strings.TrimSpace(req.Rationale), CreatedBy: resolveAuditUserID(r), CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Active: true, Source: "manual"}
	if err := s.caseInvestigation.ReplaceRelationship(r.Context(), relationshipID, newRelationship, resolveAuditUserID(r), "corrected: "+strings.TrimSpace(req.Rationale)); err != nil {
		writeCaseUpdateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newRelationship)
}

func (s *Server) handleCorrectRelatedCase(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil || s.caseInvestigation == nil || s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case investigation is not configured")
		return
	}
	relationshipID := r.PathValue("relationship")
	var req relationshipCorrectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	req.Rationale = strings.TrimSpace(req.Rationale)
	if req.Rationale == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "rationale is required")
		return
	}
	var replacement *domain.CaseRelationship
	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Cases == nil || repos.Investigation == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		relationships, err := repos.Investigation.ListRelationships(r.Context(), r.PathValue("id"), true)
		if err != nil {
			return err
		}
		var current *domain.CaseRelationship
		for i := range relationships {
			if domain.SameIdentifier(relationships[i].ID, relationshipID) {
				copy := relationships[i]
				current = &copy
				break
			}
		}
		if current == nil {
			return &domain.ErrNotFound{Entity: "case_relationship", ID: relationshipID}
		}
		if !current.Active {
			return &domain.ErrConflict{Entity: "case_relationship", ID: relationshipID, Reason: "relationship is already inactive"}
		}
		caseRecord, err := repos.Cases.Get(r.Context(), current.CaseID)
		if err != nil {
			return err
		}
		relatedCase, err := repos.Cases.Get(r.Context(), current.RelatedCaseID)
		if err != nil {
			return err
		}
		if !domain.SameIdentifier(caseRecord.CustomerID, relatedCase.CustomerID) {
			return &domain.ErrConflict{Entity: "case_relationship", ID: relationshipID, Reason: "related case belongs to a different customer"}
		}
		if !domain.IsCaseUnresolved(caseRecord.Status) || !domain.IsCaseUnresolved(relatedCase.Status) {
			return &domain.ErrConflict{Entity: "case_relationship", ID: relationshipID, Reason: "new case links require both cases to be active"}
		}
		relationshipType := strings.TrimSpace(req.RelationshipType)
		if relationshipType == "" {
			relationshipType = current.RelationshipType
		}
		replacement = &domain.CaseRelationship{ID: generateID(), CaseID: current.CaseID, RelatedCaseID: current.RelatedCaseID, RelationshipType: relationshipType, Rationale: req.Rationale, CreatedBy: resolveAuditUserID(r), CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Active: true, Source: "manual"}
		if err := repos.Investigation.ReplaceRelationship(r.Context(), relationshipID, replacement, resolveAuditUserID(r), "corrected: "+req.Rationale); err != nil {
			return err
		}
		if err := appendRequiredRelationshipHistory(r.Context(), r, repos, replacement, "corrected", req.Rationale, relationshipEventState(*current), relationshipEventState(*replacement)); err != nil {
			return err
		}
		return appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{
			CaseID: current.CaseID, EventType: "related_case_corrected", Reason: req.Rationale,
			RelatedCaseIDs: []string{current.RelatedCaseID}, Before: relationshipEventState(*current), After: relationshipEventState(*replacement),
		})
	}); err != nil {
		writeCaseUpdateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, replacement)
}
