package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// meResponse is also the login response body: the authenticated user's
// public profile. PasswordHash is never serialized (domain.User json:"-").
//
// AuthMode, Roles and Permissions are the CAP-01 additive extension: the shell
// needs the session's authority to decide what to render, and previously each
// page re-derived it from Role alone. Role is retained unchanged — the contract
// is extended, not replaced.
type meResponse struct {
	ID          string      `json:"id"`
	Email       string      `json:"email"`
	Role        domain.Role `json:"role"`
	AuthMode    AuthMode    `json:"auth_mode,omitempty"`
	Roles       []string    `json:"roles,omitempty"`
	Permissions []string    `json:"permissions,omitempty"`
}

// newMeResponse builds the profile body for both login and /auth/me so the two
// can never disagree about what a session may do.
func (s *Server) newMeResponse(user *domain.User) meResponse {
	return meResponse{
		ID:          user.ID,
		Email:       user.Email,
		Role:        user.Role,
		AuthMode:    s.authMode(),
		Roles:       []string{string(user.Role)},
		Permissions: rolePermissions(user.Role),
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.users == nil || s.refreshTokens == nil || s.tokenIssuer == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "authentication not configured")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid request body")
		return
	}

	user, err := s.users.GetByEmail(r.Context(), req.Email)
	if err != nil || !user.Active {
		if auditErr := s.recordAuthAudit(r, "", "login_failed"); auditErr != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "authentication audit could not be recorded")
			return
		}
		writeAuthError(w, http.StatusUnauthorized, apierr.CodeUnauthorized, "invalid email or password")
		return
	}

	ok, err := auth.VerifyPassword(user.PasswordHash, req.Password)
	if err != nil || !ok {
		if auditErr := s.recordAuthAudit(r, user.ID, "login_failed"); auditErr != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "authentication audit could not be recorded")
			return
		}
		writeAuthError(w, http.StatusUnauthorized, apierr.CodeUnauthorized, "invalid email or password")
		return
	}

	rawRefresh, refreshToken, err := auth.PrepareRefreshToken(user.ID, user.Role)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	refreshToken.AuthorityUpdatedAt = user.UpdatedAt

	accessToken, err := s.tokenIssuer.IssueAccessTokenForSession(user.ID, string(user.Role), generateID(), refreshToken.TokenFamily)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	auditEntry := s.newAuthAuditEntry(r, user.ID, user.ID, "login_success")
	evictedFamilies, err := s.refreshTokens.CreateSessionWithAudit(r.Context(), refreshToken, auth.MaxConcurrentSessions, auditEntry)
	if err != nil {
		if errors.Is(err, domain.ErrSessionAuthorityChanged) {
			if auditErr := s.recordAuthAudit(r, user.ID, "login_failed"); auditErr != nil {
				writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "authentication audit could not be recorded")
				return
			}
			writeAuthError(w, http.StatusUnauthorized, apierr.CodeUnauthorized, "account authority changed during login")
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "authentication audit could not be recorded")
		return
	}
	s.markAuthAuditHandled(r)
	if s.denylist != nil {
		for _, evictedFamily := range evictedFamilies {
			if err := s.denylist.RevokeSession(r.Context(), evictedFamily, auth.AccessTokenTTL); err != nil {
				log.Printf("cache evicted session revocation error: %v", err)
			}
		}
	}

	setAccessCookie(w, accessToken)
	setRefreshCookie(w, rawRefresh)
	setCSRFCookie(w, generateID()+generateID())

	writeJSON(w, http.StatusOK, s.newMeResponse(user))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !csrfTokenMatches(r) {
		writeAuthError(w, http.StatusForbidden, apierr.CodeForbidden, "missing or invalid CSRF token")
		return
	}

	userID := ""
	sessionRevoked := false
	revocationFailed := false
	families := make(map[string]struct{})

	if s.tokenIssuer != nil {
		if cookie, err := r.Cookie(accessTokenCookie); err == nil {
			if claims, err := s.tokenIssuer.VerifyAccessToken(cookie.Value); err == nil {
				userID = claims.UserID
				if claims.SessionID != "" {
					families[claims.SessionID] = struct{}{}
				}
				if s.denylist != nil {
					ttl := time.Until(claims.ExpiresAt.Time)
					if ttl <= 0 {
						ttl = time.Minute
					}
					if claims.JTI != "" {
						if err := s.denylist.RevokeToken(r.Context(), claims.JTI, ttl); err != nil {
							log.Printf("denylist access-token revoke error: %v", err)
						}
					}
					if claims.SessionID != "" {
						if err := s.denylist.RevokeSession(r.Context(), claims.SessionID, auth.AccessTokenTTL); err != nil {
							log.Printf("denylist session revoke error: %v", err)
						}
					}
				}
			}
		}
	}

	if s.refreshTokens != nil {
		if cookie, err := r.Cookie(refreshTokenCookie); err == nil {
			tok, lookupErr := s.refreshTokens.GetByHash(r.Context(), auth.HashRefreshToken(cookie.Value))
			if lookupErr != nil {
				log.Printf("lookup refresh token for logout error: %v", lookupErr)
				var notFound *domain.ErrNotFound
				if !errors.As(lookupErr, &notFound) {
					revocationFailed = true
				}
			} else {
				if userID == "" {
					userID = tok.UserID
				}
				families[tok.TokenFamily] = struct{}{}
				if s.denylist != nil {
					if err := s.denylist.RevokeSession(r.Context(), tok.TokenFamily, auth.AccessTokenTTL); err != nil {
						log.Printf("denylist refresh-family revoke error: %v", err)
					}
				}
			}
		}
	}
	if len(families) > 0 {
		if s.refreshTokens == nil {
			revocationFailed = true
		} else {
			for family := range families {
				if err := s.refreshTokens.RevokeFamily(r.Context(), family); err != nil {
					log.Printf("revoke refresh token family error: %v", err)
					revocationFailed = true
				} else {
					sessionRevoked = true
				}
			}
		}
	}

	clearSessionCookies(w)
	if sessionRevoked {
		if err := s.recordAuthAudit(r, userID, "session_revocation"); err != nil {
			revocationFailed = true
		}
	}
	if revocationFailed {
		_ = s.recordAuthAudit(r, userID, "logout_failed")
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "session revocation could not be confirmed")
		return
	}
	if err := s.recordAuthAudit(r, userID, "logout"); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "logout audit could not be recorded")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if s.refreshTokens == nil || s.users == nil || s.tokenIssuer == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "authentication not configured")
		return
	}
	if !csrfTokenMatches(r) {
		writeAuthError(w, http.StatusForbidden, apierr.CodeForbidden, "missing or invalid CSRF token")
		return
	}

	cookie, err := r.Cookie(refreshTokenCookie)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, apierr.CodeUnauthorized, "missing refresh token")
		return
	}

	newRaw, family, err := auth.RotateRefreshToken(r.Context(), s.refreshTokens, cookie.Value)
	if err != nil {
		if errors.Is(err, auth.ErrTokenReuseDetected) && family != "" && s.denylist != nil {
			if revokeErr := s.denylist.RevokeSession(r.Context(), family, auth.AccessTokenTTL); revokeErr != nil {
				log.Printf("cache reused refresh family revocation error: %v", revokeErr)
			}
			if reused, lookupErr := s.refreshTokens.GetByHash(r.Context(), auth.HashRefreshToken(cookie.Value)); lookupErr == nil {
				if auditErr := s.recordAuthAudit(r, reused.UserID, "session_revocation"); auditErr != nil {
					writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "session revocation audit could not be recorded")
					return
				}
			}
		}
		writeAuthError(w, http.StatusUnauthorized, apierr.CodeUnauthorized, "invalid or reused refresh token")
		return
	}

	tok, err := s.refreshTokens.GetByHash(r.Context(), auth.HashRefreshToken(newRaw))
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	user, err := s.users.Get(r.Context(), tok.UserID)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if !user.Active || tok.SessionRole == "" || tok.SessionRole != user.Role {
		if revokeErr := s.refreshTokens.RevokeFamily(r.Context(), tok.TokenFamily); revokeErr != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "session authority revocation could not be persisted")
			return
		}
		if s.denylist != nil {
			if revokeErr := s.denylist.RevokeSession(r.Context(), tok.TokenFamily, auth.AccessTokenTTL); revokeErr != nil {
				log.Printf("cache authority-change session revocation error: %v", revokeErr)
			}
		}
		clearSessionCookies(w)
		if auditErr := s.recordAuthAudit(r, user.ID, "session_revocation"); auditErr != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "session revocation audit could not be recorded")
			return
		}
		writeAuthError(w, http.StatusUnauthorized, apierr.CodeUnauthorized, "session authority has changed")
		return
	}

	accessToken, err := s.tokenIssuer.IssueAccessTokenForSession(user.ID, string(user.Role), generateID(), family)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if err := s.recordAuthAudit(r, user.ID, "refresh"); err != nil {
		if revokeErr := auth.RevokeRefreshTokenFamily(r.Context(), s.refreshTokens, newRaw); revokeErr != nil {
			log.Printf("revoke refresh family after refresh audit failure: %v", revokeErr)
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "refresh audit could not be recorded")
		return
	}

	setAccessCookie(w, accessToken)
	setRefreshCookie(w, newRaw)

	writeJSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
}

