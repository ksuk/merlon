package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/merlon-aml/merlon/api/internal/auth"
	"github.com/merlon-aml/merlon/api/internal/domain"
)

type contextKey string

const (
	ctxKeyAPIKey      contextKey = "api_key"
	ctxKeyPrincipal   contextKey = "principal"
	accessTokenCookie            = "access_token"
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
		if s.apikeys == nil || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		// Bootstrap token: only allows POST /api/v1/admin/apikeys (first key creation)
		if s.bootstrapToken != "" && r.URL.Path == "/api/v1/admin/apikeys" && r.Method == http.MethodPost {
			header := r.Header.Get("Authorization")
			if header == "Bearer "+s.bootstrapToken {
				if !s.bootstrapTokenAllowed(r.Context()) {
					writeAuthError(w, http.StatusUnauthorized, "bootstrap token has already been used")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
		}

		principal, ctx, err := s.authenticate(r)
		if err != nil {
			writeAuthError(w, err.status, err.message)
			return
		}

		// Admin routes require admin role
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/") && principal.Role != domain.RoleAdmin {
			writeAuthError(w, http.StatusForbidden, "admin role required")
			return
		}

		if !hasPermission(principal.Role, r.Method, r.URL.Path) {
			writeAuthError(w, http.StatusForbidden, "insufficient permissions")
			return
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type authError struct {
	status  int
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

	return Principal{}, nil, &authError{http.StatusUnauthorized, "missing Authorization header"}
}

func (s *Server) authenticateAPIKey(r *http.Request, header string) (Principal, context.Context, *authError) {
	if !strings.HasPrefix(header, "Bearer ") {
		return Principal{}, nil, &authError{http.StatusUnauthorized, "invalid Authorization format"}
	}

	token := strings.TrimPrefix(header, "Bearer ")
	keyHash := HashAPIKey(token)

	key, err := s.apikeys.GetByHash(r.Context(), keyHash)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			return Principal{}, nil, &authError{http.StatusUnauthorized, "invalid API key"}
		}
		return Principal{}, nil, &authError{http.StatusInternalServerError, err.Error()}
	}

	if !key.Active {
		return Principal{}, nil, &authError{http.StatusUnauthorized, "API key revoked"}
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
		return Principal{}, nil, &authError{http.StatusUnauthorized, "JWT authentication not configured"}
	}

	claims, err := s.tokenIssuer.VerifyAccessToken(token)
	if err != nil {
		return Principal{}, nil, &authError{http.StatusUnauthorized, "invalid or expired session"}
	}

	// Denylist consultation (immediate revocation on logout/forced signout) is
	// wired in alongside the session API in Task 7, once auth.Denylist exists
	// and Deps carries one.
	if s.denylist != nil {
		revoked, err := s.denylist.IsRevoked(r.Context(), claims.UserID)
		if err != nil {
			return Principal{}, nil, &authError{http.StatusInternalServerError, err.Error()}
		}
		if revoked {
			return Principal{}, nil, &authError{http.StatusUnauthorized, "session has been revoked"}
		}
	}

	principal := Principal{Role: domain.Role(claims.Role), UserID: claims.UserID}
	ctx := context.WithValue(r.Context(), ctxKeyPrincipal, principal)
	ctx = auth.WithRole(ctx, principal.Role)
	return principal, ctx, nil
}

func (s *Server) bootstrapTokenAllowed(ctx context.Context) bool {
	if s.apikeys == nil {
		return false
	}
	keys, err := s.apikeys.List(ctx)
	return err == nil && len(keys) == 0
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

func writeAuthError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
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
		writeError(w, http.StatusServiceUnavailable, "API key management not configured")
		return
	}

	var req createAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusServiceUnavailable, "API key management not configured")
		return
	}

	keys, err := s.apikeys.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if keys == nil {
		keys = []domain.APIKey{}
	}

	writeJSON(w, http.StatusOK, keys)
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	if s.apikeys == nil {
		writeError(w, http.StatusServiceUnavailable, "API key management not configured")
		return
	}

	id := r.PathValue("id")
	if err := s.apikeys.Revoke(r.Context(), id); err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
