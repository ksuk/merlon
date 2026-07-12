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

func TestRunBacktest(t *testing.T) {
	mockBacktest := &engine.MockBacktestEngine{
		Result: &domain.BacktestResult{
			BacktestID:        "bt_test",
			TotalTransactions: 3,
			TotalCustomers:    1,
			TotalAlerts:       1,
			ScenarioResults: []domain.BacktestScenarioResult{
				{
					ScenarioID:          "test_structuring",
					AlertsGenerated:     1,
					MediumSeverityCount: 1,
					AffectedCustomerIDs: []string{"C001"},
				},
			},
			ExecutionTimeMs: 1.5,
		},
	}

	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Scoring:      &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Monitoring:   &engine.MockMonitoringEngine{},
		Screening:    &engine.MockScreeningEngine{},
		Backtest:     mockBacktest,
	})

	// Create customer
	custBody := `{"external_id":"BT001","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(custBody))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var cust domain.Customer
	json.NewDecoder(rec.Body).Decode(&cust)

	// Create transaction
	txBody := `{"customer_id":"` + cust.ID + `","external_id":"TX001","amount":400000,"currency":"JPY","direction":"inbound","channel":"web","counterparty_id":"CP1","counterparty_country":"JP"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(txBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create txn status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	// Run backtest
	btBody := `{"customer_ids":["` + cust.ID + `"],"scenario_ids":[],"description":"test"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/backtest", strings.NewReader(btBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var result domain.BacktestResult
	json.NewDecoder(rec.Body).Decode(&result)

	if result.BacktestID != "bt_test" {
		t.Errorf("backtest_id = %q, want %q", result.BacktestID, "bt_test")
	}
	if result.TotalAlerts != 1 {
		t.Errorf("total_alerts = %d, want 1", result.TotalAlerts)
	}
}

func TestRunBacktestNoEngine(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
	})

	body := `{"customer_ids":["C001"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backtest", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestRunBacktestNoCustomerIDs(t *testing.T) {
	s := testServer()

	body := `{"customer_ids":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backtest", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestBacktestTooManyCustomers(t *testing.T) {
	s := testServer()

	ids := make([]string, 101)
	for i := range ids {
		ids[i] = `"c` + strings.Repeat("0", 5) + `"`
	}
	body := `{"customer_ids":[` + strings.Join(ids, ",") + `]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backtest", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRunBacktestCustomerNotFound(t *testing.T) {
	s := testServer()

	body := `{"customer_ids":["nonexistent"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backtest", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
