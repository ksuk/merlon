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
		s.recordAuthAudit(r, "", "login_failed")
		writeAuthError(w, http.StatusUnauthorized, apierr.CodeUnauthorized, "invalid email or password")
		return
	}

	ok, err := auth.VerifyPassword(user.PasswordHash, req.Password)
	if err != nil || !ok {
		s.recordAuthAudit(r, user.ID, "login_failed")
		writeAuthError(w, http.StatusUnauthorized, apierr.CodeUnauthorized, "invalid email or password")
		return
	}

	rawRefresh, family, evictedFamily, err := auth.IssueRefreshTokenWithEviction(r.Context(), s.refreshTokens, user.ID)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if evictedFamily != "" && s.denylist != nil {
		if err := s.denylist.RevokeSession(r.Context(), evictedFamily, auth.AccessTokenTTL); err != nil {
			if revokeErr := auth.RevokeRefreshTokenFamily(r.Context(), s.refreshTokens, rawRefresh); revokeErr != nil {
				log.Printf("revoke new refresh family after eviction denylist failure: %v", revokeErr)
			}
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
	}

	accessToken, err := s.tokenIssuer.IssueAccessTokenForSession(user.ID, string(user.Role), generateID(), family)
	if err != nil {
		if revokeErr := auth.RevokeRefreshTokenFamily(r.Context(), s.refreshTokens, rawRefresh); revokeErr != nil {
			log.Printf("revoke refresh token family after access-token failure: %v", revokeErr)
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	setAccessCookie(w, accessToken)
	setRefreshCookie(w, rawRefresh)
	setCSRFCookie(w, generateID()+generateID())

	s.recordAuthAudit(r, user.ID, "login_success")

	writeJSON(w, http.StatusOK, s.newMeResponse(user))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	userID := ""
	sessionRevoked := false
	revocationFailed := false

	if s.tokenIssuer != nil {
		if cookie, err := r.Cookie(accessTokenCookie); err == nil {
			if claims, err := s.tokenIssuer.VerifyAccessToken(cookie.Value); err == nil {
				userID = claims.UserID
				if s.denylist != nil {
					ttl := time.Until(claims.ExpiresAt.Time)
					if ttl <= 0 {
						ttl = time.Minute
					}
					if claims.JTI != "" {
						if err := s.denylist.RevokeToken(r.Context(), claims.JTI, ttl); err != nil {
							log.Printf("denylist access-token revoke error: %v", err)
							revocationFailed = true
						} else {
							sessionRevoked = true
						}
					}
					if claims.SessionID != "" {
						if err := s.denylist.RevokeSession(r.Context(), claims.SessionID, ttl); err != nil {
							log.Printf("denylist session revoke error: %v", err)
							revocationFailed = true
						} else {
							sessionRevoked = true
						}
					}
				} else {
					revocationFailed = true
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
				if s.denylist != nil {
					if err := s.denylist.RevokeSession(r.Context(), tok.TokenFamily, auth.AccessTokenTTL); err != nil {
						log.Printf("denylist refresh-family revoke error: %v", err)
						revocationFailed = true
					} else {
						sessionRevoked = true
					}
				} else {
					revocationFailed = true
				}
				if err := s.refreshTokens.RevokeFamily(r.Context(), tok.TokenFamily); err != nil {
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
		s.recordAuthAudit(r, userID, "session_revocation")
	}
	if revocationFailed {
		s.recordAuthAudit(r, userID, "logout_failed")
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "session revocation could not be confirmed")
		return
	}
	s.recordAuthAudit(r, userID, "logout")

	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if s.refreshTokens == nil || s.users == nil || s.tokenIssuer == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "authentication not configured")
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
				writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, revokeErr.Error())
				return
			}
			if reused, lookupErr := s.refreshTokens.GetByHash(r.Context(), auth.HashRefreshToken(cookie.Value)); lookupErr == nil {
				s.recordAuthAudit(r, reused.UserID, "session_revocation")
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

	accessToken, err := s.tokenIssuer.IssueAccessTokenForSession(user.ID, string(user.Role), generateID(), family)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	setAccessCookie(w, accessToken)
	setRefreshCookie(w, newRaw)
	s.recordAuthAudit(r, user.ID, "refresh")

	writeJSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
}

func (s *Server) handleRevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	if s.users == nil || s.refreshTokens == nil || s.denylist == nil {
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

	active, err := s.refreshTokens.ListActiveByUser(r.Context(), userID)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	families := make(map[string]struct{}, len(active))
	for _, token := range active {
		families[token.TokenFamily] = struct{}{}
	}
	for family := range families {
		// Deny access before revoking refresh state. If the repository write
		// fails, a retry cannot mint a usable token for this family.
		if err := s.denylist.RevokeSession(r.Context(), family, auth.AccessTokenTTL); err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		if err := s.refreshTokens.RevokeFamily(r.Context(), family); err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
	}

	s.recordAuthAuditForResource(r, resolveAuditUserID(r), userID, "user_wide_session_revocation")
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

func (s *Server) recordAuthAudit(r *http.Request, userID, action string) {
	s.recordAuthAuditForResource(r, userID, userID, action)
}

func (s *Server) recordAuthAuditForResource(r *http.Request, actorID, resourceID, action string) {
	if s.audit == nil {
		return
	}

	auditUserID := actorID
	if auditUserID == "" {
		auditUserID = "anonymous"
	}

	entry := &domain.AuditEntry{
		UserID:       auditUserID,
		Action:       action,
		ResourceType: "auth",
		ResourceID:   resourceID,
		IPAddress:    extractIP(r),
		UserAgent:    r.UserAgent(),
		CreatedAt:    time.Now(),
	}
	if err := s.audit.Create(r.Context(), entry); err != nil {
		log.Printf("audit write error: %v", err)
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
