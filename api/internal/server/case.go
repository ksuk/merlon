package server

import (
	"encoding/json"
	"errors"
	"github.com/ksuk/merlon/api/internal/apierr"
	"net/http"
	"strconv"
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
	CustomerID string              `json:"customer_id"`
	AlertIDs   []string            `json:"alert_ids"`
	Priority   domain.CasePriority `json:"priority"`
	Summary    string              `json:"summary"`
	AssignedTo string              `json:"assigned_to"`
}

type updateCaseRequest struct {
	Status     domain.CaseStatus `json:"status,omitempty"`
	AssignedTo string            `json:"assigned_to,omitempty"`
	Summary    string            `json:"summary,omitempty"`
	// Reason is required when Status is CaseStatusReopened (the case-management workflow
	// "再オープン時は理由（テキスト、必須）を記録する").
	Reason string `json:"reason,omitempty"`
	// ExpectedUpdatedAt enables optimistic locking (the data model §3.9,
	// WS-11 Task 8): when set, the update is rejected with 409 if the
	// case's stored updated_at no longer matches. Omitted entirely, the
	// update proceeds unconditionally (legacy callers, internal batch
	// jobs).
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

type addNoteRequest struct {
	Author  string `json:"author"`
	Content string `json:"content"`
}

func (s *Server) handleCreateCase(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case management not configured")
		return
	}

	var req createCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	if req.CustomerID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer_id is required")
		return
	}
	if req.Summary == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "summary is required")
		return
	}

	_, err := s.customers.Get(r.Context(), req.CustomerID)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, "customer not found")
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	now := time.Now()
	priority := req.Priority
	if priority == "" {
		priority = domain.CasePriorityMedium
	}

	c := &domain.Case{
		ID:         generateID(),
		CustomerID: req.CustomerID,
		AlertIDs:   req.AlertIDs,
		Status:     domain.CaseStatusNew,
		Priority:   priority,
		AssignedTo: req.AssignedTo,
		Summary:    req.Summary,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.cases.Create(r.Context(), c); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
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

func (s *Server) handleListCases(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case management not configured")
		return
	}

	customerID := r.URL.Query().Get("customer_id")
	if customerID != "" {
		cases, err := s.cases.ListByCustomer(r.Context(), customerID)
		if err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		writePaginatedJSON(w, http.StatusOK, cases, PaginationMeta{HasMore: false})
		return
	}

	if r.URL.Query().Get("cursor") != "" {
		s.handleListCasesCursor(w, r)
		return
	}
	s.handleListCasesOffset(w, r)
}

// handleListCasesCursor serves the HTTP API contract §1.1 cursor-based pagination.
func (s *Server) handleListCasesCursor(w http.ResponseWriter, r *http.Request) {
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	cases, err := s.cases.ListOpenByCursor(r.Context(), pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	page, meta := BuildPaginationMeta(cases, pageReq.Limit, caseCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

// handleListCasesOffset preserves the pre-existing offset/limit contract
// (the HTTP API contract §1.2 dual-support / deprecation period) while still returning the
// additive {"data", "pagination"} envelope.
func (s *Server) handleListCasesOffset(w http.ResponseWriter, r *http.Request) {
	offsetParam := r.URL.Query().Get("offset")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(offsetParam)
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	cases, err := s.cases.ListOpen(r.Context(), limit+1, offset)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	if offsetParam != "" {
		setOffsetDeprecationHeaders(w)
	}

	page, meta := BuildPaginationMeta(cases, limit, caseCursor)
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

	oldStatus := c.Status
	if req.Status != "" {
		if !domain.ValidCaseStatusTransition(oldStatus, req.Status) {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeInvalidStateTransition, "invalid case status transition")
			return
		}

		if req.Status == domain.CaseStatusReopened {
			if req.Reason == "" {
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
			c.ReopenReason = req.Reason
		}

		c.Status = req.Status
		if req.Status == domain.CaseStatusClosed {
			now := time.Now()
			c.ClosedAt = &now
		}
	}
	if req.AssignedTo != "" {
		c.AssignedTo = req.AssignedTo
	}
	if req.Summary != "" {
		c.Summary = req.Summary
	}

	if req.ExpectedUpdatedAt != nil {
		if err := s.cases.UpdateIfUnmodified(r.Context(), c, *req.ExpectedUpdatedAt); err != nil {
			var conflict *domain.ErrConflict
			if errors.As(err, &conflict) {
				writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, conflict.Error())
				return
			}
			var nf *domain.ErrNotFound
			if errors.As(err, &nf) {
				writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
				return
			}
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
	} else if err := s.cases.Update(r.Context(), c); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if req.Status != "" {
		adjustCasesOpenGauge(oldStatus, c.Status)
	}

	event := domain.WebhookEventCaseUpdated
	if c.Status == domain.CaseStatusClosed {
		event = domain.WebhookEventCaseClosed
	}
	s.dispatchWebhook(r.Context(), event, c)

	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleAddCaseNote(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil {
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
		CreatedAt: time.Now(),
	}

	if err := s.cases.AddNote(r.Context(), caseID, note); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, note)
}

// relatedCase pairs a case with how it was linked to the case under
// inspection, so the UI can distinguish auto-discovered history from
// manual links (the case-management workflow "関連ケースは customer_id で自動抽出し、
// 追加の手動リンクも可能とする").
type relatedCase struct {
	Case     domain.Case `json:"case"`
	LinkType string      `json:"link_type"`
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
	related := []relatedCase{}
	for _, other := range sameCustomer {
		if seen[other.ID] {
			continue
		}
		seen[other.ID] = true
		related = append(related, relatedCase{Case: other, LinkType: "auto"})
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
		related = append(related, relatedCase{Case: *other, LinkType: "manual"})
	}

	writeJSON(w, http.StatusOK, related)
}

type addRelatedCaseRequest struct {
	RelatedCaseID string `json:"related_case_id"`
}

// handleAddRelatedCase serves POST /api/v1/cases/{id}/related: record a
// manual link from the case under inspection to related_case_id
// (the case-management workflow "追加の手動リンクも可能とする"). The link is
// one-directional (only the target case's related_case_ids is updated).
func (s *Server) handleAddRelatedCase(w http.ResponseWriter, r *http.Request) {
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
	if req.RelatedCaseID == id {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "a case cannot be linked to itself")
		return
	}

	if _, err := s.cases.Get(r.Context(), req.RelatedCaseID); err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, "related case not found")
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	for _, existing := range c.RelatedCaseIDs {
		if existing == req.RelatedCaseID {
			writeJSON(w, http.StatusOK, c)
			return
		}
	}
	c.RelatedCaseIDs = append(c.RelatedCaseIDs, req.RelatedCaseID)

	if err := s.cases.Update(r.Context(), c); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, c)
}
