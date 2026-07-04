package server

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

type auditResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *auditResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.audit == nil || r.URL.Path == "/healthz" || r.URL.Path == "/healthz/live" || r.URL.Path == "/healthz/ready" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		aw := &auditResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(aw, r)

		if r.Method == http.MethodGet {
			return
		}

		action := resolveAction(r.Method, r.URL.Path)
		resourceType, resourceID := resolveResource(r.URL.Path)

		entry := &domain.AuditEntry{
			UserID:       resolveAuditUserID(r),
			Action:       action,
			ResourceType: resourceType,
			ResourceID:   resourceID,
			IPAddress:    extractIP(r),
			UserAgent:    r.UserAgent(),
			CreatedAt:    time.Now(),
		}

		if err := s.audit.Create(r.Context(), entry); err != nil {
			slog.ErrorContext(r.Context(), "audit write error", "error", err)
		}
	})
}

func resolveAction(method, path string) string {
	switch method {
	case http.MethodPost:
		if strings.Contains(path, "/score") {
			return "score_customer"
		}
		if strings.Contains(path, "/screen") {
			return "screen_customer"
		}
		if strings.Contains(path, "/backtest") {
			return "run_backtest"
		}
		if strings.Contains(path, "/reports/str") {
			return "create_str"
		}
		return "create"
	case http.MethodPut:
		return "update"
	case http.MethodPatch:
		return "update_status"
	case http.MethodDelete:
		return "delete"
	default:
		return method
	}
}

func resolveResource(path string) (string, string) {
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1/"), "/")
	if len(parts) == 0 {
		return "unknown", ""
	}

	resourceType := parts[0]
	resourceID := ""

	if len(parts) >= 2 {
		resourceID = parts[1]
	}

	return resourceType, resourceID
}

func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit not configured")
		return
	}

	resourceType := r.URL.Query().Get("resource_type")
	resourceID := r.URL.Query().Get("resource_id")
	limit := 50

	entries, err := s.audit.List(r.Context(), resourceType, resourceID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entries == nil {
		entries = []domain.AuditEntry{}
	}

	writeJSON(w, http.StatusOK, entries)
}

func resolveAuditUserID(r *http.Request) string {
	if principal, ok := r.Context().Value(ctxKeyPrincipal).(Principal); ok {
		if principal.UserID != "" {
			return principal.UserID
		}
		if principal.APIKeyID != "" {
			return "apikey:" + principal.APIKeyID
		}
	}
	if key, ok := r.Context().Value(ctxKeyAPIKey).(*domain.APIKey); ok && key != nil {
		return "apikey:" + key.ID
	}
	return "anonymous"
}

func extractIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
