package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

const testBootstrapToken = "test-bootstrap-token"

func testServerWithAuth() *Server {
	alerts := store.NewMemoryAlertRepo()
	cases := store.NewMemoryCaseRepo()
	return New(":0", Deps{
		Customers:          store.NewMemoryCustomerRepo(),
		Transactions:       store.NewMemoryTransactionRepo(),
		Alerts:             alerts,
		Scoring:            &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Monitoring:         &engine.MockMonitoringEngine{},
		Screening:          &engine.MockScreeningEngine{},
		Backtest:           &engine.MockBacktestEngine{},
		Audit:              store.NewMemoryAuditRepo(),
		Cases:              cases,
		CaseAlertLifecycle: store.NewMemoryCaseAlertLifecycleRepo(cases, alerts),
		APIKeys:            store.NewMemoryAPIKeyRepo(),
		BootstrapToken:     testBootstrapToken,
	})
}

func testServerWithJWT(t *testing.T) (*Server, *auth.TokenIssuer) {
	t.Helper()
	issuer, err := auth.NewHS256Issuer("test-only-secret-not-for-production")
	if err != nil {
		t.Fatalf("NewHS256Issuer: %v", err)
	}

	alerts := store.NewMemoryAlertRepo()
	cases := store.NewMemoryCaseRepo()
	s := New(":0", Deps{
		Customers:          store.NewMemoryCustomerRepo(),
		Transactions:       store.NewMemoryTransactionRepo(),
		Alerts:             alerts,
		Scoring:            &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Monitoring:         &engine.MockMonitoringEngine{},
		Screening:          &engine.MockScreeningEngine{},
		Backtest:           &engine.MockBacktestEngine{},
		Audit:              store.NewMemoryAuditRepo(),
		Cases:              cases,
		CaseAlertLifecycle: store.NewMemoryCaseAlertLifecycleRepo(cases, alerts),
		APIKeys:            store.NewMemoryAPIKeyRepo(),
		BootstrapToken:     testBootstrapToken,
		TokenIssuer:        issuer,
	})
	return s, issuer
}

func createAPIKey(t *testing.T, s *Server, name string, role domain.Role) string {
	t.Helper()
	body := `{"name":"` + name + `","role":"` + string(role) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/apikeys", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testBootstrapToken)
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
	assertErrorCode(t, rec, "unauthorized")
}

// assertErrorCode decodes rec.Body as {"error": ..., "error_code": ...} and
// fails the test if error_code does not match want (Contract Stability:
// clients must be able to branch on error_code for every error response).
func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, rec.Body.String())
	}
	if body["error_code"] != want {
		t.Errorf("error_code = %q, want %q (body: %s)", body["error_code"], want, rec.Body.String())
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
	assertErrorCode(t, rec, "unauthorized")
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
	assertErrorCode(t, rec, "forbidden")
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

	// List keys to get the ID (requires admin key)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/apikeys", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var keys []domain.APIKey
	json.NewDecoder(rec.Body).Decode(&keys)

	if len(keys) == 0 {
		t.Fatal("expected at least 1 key")
	}

	// Revoke (requires admin key)
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/apikeys/"+keys[0].ID, nil)
	req.Header.Set("Authorization", "Bearer "+key)
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

func TestAdminCreateRequiresAuth(t *testing.T) {
	s := testServerWithAuth()
	body := `{"name":"unauth-key","role":"admin"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/apikeys", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminListRequiresAuth(t *testing.T) {
	s := testServerWithAuth()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/apikeys", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminRevokeRequiresAuth(t *testing.T) {
	s := testServerWithAuth()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/apikeys/some-id", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminRequiresAdminRole(t *testing.T) {
	s := testServerWithAuth()
	viewerKey := createAPIKey(t, s, "viewer-key", domain.RoleViewer)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/apikeys", nil)
	req.Header.Set("Authorization", "Bearer "+viewerKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	assertErrorCode(t, rec, "forbidden")
}

func TestBootstrapTokenCreateKey(t *testing.T) {
	s := testServerWithAuth()
	body := `{"name":"bootstrap-key","role":"admin"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/apikeys", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testBootstrapToken)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestBootstrapTokenCannotCreateSecondKey(t *testing.T) {
	s := testServerWithAuth()

	createAPIKey(t, s, "first-key", domain.RoleAdmin)

	body := `{"name":"second-bootstrap-key","role":"admin"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/apikeys", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testBootstrapToken)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	assertErrorCode(t, rec, "unauthorized")
}

func TestBootstrapTokenCannotListKeys(t *testing.T) {
	s := testServerWithAuth()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/apikeys", nil)
	req.Header.Set("Authorization", "Bearer "+testBootstrapToken)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("bootstrap token should not be able to list keys")
	}
}

func TestAdminCreateWithAdminKey(t *testing.T) {
	s := testServerWithAuth()
	adminKey := createAPIKey(t, s, "admin-key", domain.RoleAdmin)

	body := `{"name":"second-key","role":"viewer"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/apikeys", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

// TestAuthMiddleware_AcceptsAPIKeyAndJWT verifies backward compatibility
// (acceptance criterion 3): the same endpoint must accept either an API key
// or a JWT session cookie.
func TestAuthMiddleware_AcceptsAPIKeyAndJWT(t *testing.T) {
	s, issuer := testServerWithJWT(t)
	apiKey := createAPIKey(t, s, "api-key", domain.RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("API key path: status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	token, err := issuer.IssueAccessToken("user-1", string(domain.RoleAdmin), "jti-1")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: token})
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("JWT cookie path: status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// TestAuthMiddleware_AnalystForbiddenOnWhitelistApprove verifies acceptance
// criterion 4: RequirePermission(PermWhitelistApprove) rejects an Analyst
// with 403 on a dummy route, even though the coarse method/path check
// (hasPermission) still permits Analysts to write.
func TestAuthMiddleware_AnalystForbiddenOnWhitelistApprove(t *testing.T) {
	s, _ := testServerWithJWT(t)
	analystKey := createAPIKey(t, s, "analyst-key", domain.RoleAnalyst)

	dummy := auth.RequirePermission(auth.PermWhitelistApprove)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler := s.authMiddleware(dummy)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/dummy-approve", nil)
	req.Header.Set("Authorization", "Bearer "+analystKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}
