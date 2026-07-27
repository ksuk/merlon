package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
)

type setupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleSetup implements the initial setup flow (the operational design §4.5): only the
// first call (users table empty) succeeds, and it creates the first
// administrator account. Until this has run, /healthz/ready reports
// unhealthy and login is unreachable (no users exist to log in as).
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if s.users == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "user management not configured")
		return
	}

	count, err := s.users.Count(r.Context())
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if count > 0 {
		writeAuthError(w, http.StatusConflict, apierr.CodeConflict, "initial setup has already been completed")
		return
	}

	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid request body")
		return
	}
	if req.Email == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "email is required")
		return
	}
	if err := auth.ValidatePasswordPolicy(req.Password); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	now := time.Now()
	user := &domain.User{
		ID:           generateID(),
		Email:        req.Email,
		PasswordHash: hash,
		Role:         domain.RoleAdmin,
		Active:       true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	created, err := s.users.CreateIfEmpty(r.Context(), user)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if !created {
		writeAuthError(w, http.StatusConflict, apierr.CodeConflict, "initial setup has already been completed")
		return
	}

	s.recordAuthAudit(r, user.ID, "initial_setup")

	writeJSON(w, http.StatusCreated, meResponse{ID: user.ID, Email: user.Email, Role: user.Role})
}
