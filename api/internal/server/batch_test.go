package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
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

// TestBatchMonitor_DedupsRepeatedAlertForSameScenarioAndWindow verifies
// Task4/Task7's dedup routing: calling /api/v1/batch/monitor twice for a
// customer whose transactions keep triggering the same scenario must not
// create a second alert for the same (customer_id, scenario_id,
// aggregation_window_start) tuple (the transaction-monitoring design「バッチ/リアルタイム
// 評価の重複アラート防止」).
func TestBatchMonitor_DedupsRepeatedAlertForSameScenarioAndWindow(t *testing.T) {
	s := testServerWithAllEngines()
	s.monitoring = &engine.MockMonitoringEngine{
		EvaluateFunc: func(_ context.Context, customerID string, _ domain.RiskTier, _ []domain.Transaction, _ []string) ([]domain.Alert, error) {
			return []domain.Alert{{CustomerID: customerID, ScenarioID: "structuring", Severity: domain.AlertSeverityHigh, Description: "test"}}, nil
		},
	}

	body := `{"external_id":"BMDEDUP001","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var cust domain.Customer
	json.NewDecoder(rec.Body).Decode(&cust)

	txBody := `{"customer_id":"` + cust.ID + `","external_id":"TX_BMDEDUP001","amount":100000,"currency":"JPY","direction":"inbound","channel":"web","counterparty_id":"CP1","counterparty_country":"JP"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(txBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	for i := 0; i < 2; i++ {
		req = httptest.NewRequest(http.MethodPost, "/api/v1/batch/monitor", strings.NewReader(`{}`))
		rec = httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("pass %d: status = %d, want %d, body: %s", i, rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	alerts, err := s.alerts.ListByCustomer(context.Background(), cust.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListByCustomer: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("alerts count = %d, want 1 (second batch/monitor pass must not duplicate the alert)", len(alerts))
	}
}

// TestBatchMonitor_EngineDown_QueuesPendingReview verifies OPS-005 /
// Fail-Alert (the operational design §4.4): when the monitoring engine call fails, the
// affected customer's transactions are queued as PENDING_REVIEW instead of
// being reported as a hard failure, and the endpoint itself still returns
// 200 (detection degrades to "review later", it never simply stops).
func TestBatchMonitor_EngineDown_QueuesPendingReview(t *testing.T) {
	pending := store.NewMemoryPendingEvaluationRepo()
	s := New(":0", Deps{
		Customers:          store.NewMemoryCustomerRepo(),
		Transactions:       store.NewMemoryTransactionRepo(),
		Alerts:             store.NewMemoryAlertRepo(),
		Monitoring:         &engine.MockMonitoringEngine{Err: errors.New("engine unavailable: deadline exceeded")},
		Audit:              store.NewMemoryAuditRepo(),
		Cases:              store.NewMemoryCaseRepo(),
		PendingEvaluations: pending,
	})

	body := `{"external_id":"BMPR001","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var cust domain.Customer
	json.NewDecoder(rec.Body).Decode(&cust)

	txBody := `{"customer_id":"` + cust.ID + `","external_id":"TX_BMPR001","amount":100000,"currency":"JPY","direction":"inbound","channel":"web","counterparty_id":"CP1","counterparty_country":"JP"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(txBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodPost, "/api/v1/batch/monitor", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (Fail-Alert: engine failure must not surface as an error response), body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp batchMonitorResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Failed != 0 {
		t.Errorf("failed = %d, want 0 (queued for review, not counted as failed)", resp.Failed)
	}
	if resp.QueuedForReview != 1 {
		t.Errorf("queued_for_review = %d, want 1", resp.QueuedForReview)
	}

	queued, err := pending.ListByStatus(context.Background(), domain.PendingEvaluationStatusPendingReview, 10, 0)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(queued) != 1 {
		t.Fatalf("pending_evaluations count = %d, want 1", len(queued))
	}
	if queued[0].CustomerID != cust.ID {
		t.Errorf("CustomerID = %s, want %s", queued[0].CustomerID, cust.ID)
	}
	if queued[0].Reason == "" {
		t.Error("expected non-empty reason recording the engine failure")
	}
}

// TestBatchMonitor_NoPendingRepo_TreatsEngineFailureAsHardFailure ensures the
// PENDING_REVIEW fallback is opt-in: deployments that have not wired a
// PendingEvaluationRepository keep the pre-existing behavior of reporting
// the customer as failed rather than silently discarding the error.
func TestBatchMonitor_NoPendingRepo_TreatsEngineFailureAsHardFailure(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Monitoring:   &engine.MockMonitoringEngine{Err: errors.New("engine unavailable")},
		Audit:        store.NewMemoryAuditRepo(),
		Cases:        store.NewMemoryCaseRepo(),
	})

	body := `{"external_id":"BMPR002","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var cust domain.Customer
	json.NewDecoder(rec.Body).Decode(&cust)

	txBody := `{"customer_id":"` + cust.ID + `","external_id":"TX_BMPR002","amount":100000,"currency":"JPY","direction":"inbound","channel":"web","counterparty_id":"CP1","counterparty_country":"JP"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(txBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodPost, "/api/v1/batch/monitor", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var resp batchMonitorResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Failed != 1 {
		t.Errorf("failed = %d, want 1 (no pending repo configured, falls back to hard failure)", resp.Failed)
	}
	if resp.QueuedForReview != 0 {
		t.Errorf("queued_for_review = %d, want 0", resp.QueuedForReview)
	}
}
