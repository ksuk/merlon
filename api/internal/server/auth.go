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

	"github.com/merlon-aml/merlon/api/internal/domain"
)

type contextKey string

const (
	ctxKeyAPIKey contextKey = "api_key"
)

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

		header := r.Header.Get("Authorization")
		if header == "" {
			writeAuthError(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}

		if !strings.HasPrefix(header, "Bearer ") {
			writeAuthError(w, http.StatusUnauthorized, "invalid Authorization format")
			return
		}

		token := strings.TrimPrefix(header, "Bearer ")
		keyHash := HashAPIKey(token)

		key, err := s.apikeys.GetByHash(r.Context(), keyHash)
		if err != nil {
			var nf *domain.ErrNotFound
			if errors.As(err, &nf) {
				writeAuthError(w, http.StatusUnauthorized, "invalid API key")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if !key.Active {
			writeAuthError(w, http.StatusUnauthorized, "API key revoked")
			return
		}

		// Admin routes require admin role
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/") && key.Role != domain.RoleAdmin {
			writeAuthError(w, http.StatusForbidden, "admin role required")
			return
		}

		if !hasPermission(key.Role, r.Method, r.URL.Path) {
			writeAuthError(w, http.StatusForbidden, "insufficient permissions")
			return
		}

		now := time.Now()
		key.LastUsed = &now

		ctx := context.WithValue(r.Context(), ctxKeyAPIKey, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
