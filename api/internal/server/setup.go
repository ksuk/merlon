package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/merlon-aml/merlon/api/internal/auth"
	"github.com/merlon-aml/merlon/api/internal/domain"
)

type setupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleSetup implements the initial setup flow (overview.md §4.5): only the
// first call (users table empty) succeeds, and it creates the first
// administrator account. Until this has run, /healthz/ready reports
// unhealthy and login is unreachable (no users exist to log in as).
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if s.users == nil {
		writeError(w, http.StatusServiceUnavailable, "user management not configured")
		return
	}

	count, err := s.users.Count(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count > 0 {
		writeAuthError(w, http.StatusConflict, "initial setup has already been completed")
		return
	}

	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if err := auth.ValidatePasswordPolicy(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
	if err := s.users.Create(r.Context(), user); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.recordAuthAudit(r, user.ID, "initial_setup")

	writeJSON(w, http.StatusCreated, meResponse{ID: user.ID, Email: user.Email, Role: user.Role})
}
