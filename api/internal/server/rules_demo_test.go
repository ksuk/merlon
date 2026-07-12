package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

// TestDemoScenario_ImportActivateScoreWithinTimeLimit exercises the D1
// acceptance scenario end-to-end at the API layer: import the free
// content/_sample/country_risk_sample.yaml, activate it, create a dummy
// customer, and score them. The real acceptance criterion ("docker compose
// up → sample import → scoring, within 5 minutes") requires the full stack
// (Postgres + Rust engine over gRPC) and a manual `docker compose up`, which
// this sandboxed environment cannot exercise — see the manual demo steps
// documented alongside the acceptance criteria in
// docs/fable_check/tasks/ws02-rule-management.md. This test only asserts
// that the import→activate→score request chain succeeds against the
// in-memory store and mock engines.
func TestDemoScenario_ImportActivateScoreWithinTimeLimit(t *testing.T) {
	sampleContent, err := os.ReadFile("../../../content/_sample/country_risk_sample.yaml")
	if err != nil {
		t.Fatalf("read sample content: %v", err)
	}
	var sampleDefinition map[string]any
	if err := yaml.Unmarshal(sampleContent, &sampleDefinition); err != nil {
		t.Fatalf("parse sample yaml: %v", err)
	}

	s := New(":0", Deps{
		Customers:      store.NewMemoryCustomerRepo(),
		Transactions:   store.NewMemoryTransactionRepo(),
		Alerts:         store.NewMemoryAlertRepo(),
		Scoring:        &engine.MockScoringEngine{Score: 2.0, Tier: domain.RiskTierMedium},
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
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)

	start := time.Now()

	importBody, err := json.Marshal([]map[string]any{{
		"type":       "COUNTRY_RISK",
		"name":       "country_risk_sample",
		"definition": sampleDefinition,
	}})
	if err != nil {
		t.Fatalf("marshal import body: %v", err)
	}

	importReq := httptest.NewRequest(http.MethodPost, "/api/v1/rules/import", bytes.NewReader(importBody))
	importReq.Header.Set("Authorization", "Bearer "+adminKey)
	importReq.Header.Set("Content-Type", "application/json")
	importRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(importRec, importReq)
	if importRec.Code != http.StatusCreated {
		t.Fatalf("import: status = %d, want %d, body: %s", importRec.Code, http.StatusCreated, importRec.Body.String())
	}

	activateReq := httptest.NewRequest(http.MethodPost, "/api/v1/rules/country_risk_sample/activate", nil)
	activateReq.Header.Set("Authorization", "Bearer "+adminKey)
	activateRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(activateRec, activateReq)
	if activateRec.Code != http.StatusOK {
		t.Fatalf("activate: status = %d, want %d, body: %s", activateRec.Code, http.StatusOK, activateRec.Body.String())
	}

	customerBody := `{"external_id":"DEMO001","customer_type":"individual","country_code":"JP","product_types":["spot_trading"]}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(customerBody))
	createReq.Header.Set("Authorization", "Bearer "+adminKey)
	createRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create customer: status = %d, want %d, body: %s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}
	var customer domain.Customer
	if err := json.NewDecoder(createRec.Body).Decode(&customer); err != nil {
		t.Fatalf("decode customer: %v", err)
	}

	scoreReq := httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+customer.ID+"/score", strings.NewReader(`{"rule_set_id":"country_risk_sample"}`))
	scoreReq.Header.Set("Authorization", "Bearer "+adminKey)
	scoreRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(scoreRec, scoreReq)
	if scoreRec.Code != http.StatusOK {
		t.Fatalf("score: status = %d, want %d, body: %s", scoreRec.Code, http.StatusOK, scoreRec.Body.String())
	}

	t.Logf("import->activate->score request chain completed in %s (docker-compose demo target: <5min)", time.Since(start))
}
