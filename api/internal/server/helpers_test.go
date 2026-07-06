package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/apierr"
)

func TestWriteErrorCodeIncludesBothFields(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErrorCode(rec, http.StatusNotFound, apierr.CodeNotFound, "customer not found")

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["error"] != "customer not found" {
		t.Errorf("error = %q, want %q", body["error"], "customer not found")
	}
	if body["error_code"] != string(apierr.CodeNotFound) {
		t.Errorf("error_code = %q, want %q", body["error_code"], apierr.CodeNotFound)
	}
}

func TestWriteErrorOmitsErrorCode(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusBadRequest, "legacy caller without a code")

	var body map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if _, ok := body["error_code"]; ok {
		t.Errorf("expected error_code to be omitted, got %s", body["error_code"])
	}
}

func TestErrorCodeStableAcrossMessageWording(t *testing.T) {
	rec1 := httptest.NewRecorder()
	writeErrorCode(rec1, http.StatusNotFound, apierr.CodeNotFound, "customer not found")

	rec2 := httptest.NewRecorder()
	writeErrorCode(rec2, http.StatusNotFound, apierr.CodeNotFound, "no such customer exists")

	var body1, body2 map[string]string
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

			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body["error_code"] != string(tc.wantCode) {
				t.Errorf("error_code = %q, want %q", body["error_code"], tc.wantCode)
			}
		})
	}
}
