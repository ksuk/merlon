package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

func testServerWithConfig() *Server {
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
		Webhooks:     store.NewMemoryWebhookRepo(),
		Config:       &engine.MockConfigEngine{},
	})
}

func TestValidateConfigValid(t *testing.T) {
	s := testServerWithConfig()

	body := `{"config_type":"cdd_weights","yaml_content":"test: valid"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/validate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var result engine.ConfigValidationResult
	json.NewDecoder(rec.Body).Decode(&result)

	if !result.Valid {
		t.Error("expected valid = true")
	}
}

func TestValidateConfigInvalid(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Config: &engine.MockConfigEngine{
			Result: &engine.ConfigValidationResult{
				Valid: false,
				Errors: []engine.ConfigValidationError{
					{Field: "factors", Message: "must not be empty"},
				},
			},
		},
	})

	body := `{"config_type":"cdd_weights","yaml_content":"factors: []"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/validate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var result engine.ConfigValidationResult
	json.NewDecoder(rec.Body).Decode(&result)

	if result.Valid {
		t.Error("expected valid = false")
	}
	if len(result.Errors) != 1 {
		t.Errorf("errors count = %d, want 1", len(result.Errors))
	}
}

func TestValidateConfigMissingType(t *testing.T) {
	s := testServerWithConfig()

	body := `{"yaml_content":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/validate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestValidateConfigNotConfigured(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
	})

	body := `{"config_type":"cdd_weights","yaml_content":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/validate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestSystemInfo(t *testing.T) {
	s := testServerWithConfig()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var info map[string]any
	json.NewDecoder(rec.Body).Decode(&info)

	if info["version"] != "1.0.0" {
		t.Errorf("version = %v", info["version"])
	}

	features, ok := info["features"].(map[string]any)
	if !ok {
		t.Fatal("missing features")
	}
	if features["scoring"] != true {
		t.Error("expected scoring = true")
	}
	if features["config"] != true {
		t.Error("expected config = true")
	}
	if features["demo_data"] != false {
		t.Error("expected demo_data = false when DemoDataEnabled is unset")
	}
}

func TestSystemInfoDemoDataEnabled(t *testing.T) {
	s := New(":0", Deps{
		Customers:       store.NewMemoryCustomerRepo(),
		Transactions:    store.NewMemoryTransactionRepo(),
		Alerts:          store.NewMemoryAlertRepo(),
		DemoDataEnabled: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var info map[string]any
	json.NewDecoder(rec.Body).Decode(&info)

	features, ok := info["features"].(map[string]any)
	if !ok {
		t.Fatal("missing features")
	}
	if features["demo_data"] != true {
		t.Error("expected demo_data = true when DemoDataEnabled is set")
	}
}
