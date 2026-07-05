package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

func (s *Server) handleListRetentionPolicies(w http.ResponseWriter, r *http.Request) {
	if s.retention == nil {
		writeError(w, http.StatusServiceUnavailable, "retention policy management not configured")
		return
	}

	policies, err := s.retention.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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

// handleUpdateRetentionPolicy extends (never shortens) a data category's
// retention period (audit.md RET-002, 設計原則5: 延長のみ可). The
// below-minimum check is performed here first so the response is a clear
// error_code-style message (retention_shorten_forbidden) rather than a raw
// CHECK-constraint error surfaced from the store as a generic 500; the store
// layer (retention_no_shorten CHECK, migrations/017_retention.sql) still
// enforces the same rule as defense in depth for callers that bypass this
// handler.
//
// TODO(WS-10 error_code): once WS-10's error_code-based error envelope
// lands, replace the "retention_shorten_forbidden: " message prefix with a
// structured error_code field.
func (s *Server) handleUpdateRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	if s.retention == nil {
		writeError(w, http.StatusServiceUnavailable, "retention policy management not configured")
		return
	}

	category := r.PathValue("category")

	var req updateRetentionPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	existing, err := s.retention.Get(r.Context(), category)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if existing.MinRetentionDays != nil && req.RetentionDays < *existing.MinRetentionDays {
		shorten := &domain.ErrRetentionShorten{
			DataCategory:  category,
			RequestedDays: req.RetentionDays,
			MinDays:       *existing.MinRetentionDays,
		}
		writeError(w, http.StatusBadRequest, shorten.Error())
		return
	}

	updated, err := s.retention.Update(r.Context(), category, req.RetentionDays, resolveAuditUserID(r))
	if err != nil {
		var shorten *domain.ErrRetentionShorten
		if errors.As(err, &shorten) {
			writeError(w, http.StatusBadRequest, shorten.Error())
			return
		}
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, updated)
}
