package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testCommit = "0123456789abcdef0123456789abcdef01234567"

func TestFetchTargetStatusRequiresMatchingExactCommit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/status" || r.URL.Query().Get("refresh") != "true" {
			t.Fatalf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer synthetic-token" {
			t.Fatalf("authorization header = %q", got)
		}
		_ = json.NewEncoder(w).Encode(targetStatus{
			Version: "v0.0.1-test", Commit: testCommit, BuiltAt: "2026-08-30T00:00:00Z",
			AuthMode: "enabled", BaseCurrency: "JPY",
		})
	}))
	defer server.Close()

	baseURL, err := validateLoopbackBaseURL(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fetchTargetStatus(context.Background(), newLoopbackHTTPClient(time.Second), baseURL, "synthetic-token", testCommit)
	if err != nil {
		t.Fatal(err)
	}
	if got.Commit != testCommit || got.Version != "v0.0.1-test" {
		t.Fatalf("status = %+v", got)
	}

	if _, err := fetchTargetStatus(context.Background(), newLoopbackHTTPClient(time.Second), baseURL, "synthetic-token", strings.Repeat("f", 40)); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched commit error = %v", err)
	}
}

func TestExecuteCreatesSyntheticCustomersAndMeasuresTransactions(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	createdCustomers := 0
	warmupTransactions := 0
	measuredTransactions := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/status":
			_ = json.NewEncoder(w).Encode(targetStatus{Version: "test", Commit: testCommit, AuthMode: "disabled", BaseCurrency: "JPY"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/customers":
			var payload syntheticCustomerRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(payload.ExternalID, "MERLON-PERF-test-run-C") || payload.Attributes["name"] != syntheticCustomerName {
				t.Fatalf("customer payload = %+v", payload)
			}
			mu.Lock()
			createdCustomers++
			id := createdCustomers
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "customer-" + string(rune('0'+id))})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/transactions":
			var payload syntheticTransactionRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			if strings.Contains(payload.ExternalID, "-W") {
				warmupTransactions++
			} else if strings.Contains(payload.ExternalID, "-T") {
				measuredTransactions++
			} else {
				t.Errorf("unexpected external_id %q", payload.ExternalID)
			}
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"transaction"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	report, err := execute(context.Background(), options{
		baseURL: server.URL, expectedCommit: testCommit, requests: 8,
		concurrency: 2, customers: 2, warmup: 4, requestTimeout: time.Second,
		runID: "test-run",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if createdCustomers != 2 || warmupTransactions != 4 || measuredTransactions != 8 {
		t.Fatalf("requests = customers:%d warmup:%d measured:%d", createdCustomers, warmupTransactions, measuredTransactions)
	}
	if report.Target.Commit != testCommit || report.Results.Attempted != 8 || report.Results.Failed != 0 {
		t.Fatalf("report = %+v", report)
	}
	if report.Measurement.DataSource != "built-in synthetic performance fixtures" {
		t.Fatalf("data source = %q", report.Measurement.DataSource)
	}
}

func TestParseOptionsRejectsMissingOrNonExactCommit(t *testing.T) {
	t.Parallel()

	if _, err := parseOptions([]string{"--base-url", "http://127.0.0.1:8080"}); err == nil || !strings.Contains(err.Error(), "expected-commit") {
		t.Fatalf("missing commit error = %v", err)
	}
	if _, err := parseOptions([]string{"--base-url", "http://127.0.0.1:8080", "--expected-commit", "0123456"}); err == nil || !strings.Contains(err.Error(), "40-character") {
		t.Fatalf("short commit error = %v", err)
	}
}
