package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/apierr"
)

func TestWriteErrorCodeIncludesBothFields(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErrorCode(rec, http.StatusNotFound, apierr.CodeNotFound, "customer not found")

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["error"] != any("customer not found") {
		t.Errorf("error = %q, want %q", body["error"], "customer not found")
	}
	if body["error_code"] != any(string(apierr.CodeNotFound)) {
		t.Errorf("error_code = %q, want %q", body["error_code"], apierr.CodeNotFound)
	}
}

// TestEveryErrorCarriesAStableCode replaces the test that pinned the
// code-less writer. That writer is gone: account, retention, audit and
// customer-status handlers emitted forty responses without an error_code, so
// the UI could not classify them and fell back to showing raw server text.
func TestEveryErrorCarriesAStableCode(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErrorCode(rec, http.StatusBadRequest, apierr.CodeValidationFailed, "a caller with a code")

	var body map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := body["error_code"]; !ok {
		t.Error("error_code is absent; a client cannot branch on the message text")
	}
	if _, ok := body["retryable"]; !ok {
		t.Error("retryable is absent; the client cannot tell a permanent refusal from a transient one")
	}
}

func TestErrorCodeStableAcrossMessageWording(t *testing.T) {
	rec1 := httptest.NewRecorder()
	writeErrorCode(rec1, http.StatusNotFound, apierr.CodeNotFound, "customer not found")

	rec2 := httptest.NewRecorder()
	writeErrorCode(rec2, http.StatusNotFound, apierr.CodeNotFound, "no such customer exists")

	var body1, body2 map[string]any
	json.NewDecoder(rec1.Body).Decode(&body1)
	json.NewDecoder(rec2.Body).Decode(&body2)

	if body1["error_code"] != body2["error_code"] {
		t.Errorf("error_code changed with message wording: %q vs %q", body1["error_code"], body2["error_code"])
	}
	if body1["error"] == body2["error"] {
		t.Errorf("expected differing error message wording in this test setup")
	}
}

// TestExistingHandlersReturnErrorCode is a table-driven regression test
// (customer/transaction/alert/case 404 and 400 paths) confirming the
// error_code migration reaches real handlers end-to-end, not just the
// writeErrorCode helper in isolation.
func TestExistingHandlersReturnErrorCode(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantCode   apierr.Code
	}{
		{"customer not found", http.MethodGet, "/api/v1/customers/nonexistent", "", http.StatusNotFound, apierr.CodeNotFound},
		{"customer create missing external_id", http.MethodPost, "/api/v1/customers", `{"customer_type":"individual"}`, http.StatusBadRequest, apierr.CodeValidationFailed},
		{"transaction not found", http.MethodGet, "/api/v1/transactions/nonexistent", "", http.StatusNotFound, apierr.CodeNotFound},
		{"transaction create missing customer_id", http.MethodPost, "/api/v1/transactions", `{"external_id":"TX1","amount":100,"direction":"inbound"}`, http.StatusBadRequest, apierr.CodeValidationFailed},
		{"alert not found", http.MethodGet, "/api/v1/alerts/nonexistent", "", http.StatusNotFound, apierr.CodeNotFound},
		{"case not found", http.MethodGet, "/api/v1/cases/nonexistent", "", http.StatusNotFound, apierr.CodeNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testServerFull()

			var req *http.Request
			if tc.body == "" {
				req = httptest.NewRequest(tc.method, tc.path, nil)
			} else {
				req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			}
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}

			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body["error_code"] != any(string(tc.wantCode)) {
				t.Errorf("error_code = %q, want %q", body["error_code"], tc.wantCode)
			}
		})
	}
}

// TestErrorCarriesTheCorrelationIdentifier is what makes a support request
// actionable: the request ID existed only as a response header, so an operator
// reading a screenshot of a failure had nothing to quote.
func TestErrorCarriesTheCorrelationIdentifier(t *testing.T) {
	s := testServerFull()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/does-not-exist", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var body struct {
		Error     string `json:"error"`
		Code      string `json:"error_code"`
		RequestID string `json:"request_id"`
		Retryable bool   `json:"retryable"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (body: %s)", err, rec.Body.String())
	}

	if body.RequestID == "" {
		t.Error("request_id is absent from the error body")
	}
	if header := rec.Header().Get(requestIDHeader); header != body.RequestID {
		t.Errorf("request_id in the body (%q) does not match the header (%q); support would search for the wrong identifier", body.RequestID, header)
	}
	if body.Retryable {
		t.Error("a missing record was reported as retryable; repeating the request will not create it")
	}
}

// TestTransientFailureIsMarkedRetryable is the other half: a dependency that is
// unavailable now may not be in a moment, and the client needs to know which
// kind of failure it received.
func TestTransientFailureIsMarkedRetryable(t *testing.T) {
	if !apierr.Retryable(apierr.CodeServiceUnavailable) {
		t.Error("service_unavailable is not retryable")
	}
	if !apierr.Retryable(apierr.CodeRateLimited) {
		t.Error("rate_limited is not retryable")
	}
	if apierr.Retryable(apierr.CodeValidationFailed) {
		t.Error("validation_failed is retryable; the same input will fail the same way")
	}
	if apierr.Retryable(apierr.CodeForbidden) {
		t.Error("forbidden is retryable; the caller's role will not change by repeating")
	}
}
