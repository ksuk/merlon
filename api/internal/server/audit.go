package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
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

type auditDetailsKey struct{}

// auditDetailsSink is a mutable container injected into the request context
// by auditMiddleware before calling the handler. Because it is shared by
// pointer, a handler can populate it (via setAuditDetail) and the middleware
// will see those values after ServeHTTP returns, letting specific handlers
// (e.g. handleUpdateRule's before/after diff, ALD-003) attach structured
// details to the generic audit entry without every handler needing its own
// audit-writing logic.
type auditDetailsSink struct {
	mu      sync.Mutex
	details map[string]string
}

func (s *auditDetailsSink) set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.details == nil {
		s.details = make(map[string]string)
	}
	s.details[key] = value
}

func (s *auditDetailsSink) snapshot() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.details) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.details))
	for k, v := range s.details {
		out[k] = v
	}
	return out
}

// setAuditDetail attaches a key/value pair to the audit log entry that will
// be written for this request, if auditMiddleware is active. No-op otherwise.
func setAuditDetail(r *http.Request, key, value string) {
	if sink, ok := r.Context().Value(auditDetailsKey{}).(*auditDetailsSink); ok {
		sink.set(key, value)
	}
}

func (s *Server) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.audit == nil || r.URL.Path == "/healthz" || r.URL.Path == "/healthz/live" || r.URL.Path == "/healthz/ready" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		sink := &auditDetailsSink{}
		ctx := context.WithValue(r.Context(), auditDetailsKey{}, sink)

		aw := &auditResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(aw, r.WithContext(ctx))

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
			Details:      sink.snapshot(),
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
		if strings.Contains(path, "/rules/") && strings.HasSuffix(path, "/activate") {
			return "activate_rule"
		}
		if strings.Contains(path, "/rules/") && strings.HasSuffix(path, "/deactivate") {
			return "deactivate_rule"
		}
		if path == "/api/v1/rules/import" {
			return "import_rules"
		}
		if strings.Contains(path, "/whitelist/") && strings.HasSuffix(path, "/approve") {
			return "approve_whitelist_entry"
		}
		if strings.Contains(path, "/whitelist/") && strings.HasSuffix(path, "/revoke") {
			return "revoke_whitelist_entry"
		}
		if strings.Contains(path, "/whitelist/") && strings.HasSuffix(path, "/reviews") {
			return "review_whitelist_entry"
		}
		if strings.Contains(path, "/webhooks/dlq/") && strings.HasSuffix(path, "/reprocess") {
			return "reprocess_dlq_entry"
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

	// webhooks/dlq/{id}/reprocess nests the id one level deeper than the
	// generic resource/{id} shape the fallback below assumes.
	if len(parts) >= 3 && parts[0] == "webhooks" && parts[1] == "dlq" {
		return "webhook_dlq", parts[2]
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
