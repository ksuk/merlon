package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
)

type createUserRequest struct {
	Email    string      `json:"email"`
	Password string      `json:"password"`
	Role     domain.Role `json:"role"`
}

type updateUserAuthorityRequest struct {
	Role   *domain.Role `json:"role"`
	Active *bool        `json:"active"`
}

type resetUserPasswordRequest struct {
	Password string `json:"password"`
}

func validManagedRole(role domain.Role) bool {
	return role == domain.RoleAdmin || role == domain.RoleAnalyst || role == domain.RoleViewer
}

func normalizeUserEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	parsed, err := mail.ParseAddress(email)
	return email, err == nil && parsed.Address == email && strings.Contains(email, "@")
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if s.userLifecycle == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "user lifecycle management not configured")
		return
	}
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid request body")
		return
	}
	email, valid := normalizeUserEmail(req.Email)
	if !valid {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "valid email is required")
		return
	}
	if !validManagedRole(req.Role) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "role must be admin, analyst, or viewer")
		return
	}
	if err := auth.ValidatePasswordPolicy(req.Password); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "password could not be secured")
		return
	}
	now := time.Now()
	user := &domain.User{ID: generateID(), Email: email, PasswordHash: hash, Role: req.Role, Active: true, CreatedAt: now, UpdatedAt: now}
	entry := s.userManagementAudit(r, user.ID, "user_created", map[string]string{"role": string(user.Role)})
	if err := s.userLifecycle.CreateUser(r.Context(), user, entry); err != nil {
		writeUserLifecycleError(w, err)
		return
	}
	s.markAuthAuditHandled(r)
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handleUpdateUserAuthority(w http.ResponseWriter, r *http.Request) {
	if s.userLifecycle == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "user lifecycle management not configured")
		return
	}
	var req updateUserAuthorityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid request body")
		return
	}
	if req.Role == nil || req.Active == nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "role and active are required")
		return
	}
	if req.Role != nil && !validManagedRole(*req.Role) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "role must be admin, analyst, or viewer")
		return
	}
	id := r.PathValue("id")
	details := map[string]string{"role": string(*req.Role), "active": strconv.FormatBool(*req.Active)}
	entry := s.userManagementAudit(r, id, "user_authority_updated", details)
	updated, families, err := s.userLifecycle.UpdateAuthority(r.Context(), id, *req.Role, *req.Active, time.Now(), entry)
	if err != nil {
		writeUserLifecycleError(w, err)
		return
	}
	s.markAuthAuditHandled(r)
	s.revokeCachedUserFamilies(r, families)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleResetUserPassword(w http.ResponseWriter, r *http.Request) {
	if s.userLifecycle == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "user lifecycle management not configured")
		return
	}
	var req resetUserPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid request body")
		return
	}
	if err := auth.ValidatePasswordPolicy(req.Password); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "password could not be secured")
		return
	}
	id := r.PathValue("id")
	entry := s.userManagementAudit(r, id, "user_password_reset", nil)
	updated, families, err := s.userLifecycle.ResetPassword(r.Context(), id, hash, time.Now(), entry)
	if err != nil {
		writeUserLifecycleError(w, err)
		return
	}
	s.markAuthAuditHandled(r)
	s.revokeCachedUserFamilies(r, families)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) userManagementAudit(r *http.Request, targetID, action string, details map[string]string) *domain.AuditEntry {
	entry := s.newAuthAuditEntry(r, resolveAuditUserID(r), targetID, action)
	entry.ResourceType = "users"
	entry.Details = details
	return entry
}

func (s *Server) revokeCachedUserFamilies(r *http.Request, families []string) {
	if s.denylist == nil {
		return
	}
	for _, family := range families {
		if err := s.denylist.RevokeSession(r.Context(), family, auth.AccessTokenTTL); err != nil {
			// PostgreSQL is authoritative. Cache failure cannot make a revoked
			// persistent session valid again.
			log.Printf("denylist session revoke error: %v", err)
			continue
		}
	}
}

func writeUserLifecycleError(w http.ResponseWriter, err error) {
	var notFound *domain.ErrNotFound
	if errors.As(err, &notFound) {
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
		return
	}
	var conflict *domain.ErrConflict
	if errors.As(err, &conflict) {
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, err.Error())
		return
	}
	log.Printf("user lifecycle operation error: %v", err)
	writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "user lifecycle operation failed")
}
