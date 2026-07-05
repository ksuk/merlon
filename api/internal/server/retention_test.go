package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/engine"
	"github.com/merlon-aml/merlon/api/internal/store"
)

func testServerWithRetention() *Server {
	return New(":0", Deps{
		Customers:      store.NewMemoryCustomerRepo(),
		Transactions:   store.NewMemoryTransactionRepo(),
		Alerts:         store.NewMemoryAlertRepo(),
		Scoring:        &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Monitoring:     &engine.MockMonitoringEngine{},
		Screening:      &engine.MockScreeningEngine{},
		Backtest:       &engine.MockBacktestEngine{},
		Audit:          store.NewMemoryAuditRepo(),
		Cases:          store.NewMemoryCaseRepo(),
		APIKeys:        store.NewMemoryAPIKeyRepo(),
		BootstrapToken: testBootstrapToken,
		Retention:      store.NewMemoryRetentionRepo(),
	})
}

func TestRetentionPolicyListReturnsAllCategories(t *testing.T) {
	s := testServerWithRetention()
	adminKey := createAPIKey(t, s, "retention-admin", domain.RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/retention-policies", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var policies []domain.RetentionPolicy
	if err := json.NewDecoder(rec.Body).Decode(&policies); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(policies) != 5 {
		t.Fatalf("len(policies) = %d, want 5", len(policies))
	}

	seen := map[string]bool{}
	for _, p := range policies {
		seen[p.DataCategory] = true
	}
	for _, want := range []string{"customer_data", "transaction_data", "alert_case_data", "cdd_score_history", "audit_log"} {
		if !seen[want] {
			t.Errorf("missing category %q in list response", want)
		}
	}
}

func TestRetentionPolicyUpdateExtendSucceeds(t *testing.T) {
	s := testServerWithRetention()
	adminKey := createAPIKey(t, s, "retention-admin", domain.RoleAdmin)

	body := `{"retention_days":3000}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/retention-policies/customer_data", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated domain.RetentionPolicy
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.RetentionDays != 3000 {
		t.Errorf("RetentionDays = %d, want 3000", updated.RetentionDays)
	}
}

func TestRetentionPolicyUpdateShortenReturns400(t *testing.T) {
	s := testServerWithRetention()
	adminKey := createAPIKey(t, s, "retention-admin", domain.RoleAdmin)

	body := `{"retention_days":100}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/retention-policies/customer_data", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestRetentionPolicyUpdateRequiresAdmin(t *testing.T) {
	s := testServerWithRetention()
	analystKey := createAPIKey(t, s, "retention-analyst", domain.RoleAnalyst)

	body := `{"retention_days":3000}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/retention-policies/customer_data", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+analystKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

// TestRetentionPolicyUpdateRecordsAuditLog verifies update_retention_policy /
// retention_policy show up in the audit log (Auditability First).
func TestRetentionPolicyUpdateRecordsAuditLog(t *testing.T) {
	s := testServerWithRetention()
	adminKey := createAPIKey(t, s, "retention-admin", domain.RoleAdmin)

	body := `{"retention_days":3000}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/retention-policies/customer_data", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit?resource_type=retention_policy", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	entries, _ := decodeListResponse[domain.AuditEntry](t, rec.Body)
	if len(entries) < 1 {
		t.Fatalf("expected at least 1 audit entry, got %d", len(entries))
	}
	if entries[0].Action != "update_retention_policy" {
		t.Errorf("action = %q, want %q", entries[0].Action, "update_retention_policy")
	}
	if entries[0].ResourceID != "customer_data" {
		t.Errorf("resource_id = %q, want %q", entries[0].ResourceID, "customer_data")
	}
}
