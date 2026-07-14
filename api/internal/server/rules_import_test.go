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

// fnConfigEngine lets a test inspect the yamlContent passed to ValidateConfig,
// which the fixed-response engine.MockConfigEngine cannot do.
type fnConfigEngine struct {
	fn func(ctx context.Context, configType, yamlContent string) (*engine.ConfigValidationResult, error)
}

func (f *fnConfigEngine) ValidateConfig(ctx context.Context, configType, yamlContent string) (*engine.ConfigValidationResult, error) {
	return f.fn(ctx, configType, yamlContent)
}

func TestHandleImportRules_AllValid_CreatesAll(t *testing.T) {
	s := testServerWithRules()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)

	body := `[
		{"type":"TM_SCENARIO","name":"import_tm","definition":{"schema_version":"1.0"}},
		{"type":"CDD_WEIGHT","name":"import_cdd","definition":{"schema_version":"1.0"}},
		{"type":"COUNTRY_RISK","name":"import_country","definition":{"schema_version":"1.0","default_score":3}}
	]`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rules/import", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	for _, name := range []string{"import_tm", "import_cdd", "import_country"} {
		if _, err := s.rules.Get(context.Background(), name); err != nil {
			t.Errorf("expected rule %q to have been created: %v", name, err)
		}
	}
}

func TestHandleImportRules_OneInvalid_RejectsAll(t *testing.T) {
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
		Config: &fnConfigEngine{fn: func(_ context.Context, _, yamlContent string) (*engine.ConfigValidationResult, error) {
			if strings.Contains(yamlContent, "BROKEN") {
				return &engine.ConfigValidationResult{Valid: false, Errors: []engine.ConfigValidationError{{Field: "definition", Message: "broken"}}}, nil
			}
			return &engine.ConfigValidationResult{Valid: true}, nil
		}},
		APIKeys:        store.NewMemoryAPIKeyRepo(),
		BootstrapToken: testBootstrapToken,
	})
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)

	body := `[
		{"type":"TM_SCENARIO","name":"batch_ok_1","definition":{"schema_version":"1.0"}},
		{"type":"CDD_WEIGHT","name":"batch_bad","definition":{"schema_version":"1.0","marker":"BROKEN"}},
		{"type":"SCREENING_CONFIG","name":"batch_ok_2","definition":{"schema_version":"1.0"}}
	]`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rules/import", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}

	for _, name := range []string{"batch_ok_1", "batch_bad", "batch_ok_2"} {
		if _, err := s.rules.Get(context.Background(), name); err == nil {
			t.Errorf("rule %q should not have been created (atomic rejection)", name)
		}
	}
}

func TestHandleImportRules_UsesEngineConfigValidation(t *testing.T) {
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
			Errors: []engine.ConfigValidationError{{Field: "definition", Message: "rejected by engine"}},
		}},
		APIKeys:        store.NewMemoryAPIKeyRepo(),
		BootstrapToken: testBootstrapToken,
	})
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)

	body := `[{"type":"TM_SCENARIO","name":"rejected_rule","definition":{"schema_version":"1.0"}}]`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rules/import", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if _, err := s.rules.Get(context.Background(), "rejected_rule"); err == nil {
		t.Error("rule should not have been created when engine validation fails")
	}
}

func TestExportImportRoundTrip_SemanticEquivalence(t *testing.T) {
	s := testServerWithRules()
	ctx := context.Background()
	now := time.Now()

	original := &domain.RuleDefinition{
		ID:         generateID(),
		Type:       domain.RuleTypeCountryRisk,
		Name:       "country_risk_sample",
		Definition: json.RawMessage(`{"schema_version":"1.0","default_score":3,"countries":{"JP":{"score":1},"KP":{"score":5,"reason":"FATF"}}}`),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.rules.Create(ctx, original); err != nil {
		t.Fatalf("Create: %v", err)
	}

	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)

	exportReq := httptest.NewRequest(http.MethodGet, "/api/v1/rules/country_risk_sample/export", nil)
	exportReq.Header.Set("Authorization", "Bearer "+adminKey)
	exportRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(exportRec, exportReq)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status = %d, want %d, body: %s", exportRec.Code, http.StatusOK, exportRec.Body.String())
	}

	var exported map[string]any
	if err := json.Unmarshal(exportRec.Body.Bytes(), &exported); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	exported["name"] = "country_risk_sample_reimport"

	importBody, err := json.Marshal([]any{exported})
	if err != nil {
		t.Fatalf("marshal import body: %v", err)
	}

	importReq := httptest.NewRequest(http.MethodPost, "/api/v1/rules/import", strings.NewReader(string(importBody)))
	importReq.Header.Set("Authorization", "Bearer "+adminKey)
	importReq.Header.Set("Content-Type", "application/json")
	importRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(importRec, importReq)
	if importRec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, want %d, body: %s", importRec.Code, http.StatusCreated, importRec.Body.String())
	}

	reimported, err := s.rules.Get(ctx, "country_risk_sample_reimport")
	if err != nil {
		t.Fatalf("Get reimported: %v", err)
	}

	var originalVal, reimportedVal any
	if err := json.Unmarshal(original.Definition, &originalVal); err != nil {
		t.Fatalf("unmarshal original: %v", err)
	}
	if err := json.Unmarshal(reimported.Definition, &reimportedVal); err != nil {
		t.Fatalf("unmarshal reimported: %v", err)
	}
	originalJSON, _ := json.Marshal(originalVal)
	reimportedJSON, _ := json.Marshal(reimportedVal)
	if string(originalJSON) != string(reimportedJSON) {
		t.Errorf("definition not semantically equivalent after export/import round trip:\noriginal:   %s\nreimported: %s", originalJSON, reimportedJSON)
	}
}

func TestHandleExportRule_YAMLFormat(t *testing.T) {
	s := testServerWithRules()
	ctx := context.Background()
	now := time.Now()
	if err := s.rules.Create(ctx, &domain.RuleDefinition{
		ID: generateID(), Type: domain.RuleTypeCDDWeight, Name: "cdd_basic",
		Definition: json.RawMessage(`{"schema_version":"1.0","weight":1}`), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rules/cdd_basic/export?format=yaml", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "yaml") {
		t.Errorf("Content-Type = %q, want it to mention yaml", ct)
	}
	if !strings.Contains(rec.Body.String(), "name: cdd_basic") {
		t.Errorf("expected YAML body to contain name: cdd_basic, got:\n%s", rec.Body.String())
	}
}
