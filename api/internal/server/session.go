package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/merlon-aml/merlon/api/internal/auth"
	"github.com/merlon-aml/merlon/api/internal/domain"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// meResponse is also the login response body: the authenticated user's
// public profile. PasswordHash is never serialized (domain.User json:"-").
type meResponse struct {
	ID    string      `json:"id"`
	Email string      `json:"email"`
	Role  domain.Role `json:"role"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.users == nil || s.refreshTokens == nil || s.tokenIssuer == nil {
		writeError(w, http.StatusServiceUnavailable, "authentication not configured")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := s.users.GetByEmail(r.Context(), req.Email)
	if err != nil || !user.Active {
		s.recordAuthAudit(r, "", "login_failed")
		writeAuthError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	ok, err := auth.VerifyPassword(user.PasswordHash, req.Password)
	if err != nil || !ok {
		s.recordAuthAudit(r, user.ID, "login_failed")
		writeAuthError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	accessToken, err := s.tokenIssuer.IssueAccessToken(user.ID, string(user.Role), generateID())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	rawRefresh, _, err := auth.IssueRefreshToken(r.Context(), s.refreshTokens, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	setAccessCookie(w, accessToken)
	setRefreshCookie(w, rawRefresh)
	setCSRFCookie(w, generateID()+generateID())

	s.recordAuthAudit(r, user.ID, "login_success")

	writeJSON(w, http.StatusOK, meResponse{ID: user.ID, Email: user.Email, Role: user.Role})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	userID := ""

	if s.tokenIssuer != nil {
		if cookie, err := r.Cookie(accessTokenCookie); err == nil {
			if claims, err := s.tokenIssuer.VerifyAccessToken(cookie.Value); err == nil {
				userID = claims.UserID
				if s.denylist != nil {
					ttl := time.Until(claims.ExpiresAt.Time)
					if ttl <= 0 {
						ttl = time.Minute
					}
					if err := s.denylist.Revoke(r.Context(), userID, ttl); err != nil {
						log.Printf("denylist revoke error: %v", err)
					}
				}
			}
		}
	}

	if s.refreshTokens != nil {
		if cookie, err := r.Cookie(refreshTokenCookie); err == nil {
			if err := auth.RevokeRefreshTokenFamily(r.Context(), s.refreshTokens, cookie.Value); err != nil {
				log.Printf("revoke refresh token family error: %v", err)
			}
		}
	}

	clearSessionCookies(w)
	s.recordAuthAudit(r, userID, "logout")

	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if s.refreshTokens == nil || s.users == nil || s.tokenIssuer == nil {
		writeError(w, http.StatusServiceUnavailable, "authentication not configured")
		return
	}

	cookie, err := r.Cookie(refreshTokenCookie)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "missing refresh token")
		return
	}

	newRaw, _, err := auth.RotateRefreshToken(r.Context(), s.refreshTokens, cookie.Value)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "invalid or reused refresh token")
		return
	}

	tok, err := s.refreshTokens.GetByHash(r.Context(), auth.HashRefreshToken(newRaw))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	user, err := s.users.Get(r.Context(), tok.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	accessToken, err := s.tokenIssuer.IssueAccessToken(user.ID, string(user.Role), generateID())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	setAccessCookie(w, accessToken)
	setRefreshCookie(w, newRaw)

	writeJSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if s.users == nil {
		writeError(w, http.StatusServiceUnavailable, "user management not configured")
		return
	}

	principal, ok := r.Context().Value(ctxKeyPrincipal).(Principal)
	if !ok || principal.UserID == "" {
		writeAuthError(w, http.StatusUnauthorized, "not authenticated as a user")
		return
	}

	user, err := s.users.Get(r.Context(), principal.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, meResponse{ID: user.ID, Email: user.Email, Role: user.Role})
}

func (s *Server) recordAuthAudit(r *http.Request, userID, action string) {
	if s.audit == nil {
		return
	}

	auditUserID := userID
	if auditUserID == "" {
		auditUserID = "anonymous"
	}

	entry := &domain.AuditEntry{
		UserID:       auditUserID,
		Action:       action,
		ResourceType: "auth",
		ResourceID:   userID,
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

// setCSRFCookie issues the Double Submit Cookie CSRF token (auth.md §2). It
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
