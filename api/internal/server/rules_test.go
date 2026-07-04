package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/engine"
	"github.com/merlon-aml/merlon/api/internal/store"
)

func testServerWithRules() *Server {
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
		Rules:          store.NewMemoryRuleRepo(),
		Config:         &engine.MockConfigEngine{},
		APIKeys:        store.NewMemoryAPIKeyRepo(),
		BootstrapToken: testBootstrapToken,
	})
}

func TestHandleListRules_FiltersByTypeAndActive(t *testing.T) {
	s := testServerWithRules()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	must := func(name string, rt domain.RuleType, active bool, at time.Time) {
		t.Helper()
		if err := s.rules.Create(ctx, &domain.RuleDefinition{
			ID:         generateID(),
			Type:       rt,
			Name:       name,
			Definition: json.RawMessage(`{"schema_version":"1.0"}`),
			IsActive:   active,
			CreatedAt:  at,
			UpdatedAt:  at,
		}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	must("tm_a", domain.RuleTypeTMScenario, true, base)
	must("tm_b", domain.RuleTypeTMScenario, false, base.Add(time.Minute))
	must("cdd_a", domain.RuleTypeCDDWeight, true, base.Add(2*time.Minute))

	key := createAPIKey(t, s, "viewer", domain.RoleViewer)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rules?type=TM_SCENARIO&is_active=true", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp paginatedResponse[domain.RuleDefinition]
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Name != "tm_a" {
		t.Errorf("data = %+v, want exactly [tm_a]", resp.Data)
	}
}

func TestHandleGetRule_SpecificVersion(t *testing.T) {
	s := testServerWithRules()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	v1 := &domain.RuleDefinition{
		ID: generateID(), Type: domain.RuleTypeCDDWeight, Name: "cdd_basic",
		Definition: json.RawMessage(`{"note":"v1"}`), CreatedAt: base, UpdatedAt: base,
	}
	if err := s.rules.Create(ctx, v1); err != nil {
		t.Fatalf("Create: %v", err)
	}
	v2 := &domain.RuleDefinition{
		ID: generateID(), Type: domain.RuleTypeCDDWeight, Name: "cdd_basic",
		Definition: json.RawMessage(`{"note":"v2"}`), CreatedAt: base.Add(time.Minute), UpdatedAt: base.Add(time.Minute),
	}
	if err := s.rules.CreateNewVersion(ctx, v2); err != nil {
		t.Fatalf("CreateNewVersion: %v", err)
	}

	key := createAPIKey(t, s, "viewer", domain.RoleViewer)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rules/cdd_basic?version=1", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var rd domain.RuleDefinition
	if err := json.NewDecoder(rec.Body).Decode(&rd); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rd.Version != 1 {
		t.Errorf("version = %d, want 1", rd.Version)
	}
	if string(rd.Definition) != `{"note":"v1"}` {
		t.Errorf("definition = %s, want v1 content", rd.Definition)
	}
}

func TestHandleCreateRule_RequiresAdmin(t *testing.T) {
	s := testServerWithRules()
	// The bootstrap token can only mint one API key (AUTH-006), so create the
	// admin key first via bootstrap and use it to mint the rest.
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	body := `{"type":"CDD_WEIGHT","name":"cdd_test","definition":{"schema_version":"1.0"}}`

	for _, role := range []domain.Role{domain.RoleAnalyst, domain.RoleViewer} {
		key := createAPIKeyAs(t, s, adminKey, "key-"+string(role), role)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/rules", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("role %s: status = %d, want %d, body: %s", role, rec.Code, http.StatusForbidden, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rules", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create: status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

// createAPIKeyAs mints a new API key using an already-authenticated admin
// key, for tests that need more than one key (the bootstrap token itself
// only authorizes a single use, AUTH-006).
func createAPIKeyAs(t *testing.T, s *Server, adminKey, name string, role domain.Role) string {
	t.Helper()
	body := `{"name":"` + name + `","role":"` + string(role) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/apikeys", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create API key as admin failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp createAPIKeyResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp.Key
}

func TestHandleCreateRule_ValidatesSchema(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Scoring:      &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Monitoring:   &engine.MockMonitoringEngine{},
		Screening:    &engine.MockScreeningEngine{},
		Backtest:     &engine.MockBacktestEngine{},
		Audit:        store.NewMemoryAuditRepo(),
		Cases:        store.NewMemoryCaseRepo(),
		Rules:        store.NewMemoryRuleRepo(),
		Config: &engine.MockConfigEngine{Result: &engine.ConfigValidationResult{
			Valid:  false,
			Errors: []engine.ConfigValidationError{{Field: "definition", Message: "bad schema"}},
		}},
		APIKeys:        store.NewMemoryAPIKeyRepo(),
		BootstrapToken: testBootstrapToken,
	})

	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	body := `{"type":"CDD_WEIGHT","name":"cdd_bad","definition":{"schema_version":"1.0"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rules", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleUpdateRule_IncrementsVersion(t *testing.T) {
	s := testServerWithRules()
	ctx := context.Background()
	now := time.Now()
	if err := s.rules.Create(ctx, &domain.RuleDefinition{
		ID: generateID(), Type: domain.RuleTypeCDDWeight, Name: "cdd_basic",
		Definition: json.RawMessage(`{"note":"v1"}`), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	body := `{"definition":{"note":"v2"},"is_active":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/rules/cdd_basic", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated domain.RuleDefinition
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("version = %d, want 2", updated.Version)
	}

	old, err := s.rules.GetVersion(ctx, "cdd_basic", 1)
	if err != nil {
		t.Fatalf("GetVersion(1): %v", err)
	}
	if string(old.Definition) != `{"note":"v1"}` {
		t.Errorf("old version content = %s, want v1 preserved", old.Definition)
	}
}

func TestHandleActivateRule_TriggersHotReload(t *testing.T) {
	s := testServerWithRules()
	ctx := context.Background()
	now := time.Now()
	if err := s.rules.Create(ctx, &domain.RuleDefinition{
		ID: generateID(), Type: domain.RuleTypeCDDWeight, Name: "cdd_basic",
		Definition: json.RawMessage(`{}`), IsActive: false, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rules/cdd_basic/activate", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	got, err := s.rules.Get(ctx, "cdd_basic")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.IsActive {
		t.Error("expected rule to be active after activate; a later evaluation request should now pick up this version")
	}

	// deactivate should flip it back
	req = httptest.NewRequest(http.MethodPost, "/api/v1/rules/cdd_basic/deactivate", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("deactivate: status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	got, err = s.rules.Get(ctx, "cdd_basic")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.IsActive {
		t.Error("expected rule to be inactive after deactivate")
	}
}

func TestHandleUpdateRule_RecordsAuditDiff(t *testing.T) {
	s := testServerWithRules()
	ctx := context.Background()
	now := time.Now()
	if err := s.rules.Create(ctx, &domain.RuleDefinition{
		ID: generateID(), Type: domain.RuleTypeCDDWeight, Name: "cdd_basic",
		Definition: json.RawMessage(`{"weight":1}`), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	body := `{"definition":{"weight":2}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/rules/cdd_basic", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entries, err := s.audit.List(ctx, "rules", "cdd_basic", 10)
	if err != nil {
		t.Fatalf("audit List: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected an audit entry for the rule update")
	}

	diffRaw, ok := entries[0].Details["diff"]
	if !ok {
		t.Fatalf("expected details[\"diff\"] on audit entry, got %+v", entries[0])
	}

	var diff map[string]struct {
		Before any `json:"before,omitempty"`
		After  any `json:"after,omitempty"`
	}
	if err := json.Unmarshal([]byte(diffRaw), &diff); err != nil {
		t.Fatalf("unmarshal diff: %v", err)
	}
	change, ok := diff["weight"]
	if !ok {
		t.Fatalf("expected diff to include changed key 'weight', got %v", diff)
	}
	if change.Before != float64(1) || change.After != float64(2) {
		t.Errorf("weight diff = %+v, want before=1 after=2", change)
	}
}
