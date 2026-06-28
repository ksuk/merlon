package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

func TestAuditMiddlewareRecordsWrite(t *testing.T) {
	s := testServerFull()

	body := `{"external_id":"AUD001","customer_type":"individual","country_code":"JP","product_types":["spot_trading"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	req.Header.Set("X-User-ID", "analyst01")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit?resource_type=customers", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var entries []domain.AuditEntry
	json.NewDecoder(rec.Body).Decode(&entries)

	if len(entries) < 1 {
		t.Fatalf("expected at least 1 audit entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Action != "create" {
		t.Errorf("action = %q, want %q", entry.Action, "create")
	}
	if entry.UserID != "analyst01" {
		t.Errorf("user_id = %q, want %q", entry.UserID, "analyst01")
	}
	if entry.ResourceType != "customers" {
		t.Errorf("resource_type = %q, want %q", entry.ResourceType, "customers")
	}
}

func TestAuditMiddlewareSkipsGET(t *testing.T) {
	s := testServerFull()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var entries []domain.AuditEntry
	json.NewDecoder(rec.Body).Decode(&entries)

	if len(entries) != 0 {
		t.Errorf("expected 0 audit entries for GET, got %d", len(entries))
	}
}

func TestAuditMiddlewareSkipsHealthz(t *testing.T) {
	s := testServerFull()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var entries []domain.AuditEntry
	json.NewDecoder(rec.Body).Decode(&entries)

	if len(entries) != 0 {
		t.Errorf("expected 0 audit entries, got %d", len(entries))
	}
}
