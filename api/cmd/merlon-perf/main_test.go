package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestValidateLoopbackBaseURL(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"http://127.0.0.1:8080",
		"http://127.0.0.42",
		"http://[::1]:8080",
		"https://localhost:8443",
		"https://LOCALHOST.:8443/",
	} {
		raw := raw
		t.Run("accept_"+strings.NewReplacer(":", "_", "/", "_").Replace(raw), func(t *testing.T) {
			t.Parallel()
			got, err := validateLoopbackBaseURL(raw)
			if err != nil {
				t.Fatalf("validateLoopbackBaseURL(%q): %v", raw, err)
			}
			if got.String() == "" {
				t.Fatal("normalized URL is empty")
			}
		})
	}

	for _, raw := range []string{
		"https://example.com",
		"http://192.0.2.10:8080",
		"http://localhost.example.com:8080",
		"ftp://127.0.0.1:8080",
		"http://user:secret@127.0.0.1:8080",
		"http://127.0.0.1:8080/api/v1",
		"http://127.0.0.1:8080?target=external",
		"127.0.0.1:8080",
	} {
		raw := raw
		t.Run("reject_"+strings.NewReplacer(":", "_", "/", "_").Replace(raw), func(t *testing.T) {
			t.Parallel()
			if _, err := validateLoopbackBaseURL(raw); err == nil {
				t.Fatalf("validateLoopbackBaseURL(%q) accepted a non-canonical or unsafe destination", raw)
			}
		})
	}
}

func TestLoopbackClientRejectsExternalRedirect(t *testing.T) {
	t.Parallel()

	client := newLoopbackHTTPClient(time.Second)
	redirect, err := http.NewRequest(http.MethodGet, "https://example.com/escaped", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(redirect, nil); err == nil {
		t.Fatal("external redirect was accepted")
	}
}

func TestSummarizeResultsRecordsLatencyThroughputAndErrors(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	completed := started.Add(2 * time.Second)
	results := []requestResult{
		{duration: 40 * time.Millisecond, statusCode: http.StatusCreated},
		{duration: 10 * time.Millisecond, statusCode: http.StatusCreated},
		{duration: 30 * time.Millisecond, statusCode: http.StatusCreated},
		{duration: 20 * time.Millisecond, statusCode: http.StatusCreated},
		{duration: 50 * time.Millisecond, statusCode: http.StatusInternalServerError},
		{duration: 60 * time.Millisecond, transportError: true},
	}

	got := summarizeResults(started, completed, results)
	if got.Attempted != 6 || got.Succeeded != 4 || got.Failed != 2 {
		t.Fatalf("counts = attempted:%d succeeded:%d failed:%d", got.Attempted, got.Succeeded, got.Failed)
	}
	if got.StatusCodes[http.StatusCreated] != 4 || got.StatusCodes[http.StatusInternalServerError] != 1 {
		t.Fatalf("status codes = %#v", got.StatusCodes)
	}
	if got.TransportErrors != 1 {
		t.Fatalf("transport errors = %d, want 1", got.TransportErrors)
	}
	if got.ErrorRatePercent != 33.333 {
		t.Fatalf("error rate = %v, want 33.333", got.ErrorRatePercent)
	}
	if got.SuccessfulThroughputRPS != 2 {
		t.Fatalf("throughput = %v, want 2", got.SuccessfulThroughputRPS)
	}
	if got.SuccessfulLatencyMS.Min != 10 || got.SuccessfulLatencyMS.P50 != 20 || got.SuccessfulLatencyMS.P95 != 40 || got.SuccessfulLatencyMS.P99 != 40 || got.SuccessfulLatencyMS.Max != 40 {
		t.Fatalf("successful latency = %+v", got.SuccessfulLatencyMS)
	}
}

func TestSummarizeResultsHandlesNoSuccessfulRequests(t *testing.T) {
	t.Parallel()

	started := time.Now().UTC()
	got := summarizeResults(started, started, []requestResult{{statusCode: http.StatusBadGateway}})
	if got.SuccessfulThroughputRPS != 0 || got.SuccessfulLatencyMS != (latencySummary{}) {
		t.Fatalf("summary without successful requests = %+v", got)
	}
}

func TestSummarizeResultsRequiresCreatedStatus(t *testing.T) {
	t.Parallel()

	started := time.Now().UTC()
	got := summarizeResults(started, started.Add(time.Second), []requestResult{
		{duration: time.Millisecond, statusCode: http.StatusOK},
		{duration: time.Millisecond, statusCode: http.StatusNoContent},
	})
	if got.Succeeded != 0 || got.Failed != 2 || got.ErrorRatePercent != 100 {
		t.Fatalf("non-created 2xx summary = %+v", got)
	}
}

func TestValidateHarnessCommitRequiresExactMatch(t *testing.T) {
	t.Parallel()

	expected := "0123456789abcdef0123456789abcdef01234567"
	for _, harnessCommit := range []string{"", "0123456789ab", strings.Repeat("f", 40)} {
		if err := validateHarnessCommit(harnessCommit, expected); err == nil {
			t.Fatalf("validateHarnessCommit(%q) succeeded", harnessCommit)
		}
	}
	if err := validateHarnessCommit(strings.ToUpper(expected), expected); err != nil {
		t.Fatalf("matching exact commit was rejected: %v", err)
	}
}
