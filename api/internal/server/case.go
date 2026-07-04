package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/metrics"
)

// openCaseStatuses are the sub-statuses merlon_cases_open (OPS-003,
// overview.md §4.4) tracks. "closed" is deliberately absent: a closed case
// is not an open case, so it is only ever decremented from, never counted.
var openCaseStatuses = map[domain.CaseStatus]bool{
	domain.CaseStatusOpen:          true,
	domain.CaseStatusInvestigating: true,
	domain.CaseStatusEscalated:     true,
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
}

type addNoteRequest struct {
	Author  string `json:"author"`
	Content string `json:"content"`
}

func (s *Server) handleCreateCase(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil {
		writeError(w, http.StatusServiceUnavailable, "case management not configured")
		return
	}

	var req createCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.CustomerID == "" {
		writeError(w, http.StatusBadRequest, "customer_id is required")
		return
	}
	if req.Summary == "" {
		writeError(w, http.StatusBadRequest, "summary is required")
		return
	}

	_, err := s.customers.Get(r.Context(), req.CustomerID)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, "customer not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
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
		Status:     domain.CaseStatusOpen,
		Priority:   priority,
		AssignedTo: req.AssignedTo,
		Summary:    req.Summary,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.cases.Create(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	adjustCasesOpenGauge("", c.Status)

	s.dispatchWebhook(r.Context(), domain.WebhookEventCaseCreated, c)

	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleGetCase(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil {
		writeError(w, http.StatusServiceUnavailable, "case management not configured")
		return
	}

	id := r.PathValue("id")
	c, err := s.cases.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, c)
}

func caseCursor(c domain.Case) Cursor {
	return Cursor{CreatedAt: c.CreatedAt, ID: c.ID}
}

func (s *Server) handleListCases(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil {
		writeError(w, http.StatusServiceUnavailable, "case management not configured")
		return
	}

	customerID := r.URL.Query().Get("customer_id")
	if customerID != "" {
		cases, err := s.cases.ListByCustomer(r.Context(), customerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
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

// handleListCasesCursor serves api.md §1.1 cursor-based pagination.
func (s *Server) handleListCasesCursor(w http.ResponseWriter, r *http.Request) {
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	cases, err := s.cases.ListOpenByCursor(r.Context(), pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	page, meta := BuildPaginationMeta(cases, pageReq.Limit, caseCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

// handleListCasesOffset preserves the pre-existing offset/limit contract
// (api.md §1.2 dual-support / deprecation period) while still returning the
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusServiceUnavailable, "case management not configured")
		return
	}

	id := r.PathValue("id")

	c, err := s.cases.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req updateCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	oldStatus := c.Status
	if req.Status != "" {
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

	if err := s.cases.Update(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusServiceUnavailable, "case management not configured")
		return
	}

	caseID := r.PathValue("id")

	_, err := s.cases.Get(r.Context(), caseID)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req addNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, note)
}
