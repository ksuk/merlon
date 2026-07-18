package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
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

		// GETs are not recorded, except the audit export endpoint itself
		// (ALD-006: the export operation must be traceable even though it
		// is a read).
		if r.Method == http.MethodGet && r.URL.Path != "/api/v1/audit/export" {
			return
		}

		action := resolveAction(r.Method, r.URL.Path)
		resourceType, resourceID := resolveResource(r.URL.Path)
		sink.set("http_status", strconv.Itoa(aw.statusCode))
		switch {
		case aw.statusCode >= http.StatusInternalServerError:
			sink.set("outcome", "failed")
		case aw.statusCode >= http.StatusBadRequest:
			sink.set("outcome", "denied")
		default:
			sink.set("outcome", "success")
		}

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
	case http.MethodGet:
		if path == "/api/v1/audit/export" {
			return "export_audit_logs"
		}
		return method
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
		if strings.Contains(path, "/admin/retention-policies/") {
			return "update_retention_policy"
		}
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

	// admin/retention-policies/{category} likewise nests one level deeper
	// than the generic admin/{resource} shape (ALD-002: resource_type should
	// read "retention_policy", not "admin").
	if len(parts) >= 3 && parts[0] == "admin" && parts[1] == "retention-policies" {
		return "retention_policy", parts[2]
	}

	resourceType := parts[0]
	resourceID := ""

	if len(parts) >= 2 {
		resourceID = parts[1]
	}

	return resourceType, resourceID
}

func auditCursor(e domain.AuditEntry) Cursor {
	return Cursor{CreatedAt: e.CreatedAt, ID: strconv.FormatInt(e.ID, 10)}
}

// parseAuditListFilter reads ALD-001's filter axes (period, actor, resource,
// operation category) plus pagination from the request's query string.
// fetchLimit is pageReq.Limit+1 (the BuildPaginationMeta lookahead
// convention); the caller trims it back down to pageReq.Limit before
// responding.
func parseAuditListFilter(r *http.Request) (domain.AuditListFilter, PageRequest, error) {
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		return domain.AuditListFilter{}, PageRequest{}, err
	}

	q := r.URL.Query()
	filter := domain.AuditListFilter{
		ResourceType:   q.Get("resource_type"),
		ResourceID:     q.Get("resource_id"),
		UserID:         q.Get("user_id"),
		ActionCategory: q.Get("action_category"),
		Cursor:         toDomainCursor(pageReq.Cursor),
		Limit:          pageReq.Limit + 1,
	}

	if raw := q.Get("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return domain.AuditListFilter{}, PageRequest{}, fmt.Errorf("invalid since: %w", err)
		}
		filter.Since = &t
	}
	if raw := q.Get("until"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return domain.AuditListFilter{}, PageRequest{}, fmt.Errorf("invalid until: %w", err)
		}
		filter.Until = &t
	}

	return filter, pageReq, nil
}

// handleListAuditLogs serves ALD-001/002: filtered, paginated audit log
// listing. Routed behind auth.RequirePermission(auth.PermAuditRead)
// (ALD-005).
func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "audit not configured")
		return
	}

	filter, pageReq, err := parseAuditListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	entries, err := s.audit.List(r.Context(), filter)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	page, meta := BuildPaginationMeta(entries, pageReq.Limit, auditCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

// handleExportAuditLogs serves ALD-004 (CSV/JSON export preserving the same
// filter as the listing endpoint) and records its own invocation to the
// audit log (ALD-006). Routed behind auth.RequirePermission(auth.PermAuditRead)
// (ALD-005).
func (s *Server) handleExportAuditLogs(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit not configured")
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" {
		writeError(w, http.StatusBadRequest, "unsupported format: "+format)
		return
	}

	// Export has no natural page size (it must return every row matching
	// the filter), so Limit is left at zero — meaning "no limit" to both
	// repository implementations, unlike the paginated listing endpoint.
	q := r.URL.Query()
	filter := domain.AuditListFilter{
		ResourceType:   q.Get("resource_type"),
		ResourceID:     q.Get("resource_id"),
		UserID:         q.Get("user_id"),
		ActionCategory: q.Get("action_category"),
	}
	if raw := q.Get("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since: "+err.Error())
			return
		}
		filter.Since = &t
	}
	if raw := q.Get("until"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid until: "+err.Error())
			return
		}
		filter.Until = &t
	}

	entries, err := s.audit.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	switch format {
	case "csv":
		writeAuditCSV(w, entries)
	case "json":
		writeJSON(w, http.StatusOK, entries)
	}

	setAuditDetail(r, "export_format", format)
	setAuditDetail(r, "export_count", strconv.Itoa(len(entries)))
}

func writeAuditCSV(w http.ResponseWriter, entries []domain.AuditEntry) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=audit_logs.csv")

	writer := csv.NewWriter(w)
	defer writer.Flush()

	writer.Write([]string{"id", "created_at", "user_id", "action", "resource_type", "resource_id", "details", "ip_address", "user_agent"})
	for _, e := range entries {
		details, _ := json.Marshal(e.Details)
		writer.Write([]string{
			strconv.FormatInt(e.ID, 10),
			e.CreatedAt.Format(time.RFC3339),
			sanitizeCSVCell(e.UserID),
			sanitizeCSVCell(e.Action),
			sanitizeCSVCell(e.ResourceType),
			sanitizeCSVCell(e.ResourceID),
			sanitizeCSVCell(string(details)),
			sanitizeCSVCell(e.IPAddress),
			sanitizeCSVCell(e.UserAgent),
		})
	}
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
