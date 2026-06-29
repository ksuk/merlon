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

func testServerWithAllEngines() *Server {
	return New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Scoring:      &engine.MockScoringEngine{Score: 3.0, Tier: domain.RiskTierMedium},
		Monitoring:   &engine.MockMonitoringEngine{},
		Screening:    &engine.MockScreeningEngine{},
		Backtest:     &engine.MockBacktestEngine{},
		Audit:        store.NewMemoryAuditRepo(),
		Cases:        store.NewMemoryCaseRepo(),
	})
}

func TestBatchScoreAll(t *testing.T) {
	s := testServerWithAllEngines()

	for _, eid := range []string{"BS001", "BS002", "BS003"} {
		body := `{"external_id":"` + eid + `","customer_type":"individual","country_code":"JP"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/score", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp batchScoreResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Total != 3 {
		t.Errorf("total = %d, want 3", resp.Total)
	}
	if resp.Succeeded != 3 {
		t.Errorf("succeeded = %d, want 3", resp.Succeeded)
	}
	if resp.Failed != 0 {
		t.Errorf("failed = %d, want 0", resp.Failed)
	}
	if len(resp.Results) != 3 {
		t.Errorf("results count = %d, want 3", len(resp.Results))
	}
	if resp.Duration == "" {
		t.Error("expected non-empty duration")
	}
}

func TestBatchScoreSpecificCustomers(t *testing.T) {
	s := testServerWithAllEngines()

	var ids []string
	for _, eid := range []string{"BSS001", "BSS002"} {
		body := `{"external_id":"` + eid + `","customer_type":"individual","country_code":"JP"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		var c domain.Customer
		json.NewDecoder(rec.Body).Decode(&c)
		ids = append(ids, c.ID)
	}

	body := `{"customer_ids":["` + ids[0] + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/score", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var resp batchScoreResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Total != 1 {
		t.Errorf("total = %d, want 1", resp.Total)
	}
}

func TestBatchScoreNoEngine(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/score", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestBatchScoreTooManyCustomers(t *testing.T) {
	s := testServerWithAllEngines()

	ids := make([]string, 1001)
	for i := range ids {
		ids[i] = `"nonexistent` + strings.Repeat("0", 5) + `"`
	}
	body := `{"customer_ids":[` + strings.Join(ids, ",") + `]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/score", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestBatchMonitorAll(t *testing.T) {
	s := testServerWithAllEngines()

	body := `{"external_id":"BM001","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var cust domain.Customer
	json.NewDecoder(rec.Body).Decode(&cust)

	txBody := `{"customer_id":"` + cust.ID + `","external_id":"TX_BM001","amount":100000,"currency":"JPY","direction":"inbound","channel":"web","counterparty_id":"CP1","counterparty_country":"JP"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(txBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodPost, "/api/v1/batch/monitor", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp batchMonitorResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Total != 1 {
		t.Errorf("total = %d, want 1", resp.Total)
	}
	if resp.Succeeded != 1 {
		t.Errorf("succeeded = %d, want 1", resp.Succeeded)
	}
}
