package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/metrics"
)

// metricsMiddleware records merlon_api_request_duration_seconds (OPS-003,
// the operational design §4.4) for every request. The path label uses the matched
// ServeMux pattern (e.g. "/api/v1/alerts/{id}") rather than the literal
// request path, so real ID values never appear in a label and cardinality
// stays bounded by the number of registered routes.
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		mw := &auditResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		_, matchedPattern := s.mux.Handler(r)

		next.ServeHTTP(mw, r)

		path := patternPath(matchedPattern)
		metrics.APIRequestDuration.
			WithLabelValues(r.Method, path, strconv.Itoa(mw.statusCode)).
			Observe(time.Since(start).Seconds())
	})
}

// patternPath strips the leading "METHOD " prefix ServeMux includes in
// r.Pattern for method-specific routes, e.g. "GET /api/v1/alerts/{id}" ->
// "/api/v1/alerts/{id}". Falls back to "unmatched" if no route matched
// (r.Pattern is empty, e.g. request rejected before reaching the mux).
func patternPath(pattern string) string {
	if pattern == "" {
		return "unmatched"
	}
	if idx := strings.IndexByte(pattern, ' '); idx != -1 {
		return pattern[idx+1:]
	}
	return pattern
}
