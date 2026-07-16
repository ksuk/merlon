package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
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
	body := `{"definition":{"note":"v2"},"is_active":false}`
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
		Definition: json.RawMessage(`{}`), IsActive: false, CreatedBy: "system", CreatedAt: now, UpdatedAt: now,
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

func TestRuleActivationRequiresDifferentAdmin(t *testing.T) {
	s := testServerWithRules()
	adminOne := createAPIKey(t, s, "admin-one", domain.RoleAdmin)
	adminTwo := createAPIKeyAs(t, s, adminOne, "admin-two", domain.RoleAdmin)

	create := httptest.NewRequest(http.MethodPost, "/api/v1/rules", strings.NewReader(`{"type":"CDD_WEIGHT","name":"dual_control","definition":{}}`))
	create.Header.Set("Authorization", "Bearer "+adminOne)
	created := httptest.NewRecorder()
	s.Handler().ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body: %s", created.Code, created.Body.String())
	}

	activate := httptest.NewRequest(http.MethodPost, "/api/v1/rules/dual_control/activate", nil)
	activate.Header.Set("Authorization", "Bearer "+adminOne)
	blocked := httptest.NewRecorder()
	s.Handler().ServeHTTP(blocked, activate)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("same-admin activation status = %d, want %d", blocked.Code, http.StatusForbidden)
	}

	activate = httptest.NewRequest(http.MethodPost, "/api/v1/rules/dual_control/activate", nil)
	activate.Header.Set("Authorization", "Bearer "+adminTwo)
	approved := httptest.NewRecorder()
	s.Handler().ServeHTTP(approved, activate)
	if approved.Code != http.StatusOK {
		t.Fatalf("second-admin activation status = %d, want %d, body: %s", approved.Code, http.StatusOK, approved.Body.String())
	}
}

func TestRuleActivationRejectsAuthorOfLatestVersionWhenOlderVersionIsActive(t *testing.T) {
	s := testServerWithRules()
	adminA := createAPIKey(t, s, "admin-a", domain.RoleAdmin)
	adminB := createAPIKeyAs(t, s, adminA, "admin-b", domain.RoleAdmin)
	adminC := createAPIKeyAs(t, s, adminA, "admin-c", domain.RoleAdmin)

	create := httptest.NewRequest(http.MethodPost, "/api/v1/rules", strings.NewReader(`{"type":"CDD_WEIGHT","name":"versioned_dual_control","definition":{"note":"v1"}}`))
	create.Header.Set("Authorization", "Bearer "+adminA)
	created := httptest.NewRecorder()
	s.Handler().ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body: %s", created.Code, created.Body.String())
	}

	activateV1 := httptest.NewRequest(http.MethodPost, "/api/v1/rules/versioned_dual_control/activate", nil)
	activateV1.Header.Set("Authorization", "Bearer "+adminB)
	activatedV1 := httptest.NewRecorder()
	s.Handler().ServeHTTP(activatedV1, activateV1)
	if activatedV1.Code != http.StatusOK {
		t.Fatalf("activate v1: status = %d, body: %s", activatedV1.Code, activatedV1.Body.String())
	}

	update := httptest.NewRequest(http.MethodPut, "/api/v1/rules/versioned_dual_control", strings.NewReader(`{"definition":{"note":"v2"}}`))
	update.Header.Set("Authorization", "Bearer "+adminB)
	updated := httptest.NewRecorder()
	s.Handler().ServeHTTP(updated, update)
	if updated.Code != http.StatusOK {
		t.Fatalf("update: status = %d, body: %s", updated.Code, updated.Body.String())
	}

	selfApprove := httptest.NewRequest(http.MethodPost, "/api/v1/rules/versioned_dual_control/activate", nil)
	selfApprove.Header.Set("Authorization", "Bearer "+adminB)
	blocked := httptest.NewRecorder()
	s.Handler().ServeHTTP(blocked, selfApprove)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("same-author activation status = %d, want %d, body: %s", blocked.Code, http.StatusForbidden, blocked.Body.String())
	}
	active, err := s.rules.GetActive(context.Background(), "versioned_dual_control")
	if err != nil {
		t.Fatal(err)
	}
	if active.Version != 1 {
		t.Fatalf("active version after denied self-approval = %d, want 1", active.Version)
	}

	approve := httptest.NewRequest(http.MethodPost, "/api/v1/rules/versioned_dual_control/activate", nil)
	approve.Header.Set("Authorization", "Bearer "+adminC)
	approved := httptest.NewRecorder()
	s.Handler().ServeHTTP(approved, approve)
	if approved.Code != http.StatusOK {
		t.Fatalf("independent activation: status = %d, body: %s", approved.Code, approved.Body.String())
	}
	active, err = s.rules.GetActive(context.Background(), "versioned_dual_control")
	if err != nil {
		t.Fatal(err)
	}
	if active.Version != 2 {
		t.Fatalf("active version = %d, want 2", active.Version)
	}

	entries, err := s.audit.List(context.Background(), domain.AuditListFilter{
		ResourceType: "rules", ResourceID: "versioned_dual_control", Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	foundApproval := false
	foundDenial := false
	for _, entry := range entries {
		if entry.Action == "activate_rule" && entry.Details["target_version"] == "2" && entry.Details["approval_result"] == "denied" {
			if entry.Details["rule_author"] == "" || entry.Details["approver"] == "" || entry.Details["approval_reason"] == "" {
				t.Fatalf("denied approval audit entry lacks maker/checker rationale: %+v", entry.Details)
			}
			if entry.Details["http_status"] != "403" || entry.Details["outcome"] != "denied" {
				t.Fatalf("denied approval audit outcome = %+v, want HTTP 403 denied", entry.Details)
			}
			foundDenial = true
		}
		if entry.Action == "activate_rule" && entry.Details["target_version"] == "2" && entry.Details["approval_result"] == "approved" {
			if entry.Details["rule_author"] == "" || entry.Details["approver"] == "" {
				t.Fatalf("approval audit entry lacks maker/checker details: %+v", entry.Details)
			}
			foundApproval = true
		}
	}
	if !foundApproval {
		t.Fatalf("no successful v2 approval audit entry found: %+v", entries)
	}
	if !foundDenial {
		t.Fatalf("no denied v2 self-approval audit entry found: %+v", entries)
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

	entries, err := s.audit.List(ctx, domain.AuditListFilter{ResourceType: "rules", ResourceID: "cdd_basic", Limit: 10})
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
