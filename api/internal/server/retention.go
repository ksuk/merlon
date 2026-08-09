package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
)

func (s *Server) handleListRetentionPolicies(w http.ResponseWriter, r *http.Request) {
	if s.retention == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "retention policy management not configured")
		return
	}

	policies, err := s.retention.List(r.Context())
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if policies == nil {
		policies = []domain.RetentionPolicy{}
	}

	writeJSON(w, http.StatusOK, policies)
}

type updateRetentionPolicyRequest struct {
	RetentionDays int `json:"retention_days"`
}

// handleUpdateRetentionPolicy updates a deployment-controlled retention
// period. Zero and negative periods are rejected to prevent a bad setting
// from making all records immediately eligible for purge.
func (s *Server) handleUpdateRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	if s.retention == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "retention policy management not configured")
		return
	}

	category := r.PathValue("category")

	var req updateRetentionPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	if req.RetentionDays <= 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, (&domain.ErrInvalidRetentionDays{Days: req.RetentionDays}).Error())
		return
	}

	existing, err := s.retention.Get(r.Context(), category)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	if existing.MinRetentionDays != nil && req.RetentionDays < *existing.MinRetentionDays {
		shorten := &domain.ErrRetentionShorten{
			DataCategory:  category,
			RequestedDays: req.RetentionDays,
			MinDays:       *existing.MinRetentionDays,
		}
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, shorten.Error())
		return
	}
	if s.audit == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeInternal, "audit repository not configured")
		return
	}
	if err := s.audit.Create(r.Context(), &domain.AuditEntry{
		UserID:       resolveAuditUserID(r),
		Action:       "retention_policy_update_started",
		ResourceType: "retention_policy",
		ResourceID:   category,
		Details: map[string]string{
			"previous_retention_days":  strconv.Itoa(existing.RetentionDays),
			"requested_retention_days": strconv.Itoa(req.RetentionDays),
		},
		CreatedAt: time.Now(),
	}); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	updated, err := s.retention.Update(r.Context(), category, req.RetentionDays, resolveAuditUserID(r))
	if err != nil {
		var shorten *domain.ErrRetentionShorten
		if errors.As(err, &shorten) {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, shorten.Error())
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

	entry := &domain.AuditEntry{
		UserID:       resolveAuditUserID(r),
		Action:       "update_retention_policy",
		ResourceType: "retention_policy",
		ResourceID:   category,
		Details: map[string]string{
			"previous_retention_days": strconv.Itoa(existing.RetentionDays),
			"retention_days":          strconv.Itoa(updated.RetentionDays),
		},
		CreatedAt: time.Now(),
	}
	if err := s.audit.Create(r.Context(), entry); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, updated)
}
