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

func testServerWithAuth() *Server {
	return New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Scoring:      &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Monitoring:   &engine.MockMonitoringEngine{},
		Screening:    &engine.MockScreeningEngine{},
		Backtest:     &engine.MockBacktestEngine{},
		Audit:        store.NewMemoryAuditRepo(),
		Cases:        store.NewMemoryCaseRepo(),
		APIKeys:      store.NewMemoryAPIKeyRepo(),
	})
}

func createAPIKey(t *testing.T, s *Server, name string, role domain.Role) string {
	t.Helper()
	body := `{"name":"` + name + `","role":"` + string(role) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/apikeys", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create API key failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp createAPIKeyResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp.Key
}

func TestAuthNoHeader(t *testing.T) {
	s := testServerWithAuth()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthInvalidKey(t *testing.T) {
	s := testServerWithAuth()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	req.Header.Set("Authorization", "Bearer invalid-key")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthValidKey(t *testing.T) {
	s := testServerWithAuth()
	key := createAPIKey(t, s, "test-key", domain.RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAuthHealthzNoAuth(t *testing.T) {
	s := testServerWithAuth()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuthViewerCannotWrite(t *testing.T) {
	s := testServerWithAuth()
	key := createAPIKey(t, s, "viewer-key", domain.RoleViewer)

	body := `{"external_id":"AUTH001","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestAuthAnalystCanWrite(t *testing.T) {
	s := testServerWithAuth()
	key := createAPIKey(t, s, "analyst-key", domain.RoleAnalyst)

	body := `{"external_id":"AUTH002","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestAuthRevokedKey(t *testing.T) {
	s := testServerWithAuth()
	key := createAPIKey(t, s, "revoke-test", domain.RoleAdmin)

	// List keys to get the ID
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/apikeys", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var keys []domain.APIKey
	json.NewDecoder(rec.Body).Decode(&keys)

	if len(keys) == 0 {
		t.Fatal("expected at least 1 key")
	}

	// Revoke
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/apikeys/"+keys[0].ID, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Try using revoked key
	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
