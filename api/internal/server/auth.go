package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/ksuk/merlon/api/internal/apierr"
	"net/http"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
)

type contextKey string

const (
	ctxKeyAPIKey       contextKey = "api_key"
	ctxKeyPrincipal    contextKey = "principal"
	accessTokenCookie             = "access_token"
	refreshTokenCookie            = "refresh_token"
	csrfCookieName                = "csrf_token"
	csrfHeaderName                = "X-CSRF-Token"
)

// Principal is the unified authenticated subject, whether it arrived via a
// JWT session cookie (UserID non-empty) or an API key (APIKeyID non-empty).
type Principal struct {
	Role     domain.Role
	UserID   string
	APIKeyID string
}

func HashAPIKey(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.apikeys == nil || r.URL.Path == "/healthz" || r.URL.Path == "/healthz/live" || r.URL.Path == "/healthz/ready" {
			next.ServeHTTP(w, r)
			return
		}

		// Session endpoints authenticate themselves (login: credentials;
		// refresh/logout: the refresh_token cookie, which may outlive an
		// already-expired access token).
		if isPublicAuthPath(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Bootstrap token: only allows POST /api/v1/admin/apikeys (first key creation)
		if s.bootstrapToken != "" && r.URL.Path == "/api/v1/admin/apikeys" && r.Method == http.MethodPost {
			header := r.Header.Get("Authorization")
			if header == "Bearer "+s.bootstrapToken {
				if !s.bootstrapTokenAllowed(r.Context()) {
					writeAuthError(w, http.StatusUnauthorized, apierr.CodeUnauthorized, "bootstrap token has already been used")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
		}

		principal, ctx, err := s.authenticate(r)
		if err != nil {
			writeAuthError(w, err.status, err.code, err.message)
			return
		}

		// Admin routes require admin role
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/") && principal.Role != domain.RoleAdmin {
			writeAuthError(w, http.StatusForbidden, apierr.CodeForbidden, "admin role required")
			return
		}

		if !hasPermission(principal.Role, r.Method, r.URL.Path) {
			writeAuthError(w, http.StatusForbidden, apierr.CodeForbidden, "insufficient permissions")
			return
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type authError struct {
	status  int
	code    apierr.Code
	message string
}

// authenticate accepts either an API key (Authorization: Bearer <key>) or a
// JWT session cookie (access_token), preferring the Authorization header
// when both are present. Both paths deposit the same Principal into the
// returned context (server.ctxKeyPrincipal and auth.WithRole).
func (s *Server) authenticate(r *http.Request) (Principal, context.Context, *authError) {
	if header := r.Header.Get("Authorization"); header != "" {
		return s.authenticateAPIKey(r, header)
	}

	if cookie, err := r.Cookie(accessTokenCookie); err == nil {
		return s.authenticateJWT(r, cookie.Value)
	}

	return Principal{}, nil, &authError{http.StatusUnauthorized, apierr.CodeUnauthorized, "missing Authorization header"}
}

func (s *Server) authenticateAPIKey(r *http.Request, header string) (Principal, context.Context, *authError) {
	if !strings.HasPrefix(header, "Bearer ") {
		return Principal{}, nil, &authError{http.StatusUnauthorized, apierr.CodeUnauthorized, "invalid Authorization format"}
	}

	token := strings.TrimPrefix(header, "Bearer ")
	keyHash := HashAPIKey(token)

	key, err := s.apikeys.GetByHash(r.Context(), keyHash)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			return Principal{}, nil, &authError{http.StatusUnauthorized, apierr.CodeUnauthorized, "invalid API key"}
		}
		return Principal{}, nil, &authError{http.StatusInternalServerError, apierr.CodeInternal, err.Error()}
	}

	if !key.Active {
		return Principal{}, nil, &authError{http.StatusUnauthorized, apierr.CodeUnauthorized, "API key revoked"}
	}

	now := time.Now()
	key.LastUsed = &now

	principal := Principal{Role: key.Role, APIKeyID: key.ID}
	ctx := context.WithValue(r.Context(), ctxKeyAPIKey, key)
	ctx = context.WithValue(ctx, ctxKeyPrincipal, principal)
	ctx = auth.WithRole(ctx, principal.Role)
	return principal, ctx, nil
}

func (s *Server) authenticateJWT(r *http.Request, token string) (Principal, context.Context, *authError) {
	if s.tokenIssuer == nil {
		return Principal{}, nil, &authError{http.StatusUnauthorized, apierr.CodeUnauthorized, "JWT authentication not configured"}
	}

	claims, err := s.tokenIssuer.VerifyAccessToken(token)
	if err != nil {
		return Principal{}, nil, &authError{http.StatusUnauthorized, apierr.CodeUnauthorized, "invalid or expired session"}
	}

	if s.denylist != nil {
		revoked, err := s.denylist.IsRevoked(r.Context(), claims.UserID)
		if err != nil {
			return Principal{}, nil, &authError{http.StatusInternalServerError, apierr.CodeInternal, err.Error()}
		}
		if revoked {
			return Principal{}, nil, &authError{http.StatusUnauthorized, apierr.CodeUnauthorized, "session has been revoked"}
		}
	}

	// CSRF (Double Submit Cookie, the authentication model §2): cookie-based auth is subject to
	// CSRF, unlike the Bearer API key path, so state-changing requests must
	// echo the non-HttpOnly csrf_token cookie back in a header.
	if !isSafeMethod(r.Method) && !csrfTokenMatches(r) {
		return Principal{}, nil, &authError{http.StatusForbidden, apierr.CodeForbidden, "missing or invalid CSRF token"}
	}

	principal := Principal{Role: domain.Role(claims.Role), UserID: claims.UserID}
	ctx := context.WithValue(r.Context(), ctxKeyPrincipal, principal)
	ctx = auth.WithRole(ctx, principal.Role)
	return principal, ctx, nil
}

// isPublicAuthPath reports whether r targets a session endpoint that
// authenticates itself rather than relying on authMiddleware (login checks
// credentials directly; refresh/logout authenticate via the refresh_token
// cookie, which may still be valid after the access token has expired).
func isPublicAuthPath(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v1/auth/login", "/api/v1/auth/refresh", "/api/v1/auth/logout", "/api/v1/setup":
		return true
	}
	return false
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func csrfTokenMatches(r *http.Request) bool {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	header := r.Header.Get(csrfHeaderName)
	if header == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) == 1
}

// bootstrapTokenAllowed reports whether the bootstrap token may still be
// used to create the first API key. It is disabled once any API key exists,
// and (AUTH-006 "初期セットアップ完了後は無効化") also once initial setup
// has completed, i.e. any local user account exists.
func (s *Server) bootstrapTokenAllowed(ctx context.Context) bool {
	if s.apikeys == nil {
		return false
	}
	keys, err := s.apikeys.List(ctx)
	if err != nil || len(keys) != 0 {
		return false
	}

	if s.users != nil {
		count, err := s.users.Count(ctx)
		if err != nil || count != 0 {
			return false
		}
	}

	return true
}

func hasPermission(role domain.Role, method, path string) bool {
	switch role {
	case domain.RoleAdmin:
		return true
	case domain.RoleAnalyst:
		return true
	case domain.RoleViewer:
		return method == http.MethodGet
	default:
		return false
	}
}

func writeAuthError(w http.ResponseWriter, status int, code apierr.Code, msg string) {
	writeJSON(w, status, errorResponse{Error: msg, Code: code})
}

type createAPIKeyRequest struct {
	Name string      `json:"name"`
	Role domain.Role `json:"role"`
}

type createAPIKeyResponse struct {
	ID   string      `json:"id"`
	Name string      `json:"name"`
	Role domain.Role `json:"role"`
	Key  string      `json:"key"`
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if s.apikeys == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "API key management not configured")
		return
	}

	var req createAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	if req.Name == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "name is required")
		return
	}
	if req.Role == "" {
		req.Role = domain.RoleViewer
	}

	rawKey := generateID() + "-" + generateID()
	keyHash := HashAPIKey(rawKey)

	key := &domain.APIKey{
		ID:        generateID(),
		Name:      req.Name,
		KeyHash:   keyHash,
		Role:      req.Role,
		Active:    true,
		CreatedAt: time.Now(),
	}

	if err := s.apikeys.Create(r.Context(), key); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, createAPIKeyResponse{
		ID:   key.ID,
		Name: key.Name,
		Role: key.Role,
		Key:  rawKey,
	})
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	if s.apikeys == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "API key management not configured")
		return
	}

	keys, err := s.apikeys.List(r.Context())
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if keys == nil {
		keys = []domain.APIKey{}
	}

	writeJSON(w, http.StatusOK, keys)
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	if s.apikeys == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "API key management not configured")
		return
	}

	id := r.PathValue("id")
	if err := s.apikeys.Revoke(r.Context(), id); err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