func (s *Server) handleRevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	if s.users == nil || s.refreshTokens == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "session revocation not configured")
		return
	}

	userID := r.PathValue("id")
	if _, err := s.users.Get(r.Context(), userID); err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, "user not found")
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	auditEntry := s.newAuthAuditEntry(r, resolveAuditUserID(r), userID, "user_wide_session_revocation")
	families, err := s.refreshTokens.RevokeUserSessionsWithAudit(r.Context(), userID, auditEntry)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	s.markAuthAuditHandled(r)

	for _, family := range families {
		if s.denylist != nil {
			if err := s.denylist.RevokeSession(r.Context(), family, auth.AccessTokenTTL); err != nil {
				log.Printf("cache user-wide session revocation error: %v", err)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked", "revoked_sessions": len(families)})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if s.users == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "user management not configured")
		return
	}

	principal, ok := r.Context().Value(ctxKeyPrincipal).(Principal)
	if !ok || principal.UserID == "" {
		writeAuthError(w, http.StatusUnauthorized, apierr.CodeUnauthorized, "not authenticated as a user")
		return
	}

	user, err := s.users.Get(r.Context(), principal.UserID)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, s.newMeResponse(user))
}

// handleListUsers is the admin user-management screen's data source
// (ui/src/pages/users.tsx). Routed under /api/v1/admin/ so authMiddleware's
// existing admin-only prefix check applies.
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if s.users == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "user management not configured")
		return
	}

	users, err := s.users.List(r.Context())
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if users == nil {
		users = []domain.User{}
	}

	writeJSON(w, http.StatusOK, users)
}

