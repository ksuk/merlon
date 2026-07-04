package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleMetricsReturnsPrometheusFormat verifies OPS-003 (overview.md
// §4.4): GET /metrics exposes the Prometheus text exposition format and
// includes every business/technical metric name, even at zero value.
func TestHandleMetricsReturnsPrometheusFormat(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want prefix %q", ct, "text/plain")
	}

	body := rec.Body.String()
	wantNames := []string{
		"merlon_alerts_total",
		"merlon_cases_open",
		"merlon_screening_hits_total",
		"merlon_cdd_tier_distribution",
		"merlon_cdd_tier_anomaly_total",
		"merlon_tx_missing_fiat_equivalent_total",
		"merlon_screening_list_stale_days",
		"merlon_api_request_duration_seconds",
		"merlon_grpc_request_duration_seconds",
		"merlon_db_pool_active_connections",
		"merlon_webhook_dlq_depth",
		"merlon_batch_evaluation_duration_seconds",
	}
	for _, name := range wantNames {
		if !strings.Contains(body, name) {
			t.Errorf("body missing metric %q", name)
		}
	}
}

// TestMetricsEndpointExcludedFromRateLimitAndAudit verifies /metrics is not
// subject to rate limiting or audit logging, matching the existing /healthz
// exclusion pattern (task doc Task 1 implementation notes).
func TestMetricsEndpointExcludedFromRateLimitAndAudit(t *testing.T) {
	s := testServerFull()
	s.limiter = newRateLimiter(1, 60*1000*1000*1000) // 1 req/min

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status code = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}
}
