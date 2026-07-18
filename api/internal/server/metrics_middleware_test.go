package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// histogramSampleCount returns the sample count of the
// merlon_api_request_duration_seconds series matching the given label
// values, or 0 if no such series exists yet. Tests compare before/after
// deltas rather than absolute series counts because the underlying
// collector is a shared, process-global registry other tests in this
// package also record into.
func histogramSampleCount(t *testing.T, method, path, status string) uint64 {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	for _, f := range families {
		if f.GetName() != "merlon_api_request_duration_seconds" {
			continue
		}
		for _, m := range f.Metric {
			if matchesLabels(m, map[string]string{"method": method, "path": path, "status": status}) {
				return m.Histogram.GetSampleCount()
			}
		}
	}
	return 0
}

func matchesLabels(m *dto.Metric, want map[string]string) bool {
	got := make(map[string]string, len(m.Label))
	for _, l := range m.Label {
		got[l.GetName()] = l.GetValue()
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

// TestMetricsMiddlewareRecordsDuration verifies that serving a request
// through the full middleware chain records a sample in
// merlon_api_request_duration_seconds for that request's matched route
// (Task 2, the operational design §4.4).
func TestMetricsMiddlewareRecordsDuration(t *testing.T) {
	s := testServer()

	before := histogramSampleCount(t, http.MethodGet, "/api/v1/customers", "200")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	after := histogramSampleCount(t, http.MethodGet, "/api/v1/customers", "200")
	if after != before+1 {
		t.Errorf("sample count = %d, want %d (before %d + this request)", after, before+1, before)
	}
}

// TestMetricsMiddlewarePathLabelUsesPattern verifies the path label is the
// matched route pattern (e.g. "/api/v1/alerts/{id}"), not the literal
// request path with real ID values, so label cardinality stays bounded.
func TestMetricsMiddlewarePathLabelUsesPattern(t *testing.T) {
	s := testServer()

	const pattern = "/api/v1/alerts/{id}"
	before := histogramSampleCount(t, http.MethodGet, pattern, "404")

	for _, id := range []string{"alert-id-aaaa", "alert-id-bbbb"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/"+id, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
	}

	after := histogramSampleCount(t, http.MethodGet, pattern, "404")
	if after != before+2 {
		t.Errorf("sample count for pattern series = %d, want %d (before %d + 2 requests sharing one series)", after, before+2, before)
	}

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() != "merlon_api_request_duration_seconds" {
			continue
		}
		for _, m := range f.Metric {
			for _, l := range m.Label {
				if l.GetName() != "path" {
					continue
				}
				if l.GetValue() == "/api/v1/alerts/alert-id-aaaa" || l.GetValue() == "/api/v1/alerts/alert-id-bbbb" {
					t.Errorf("path label leaked a literal ID: %q, want pattern %q", l.GetValue(), pattern)
				}
			}
		}
	}
}