func (s *Server) recordAuthAudit(r *http.Request, userID, action string) error {
	return s.recordAuthAuditForResource(r, userID, userID, action)
}

func (s *Server) recordAuthAuditForResource(r *http.Request, actorID, resourceID, action string) error {
	if s.audit == nil {
		return nil
	}
	entry := s.newAuthAuditEntry(r, actorID, resourceID, action)
	if err := s.audit.Create(r.Context(), entry); err != nil {
		log.Printf("audit write error: %v", err)
		return err
	}
	s.markAuthAuditHandled(r)
	return nil
}

func (s *Server) newAuthAuditEntry(r *http.Request, actorID, resourceID, action string) *domain.AuditEntry {
	auditUserID := actorID
	if auditUserID == "" {
		auditUserID = "anonymous"
	}

	return &domain.AuditEntry{
		UserID:       auditUserID,
		Action:       action,
		ResourceType: "auth",
		ResourceID:   resourceID,
		IPAddress:    extractIP(r),
		UserAgent:    r.UserAgent(),
		CreatedAt:    time.Now(),
	}
}

func (s *Server) markAuthAuditHandled(r *http.Request) {
	if sink, ok := r.Context().Value(auditDetailsKey{}).(*auditDetailsSink); ok {
		sink.markHandledByRoute()
	}
}

func setAccessCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessTokenCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(auth.AccessTokenTTL.Seconds()),
	})
}

func setRefreshCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshTokenCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(auth.RefreshTokenTTL.Seconds()),
	})
}

// setCSRFCookie issues the Double Submit Cookie CSRF token (the authentication model §2). It
// is intentionally not HttpOnly: client-side JS must read it and echo it
// back in the X-CSRF-Token header on state-changing requests.
func setCSRFCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(auth.RefreshTokenTTL.Seconds()),
	})
}

func clearSessionCookies(w http.ResponseWriter) {
	for _, name := range []string{accessTokenCookie, refreshTokenCookie, csrfCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			HttpOnly: name != csrfCookieName,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   -1,
		})
	}
}
