package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

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

func (s *Server) handleListCases(w http.ResponseWriter, r *http.Request) {
	if s.cases == nil {
		writeError(w, http.StatusServiceUnavailable, "case management not configured")
		return
	}

	customerID := r.URL.Query().Get("customer_id")

	var cases []domain.Case
	var err error

	if customerID != "" {
		cases, err = s.cases.ListByCustomer(r.Context(), customerID)
	} else {
		cases, err = s.cases.ListOpen(r.Context(), 50, 0)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cases == nil {
		cases = []domain.Case{}
	}

	writeJSON(w, http.StatusOK, cases)
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
