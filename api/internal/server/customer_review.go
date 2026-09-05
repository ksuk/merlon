package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/review"
)

func customerReviewCursor(item domain.CustomerReview) Cursor {
	return Cursor{CreatedAt: item.DueAt, ID: item.ID}
}

func (s *Server) handleListCustomerReviews(w http.ResponseWriter, r *http.Request) {
	if s.reviews == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "CDD review service not configured")
		return
	}
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	fingerprint := queueFilterFingerprint(r)
	if err := bindQueueCursorFilter(pageReq.Cursor, fingerprint); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	filter, err := parseCustomerReviewFilter(r, pageReq.Cursor)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	filter.Limit = pageReq.Limit + 1
	items, err := s.reviews.List(r.Context(), filter)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	page, meta := BuildPaginationMeta(items, pageReq.Limit, customerReviewCursor)
	meta = addQueueCursorFilter(meta, fingerprint)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

func parseCustomerReviewFilter(r *http.Request, cursor *Cursor) (domain.CustomerReviewFilter, error) {
	f := domain.CustomerReviewFilter{CustomerID: domain.CanonicalIdentifier(r.URL.Query().Get("customer_id")),
		AssignedTo: r.URL.Query().Get("assigned_to"), AssignedTeam: r.URL.Query().Get("team"),
		Cursor: toDomainCursor(cursor)}
	if tier := domain.RiskTier(r.URL.Query().Get("tier")); tier != "" {
		if tier != domain.RiskTierLow && tier != domain.RiskTierMedium && tier != domain.RiskTierHigh {
			return f, errors.New("unsupported review tier")
		}
		f.Tier = tier
	}
	for _, raw := range csvValues(r.URL.Query().Get("status")) {
		status := domain.CustomerReviewStatus(raw)
		if !status.Valid() {
			return f, errors.New("unsupported review status: " + raw)
		}
		f.Statuses = append(f.Statuses, status)
	}
	if due := r.URL.Query().Get("due"); due != "" {
		switch due {
		case "due":
			f.Statuses = append(f.Statuses, domain.CustomerReviewStatusDue)
		case "overdue":
			f.Statuses = append(f.Statuses, domain.CustomerReviewStatusOverdue)
		default:
			return f, errors.New("due must be due or overdue")
		}
	}
	var err error
	if raw := r.URL.Query().Get("due_before"); raw != "" {
		value, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			return f, errors.New("due_before must be RFC3339")
		}
		f.DueBefore = &value
	}
	if raw := r.URL.Query().Get("due_after"); raw != "" {
		value, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			return f, errors.New("due_after must be RFC3339")
		}
		f.DueAfter = &value
	}
	_ = err
	return f, nil
}

func (s *Server) handleGetCustomerReview(w http.ResponseWriter, r *http.Request) {
	if s.reviews == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "CDD review service not configured")
		return
	}
	item, err := s.reviews.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeCustomerReviewError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type patchCustomerReviewRequest struct {
	AssignedTo      *string                     `json:"assigned_to,omitempty"`
	AssignedTeam    *string                     `json:"assigned_team,omitempty"`
	Status          domain.CustomerReviewStatus `json:"status,omitempty"`
	Action          string                      `json:"action,omitempty"`
	ExpectedVersion int64                       `json:"expected_version"`
}

func (s *Server) handlePatchCustomerReview(w http.ResponseWriter, r *http.Request) {
	if s.reviews == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "CDD review service not configured")
		return
	}
	var req patchCustomerReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	actor := resolveAuditUserID(r)
	var item *domain.CustomerReview
	var err error
	if req.Action == "start" || req.Action == "resume" || req.Status == domain.CustomerReviewStatusInProgress {
		item, err = s.reviews.Start(r.Context(), r.PathValue("id"), actor, req.ExpectedVersion)
	} else {
		assignedTo, assignedTeam := "", ""
		if req.AssignedTo != nil {
			assignedTo = *req.AssignedTo
		}
		if req.AssignedTeam != nil {
			assignedTeam = *req.AssignedTeam
		}
		item, err = s.reviews.Assign(r.Context(), r.PathValue("id"), assignedTo, assignedTeam, actor, req.ExpectedVersion)
	}
	if err != nil {
		writeCustomerReviewError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleCompleteCustomerReview(w http.ResponseWriter, r *http.Request) {
	if s.reviews == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "CDD review service not configured")
		return
	}
	var req domain.CustomerReviewCompletion
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	role := ""
	if value, ok := auth.RoleFromContext(r.Context()); ok {
		role = string(value)
	}
	if role == "" && s.apikeys == nil {
		role = "admin"
	}
	req.Role = role
	if req.Actor == "" {
		req.Actor = resolveAuditUserID(r)
	}
	item, err := s.reviews.Complete(r.Context(), r.PathValue("id"), req)
	if err != nil {
		writeCustomerReviewError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func writeCustomerReviewError(w http.ResponseWriter, err error) {
	var notFound *domain.ErrNotFound
	var conflict *domain.ErrConflict
	var transition *domain.ErrInvalidStateTransition
	switch {
	case errors.As(err, &notFound):
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
	case errors.As(err, &conflict):
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, err.Error())
	case errors.As(err, &transition):
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, err.Error())
	case errors.Is(err, review.ErrInvalid), strings.Contains(err.Error(), "rationale is required"), strings.Contains(err.Error(), "cannot complete"):
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
	case errors.Is(err, review.ErrNotConfigured):
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, err.Error())
	default:
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
	}
}
