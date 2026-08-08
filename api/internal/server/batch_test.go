package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	if len(resp.Results) != 1 || !resp.Results[0].PendingReview {
		t.Fatalf("results = %+v, want one pending_review result", resp.Results)
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

type countingPendingEvaluationRepo struct {
	*store.MemoryPendingEvaluationRepo
	bulkCalls        int
	perCustomerCalls int
	bulkErr          error
}

func (r *countingPendingEvaluationRepo) ListPendingByCustomers(ctx context.Context, customerIDs []string, status domain.PendingEvaluationStatus) ([]domain.PendingEvaluation, error) {
	r.bulkCalls++
	if r.bulkErr != nil {
		return nil, r.bulkErr
	}
	return r.MemoryPendingEvaluationRepo.ListPendingByCustomers(ctx, customerIDs, status)
}

func TestBatchMonitor_BulkPendingFailureDoesNotFallBackToPerCustomerReads(t *testing.T) {
	pending := &countingPendingEvaluationRepo{MemoryPendingEvaluationRepo: store.NewMemoryPendingEvaluationRepo(), bulkErr: errors.New("database read unavailable")}
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	now := time.Now().UTC()
	for i := 1; i <= 2; i++ {
		id := fmt.Sprintf("c%d", i)
		if err := customers.Create(context.Background(), &domain.Customer{ID: id, ExternalID: id, CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := transactions.Create(context.Background(), &domain.Transaction{ID: fmt.Sprintf("t%d", i), CustomerID: id, Amount: 100, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: now, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	s := New(":0", Deps{Customers: customers, Transactions: transactions, Alerts: store.NewMemoryAlertRepo(), Monitoring: &engine.MockMonitoringEngine{Err: errors.New("engine outage")}, PendingEvaluations: pending})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/monitor", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if pending.bulkCalls != 1 || pending.perCustomerCalls != 0 {
		t.Fatalf("bulk calls = %d, per-customer calls = %d; want 1 and 0", pending.bulkCalls, pending.perCustomerCalls)
	}
}

// recordingMonitoringEngine implements engine.MonitoringEngineV2 so tests can
// assert on the canonical request the handler builds — the evaluation mode and
// the transaction snapshot it hands the engine.
type recordingMonitoringEngine struct {
	engine.MockMonitoringEngine
	requests []engine.MonitoringRequest
}

func (m *recordingMonitoringEngine) Evaluate(_ context.Context, req engine.MonitoringRequest) ([]domain.Alert, error) {
	m.requests = append(m.requests, req)
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Alerts, nil
}

// TestBatchMonitor_ModeSelectsEvaluationMode pins the mode the handler passes
// to the engine. An initial-migration backfill has to run a batch pass too:
// scenarios declaring evaluation_mode: batch (dormant_account_reactivation,
// high_frequency_small_amount) are filtered out of a realtime evaluation, so a
// realtime-only backfill reports success having never applied them.
func TestBatchMonitor_ModeSelectsEvaluationMode(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want engine.EvaluationMode
	}{
		{name: "absent mode stays realtime", body: `{}`, want: engine.EvaluationModeRealtime},
		{name: "explicit realtime", body: `{"mode":"realtime"}`, want: engine.EvaluationModeRealtime},
		{name: "explicit batch", body: `{"mode":"batch"}`, want: engine.EvaluationModeBatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			monitoring := &recordingMonitoringEngine{}
			customers := store.NewMemoryCustomerRepo()
			transactions := store.NewMemoryTransactionRepo()
			now := time.Now().UTC()
			if err := customers.Create(context.Background(), &domain.Customer{ID: "c1", ExternalID: "c1", CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			if err := transactions.Create(context.Background(), &domain.Transaction{ID: "t1", CustomerID: "c1", ExternalID: "t1", Amount: 100, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: now, CreatedAt: now}); err != nil {
				t.Fatal(err)
			}
			s := New(":0", Deps{Customers: customers, Transactions: transactions, Alerts: store.NewMemoryAlertRepo(), Monitoring: monitoring})

			req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/monitor", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if len(monitoring.requests) != 1 {
				t.Fatalf("engine calls = %d, want 1", len(monitoring.requests))
			}
			if got := monitoring.requests[0].Mode; got != tc.want {
				t.Errorf("mode = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBatchMonitor_RejectsUnknownMode(t *testing.T) {
	s := testServerWithAllEngines()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/monitor", strings.NewReader(`{"mode":"both"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	// "both" is a scenario-side declaration, not a request mode: the engine
	// treats anything non-batch as realtime, so accepting it would silently
	// mean realtime.
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// TestBatchMonitor_EvaluatesHistoryBeyondBatchCustomerLimit covers the silent
// truncation this handler used to perform: it loaded a customer's history with
// ListByCustomer(..., maxBatchCustomers, 0), i.e. the newest 1000 rows by
// executed_at DESC. Everything older was dropped without a word, which
// invalidates long-window and dormancy scenarios and makes a history backfill
// report success over data it never read.
func TestBatchMonitor_EvaluatesHistoryBeyondBatchCustomerLimit(t *testing.T) {
	monitoring := &recordingMonitoringEngine{}
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	now := time.Now().UTC()
	if err := customers.Create(context.Background(), &domain.Customer{ID: "c1", ExternalID: "c1", CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	const total = maxBatchCustomers + 5
	oldest := now.Add(-time.Duration(total) * time.Hour)
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("t%04d", i)
		txn := &domain.Transaction{
			ID: id, CustomerID: "c1", ExternalID: id,
			Amount: 100, Currency: "JPY", Direction: domain.DirectionInbound,
			ExecutedAt: oldest.Add(time.Duration(i) * time.Hour), CreatedAt: now,
		}
		if err := transactions.Create(context.Background(), txn); err != nil {
			t.Fatal(err)
		}
	}

	s := New(":0", Deps{Customers: customers, Transactions: transactions, Alerts: store.NewMemoryAlertRepo(), Monitoring: monitoring})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/monitor", strings.NewReader(`{"mode":"batch"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(monitoring.requests) != 1 {
		t.Fatalf("engine calls = %d, want 1", len(monitoring.requests))
	}
	got := monitoring.requests[0].Transactions
	if len(got) != total {
		t.Fatalf("transactions passed to engine = %d, want %d (older history must not be truncated)", len(got), total)
	}
	if !got[0].ExecutedAt.Equal(oldest) {
		t.Errorf("oldest transaction executed_at = %s, want %s", got[0].ExecutedAt, oldest)
	}
}

// TestBatchMonitor_NonBaseCurrencyQueuesPendingReview mirrors the realtime
// ingest path (monitorCreatedTransaction): the engine sums nominal amounts, so
// a mixed-currency snapshot would be compared against base-currency thresholds
// and produce a detection result that is simply wrong. Fail-Alert routes it to
// review rather than reporting a clean pass.
func TestBatchMonitor_NonBaseCurrencyQueuesPendingReview(t *testing.T) {
	monitoring := &recordingMonitoringEngine{}
	pending := store.NewMemoryPendingEvaluationRepo()
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	now := time.Now().UTC()
	if err := customers.Create(context.Background(), &domain.Customer{ID: "c1", ExternalID: "c1", CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := transactions.Create(context.Background(), &domain.Transaction{ID: "t1", CustomerID: "c1", ExternalID: "t1", Amount: 5000, Currency: "USD", Direction: domain.DirectionOutbound, ExecutedAt: now, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	s := New(":0", Deps{
		Customers: customers, Transactions: transactions, Alerts: store.NewMemoryAlertRepo(),
		Monitoring: monitoring, PendingEvaluations: pending, TMBaseCurrency: "JPY",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/monitor", strings.NewReader(`{"mode":"batch"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp batchMonitorResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.QueuedForReview != 1 {
		t.Errorf("queued_for_review = %d, want 1", resp.QueuedForReview)
	}
	if resp.Succeeded != 0 {
		t.Errorf("succeeded = %d, want 0 (a non-base-currency snapshot is not a clean pass)", resp.Succeeded)
	}
	if len(monitoring.requests) != 0 {
		t.Errorf("engine calls = %d, want 0 (the aggregation must not run on mixed currencies)", len(monitoring.requests))
	}

	queued, err := pending.ListByStatus(context.Background(), domain.PendingEvaluationStatusPendingReview, 10, 0)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(queued) != 1 {
		t.Fatalf("pending_evaluations count = %d, want 1", len(queued))
	}
	if !strings.Contains(queued[0].Reason, "USD") {
		t.Errorf("reason = %q, want it to name the offending currency", queued[0].Reason)
	}
}

// TestBatchMonitor_StatusPolicyFollowsMode pins the data model §1.1.2 rule that
// a dormant customer is evaluated only 取引発生時 (the realtime path), never on
// a batch pass, matching evaluateCustomerBatch in
// api/internal/batch/scheduler.go. A closed customer is excluded in both modes.
func TestBatchMonitor_StatusPolicyFollowsMode(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      domain.CustomerStatus
		body        string
		wantEvalled bool
	}{
		{name: "dormant is evaluated in realtime", status: domain.CustomerStatusDormant, body: `{}`, wantEvalled: true},
		{name: "dormant is skipped in batch", status: domain.CustomerStatusDormant, body: `{"mode":"batch"}`, wantEvalled: false},
		{name: "frozen is evaluated in batch", status: domain.CustomerStatusFrozen, body: `{"mode":"batch"}`, wantEvalled: true},
		{name: "closed is skipped in batch", status: domain.CustomerStatusClosed, body: `{"mode":"batch"}`, wantEvalled: false},
		{name: "closed is skipped in realtime", status: domain.CustomerStatusClosed, body: `{}`, wantEvalled: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			monitoring := &recordingMonitoringEngine{}
			customers := store.NewMemoryCustomerRepo()
			transactions := store.NewMemoryTransactionRepo()
			now := time.Now().UTC()
			if err := customers.Create(context.Background(), &domain.Customer{ID: "c1", ExternalID: "c1", CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", Status: tc.status, CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			if err := transactions.Create(context.Background(), &domain.Transaction{ID: "t1", CustomerID: "c1", ExternalID: "t1", Amount: 100, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: now, CreatedAt: now}); err != nil {
				t.Fatal(err)
			}
			s := New(":0", Deps{Customers: customers, Transactions: transactions, Alerts: store.NewMemoryAlertRepo(), Monitoring: monitoring})

			req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/monitor", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if evalled := len(monitoring.requests) == 1; evalled != tc.wantEvalled {
				t.Errorf("engine calls = %d, want evaluated = %v", len(monitoring.requests), tc.wantEvalled)
			}
		})
	}
}

func (r *countingPendingEvaluationRepo) ListPendingByCustomer(ctx context.Context, customerID string, status domain.PendingEvaluationStatus) ([]domain.PendingEvaluation, error) {
	r.perCustomerCalls++
	return r.MemoryPendingEvaluationRepo.ListPendingByCustomer(ctx, customerID, status)
}

func TestBatchMonitor_EngineOutageBulkLoadsPendingRecordsOnce(t *testing.T) {
	pending := &countingPendingEvaluationRepo{MemoryPendingEvaluationRepo: store.NewMemoryPendingEvaluationRepo()}
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	now := time.Now().UTC()
	for i := 1; i <= 2; i++ {
		id := fmt.Sprintf("c%d", i)
		customer := &domain.Customer{ID: id, ExternalID: id, CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", CreatedAt: now, UpdatedAt: now}
		if err := customers.Create(context.Background(), customer); err != nil {
			t.Fatal(err)
		}
		txn := &domain.Transaction{ID: fmt.Sprintf("t%d", i), CustomerID: id, ExternalID: fmt.Sprintf("t%d", i), Amount: 100, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: now, CreatedAt: now}
		if err := transactions.Create(context.Background(), txn); err != nil {
			t.Fatal(err)
		}
	}
	s := New(":0", Deps{
		Customers: customers, Transactions: transactions, Alerts: store.NewMemoryAlertRepo(),
		Monitoring:         &engine.MockMonitoringEngine{Err: errors.New("engine outage")},
		PendingEvaluations: pending,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/monitor", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if pending.bulkCalls != 1 || pending.perCustomerCalls != 0 {
		t.Fatalf("bulk calls = %d, per-customer calls = %d; want 1 and 0", pending.bulkCalls, pending.perCustomerCalls)
	}
}
