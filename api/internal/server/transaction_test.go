package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

func createTestCustomer(t *testing.T, s *Server) domain.Customer {
	t.Helper()
	body := `{"external_id":"TX_CUST","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create customer failed: %d %s", rec.Code, rec.Body.String())
	}
	var c domain.Customer
	json.NewDecoder(rec.Body).Decode(&c)
	return c
}

func TestCreateTransaction(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX001","amount":100000,"currency":"JPY","direction":"outbound","transaction_type":" Deposit "}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var tx domain.Transaction
	json.NewDecoder(rec.Body).Decode(&tx)

	if tx.ID == "" {
		t.Error("expected non-empty ID")
	}
	if tx.Amount != 100000 {
		t.Errorf("amount = %f, want 100000", tx.Amount)
	}
	if tx.TransactionType != domain.TransactionType("deposit") {
		t.Errorf("transaction_type = %q, want normalized deposit", tx.TransactionType)
	}
}

type transactionMonitoringResponse struct {
	domain.Transaction
	MonitoringEvaluation *struct {
		PendingEvaluationID string                         `json:"pending_evaluation_id"`
		Status              domain.PendingEvaluationStatus `json:"status"`
		Reason              string                         `json:"reason"`
	} `json:"monitoring_evaluation,omitempty"`
}

func newMonitoringUnavailableServer() (*Server, *store.MemoryTransactionRepo, *store.MemoryPendingEvaluationRepo, *store.MemoryAuditRepo) {
	transactions := store.NewMemoryTransactionRepo()
	pending := store.NewMemoryPendingEvaluationRepo()
	audit := store.NewMemoryAuditRepo()
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(), Transactions: transactions, Alerts: store.NewMemoryAlertRepo(),
		Audit: audit, Scoring: &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		PendingEvaluations: pending, Screening: &engine.MockScreeningEngine{},
	})
	return s, transactions, pending, audit
}

func TestCreateTransactionMonitoringUnavailableQueuesPendingAtomically(t *testing.T) {
	s, _, pending, audit := newMonitoringUnavailableServer()
	cust := createTestCustomer(t, s)
	body := `{"customer_id":"` + cust.ID + `","external_id":"TX-MONITORING-UNAVAILABLE","amount":100,"currency":"JPY","direction":"inbound","executed_at":"2026-01-02T03:04:05Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created transactionMonitoringResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.MonitoringEvaluation == nil {
		t.Fatal("create response omitted monitoring_evaluation")
	}
	if created.MonitoringEvaluation.Status != domain.PendingEvaluationStatusPendingReview || created.MonitoringEvaluation.PendingEvaluationID == "" {
		t.Fatalf("monitoring_evaluation = %+v, want pending review with durable id", created.MonitoringEvaluation)
	}
	if created.MonitoringEvaluation.Reason != "monitoring_unavailable: dependency_not_configured" {
		t.Fatalf("reason = %q, want stable dependency reason", created.MonitoringEvaluation.Reason)
	}

	queued, err := pending.ListByStatus(context.Background(), domain.PendingEvaluationStatusPendingReview, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 || queued[0].ID != created.MonitoringEvaluation.PendingEvaluationID || len(queued[0].TransactionIDs) != 1 || queued[0].TransactionIDs[0] != created.ID {
		t.Fatalf("queued = %+v, want the created transaction in one pending record", queued)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/transactions/"+created.ID, nil)
	getRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body: %s", getRec.Code, getRec.Body.String())
	}
	var loaded transactionMonitoringResponse
	if err := json.NewDecoder(getRec.Body).Decode(&loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.MonitoringEvaluation == nil || loaded.MonitoringEvaluation.PendingEvaluationID != created.MonitoringEvaluation.PendingEvaluationID || loaded.MonitoringEvaluation.Status != domain.PendingEvaluationStatusPendingReview {
		t.Fatalf("GET monitoring_evaluation = %+v, want create response state", loaded.MonitoringEvaluation)
	}

	entries, err := audit.List(context.Background(), domain.AuditListFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action == "pending_evaluation_queued" && entry.ResourceID == created.MonitoringEvaluation.PendingEvaluationID && entry.Details["transaction_id"] == created.ID && entry.Details["reason"] == created.MonitoringEvaluation.Reason {
			found = true
		}
	}
	if !found {
		t.Fatal("pending_evaluation_queued audit entry not found")
	}
}

func TestCreateTransactionMonitoringUnavailableWithoutPendingStoreRollsBack(t *testing.T) {
	transactions := store.NewMemoryTransactionRepo()
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(), Transactions: transactions, Alerts: store.NewMemoryAlertRepo(),
		Audit: store.NewMemoryAuditRepo(), Scoring: &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Screening: &engine.MockScreeningEngine{},
	})
	cust := createTestCustomer(t, s)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{"customer_id":"`+cust.ID+`","external_id":"TX-NO-PENDING-STORE","amount":100,"currency":"JPY","direction":"inbound"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	items, err := transactions.ListByCustomer(context.Background(), cust.ID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("transactions after missing pending store = %d, want rollback", len(items))
	}
}

func TestCreateTransactionMonitoringUnavailableIdempotentReplayDoesNotDuplicatePendingOrAudit(t *testing.T) {
	s, transactions, pending, audit := newMonitoringUnavailableServer()
	cust := createTestCustomer(t, s)
	body := `{"customer_id":"` + cust.ID + `","external_id":"TX-MONITORING-IDEMPOTENT","amount":100,"currency":"JPY","direction":"inbound","executed_at":"2026-01-02T03:04:05Z"}`
	responses := make([]transactionMonitoringResponse, 0, 2)
	for i, wantStatus := range []int{http.StatusCreated, http.StatusOK} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", "monitoring-unavailable-retry")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != wantStatus {
			t.Fatalf("request %d status = %d, want %d, body: %s", i+1, rec.Code, wantStatus, rec.Body.String())
		}
		var response transactionMonitoringResponse
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, response)
	}
	if responses[0].ID != responses[1].ID || responses[0].MonitoringEvaluation == nil || responses[1].MonitoringEvaluation == nil || responses[0].MonitoringEvaluation.PendingEvaluationID != responses[1].MonitoringEvaluation.PendingEvaluationID {
		t.Fatalf("replay responses disagree: first=%+v second=%+v", responses[0], responses[1])
	}
	items, err := transactions.ListByCustomer(context.Background(), cust.ID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := pending.ListByStatus(context.Background(), domain.PendingEvaluationStatusPendingReview, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || len(queued) != 1 {
		t.Fatalf("transactions/pending = %d/%d, want 1/1", len(items), len(queued))
	}
	entries, err := audit.List(context.Background(), domain.AuditListFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	queuedAudits := 0
	for _, entry := range entries {
		if entry.Action == "pending_evaluation_queued" {
			queuedAudits++
		}
	}
	if queuedAudits != 1 {
		t.Fatalf("pending_evaluation_queued audits = %d, want 1", queuedAudits)
	}
}

func TestCreateTransactionAuditFailureRollsBackTransaction(t *testing.T) {
	audit := store.NewMemoryAuditRepo()
	transactions := store.NewMemoryTransactionRepo()
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(), Transactions: transactions, Alerts: store.NewMemoryAlertRepo(),
		Audit: audit, Scoring: &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Monitoring: &engine.MockMonitoringEngine{}, Screening: &engine.MockScreeningEngine{},
	})
	cust := createTestCustomer(t, s)
	audit.SetCreateFailure(errors.New("audit unavailable"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{"customer_id":"`+cust.ID+`","external_id":"TX-AUDIT-FAIL","amount":100,"currency":"JPY","direction":"inbound"}`))
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, req)
	if response.Code == http.StatusCreated {
		t.Fatalf("status = %d, want failure when required audit append fails", response.Code)
	}
	items, err := transactions.ListByCustomer(context.Background(), cust.ID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("transactions after audit rollback = %d, want 0", len(items))
	}
}

func TestCreateTransactionOutboxFailureRollsBackTransaction(t *testing.T) {
	outbox := store.NewMemoryEventOutboxRepo()
	transactions := store.NewMemoryTransactionRepo()
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(), Transactions: transactions, Alerts: store.NewMemoryAlertRepo(),
		Audit: store.NewMemoryAuditRepo(), EventOutbox: outbox,
		Scoring:    &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Monitoring: &engine.MockMonitoringEngine{}, Screening: &engine.MockScreeningEngine{},
	})
	cust := createTestCustomer(t, s)
	outbox.SetEnqueueFailure(errors.New("outbox unavailable"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{"customer_id":"`+cust.ID+`","external_id":"TX-OUTBOX-FAIL","amount":100,"currency":"JPY","direction":"inbound"}`))
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, req)
	if response.Code == http.StatusCreated {
		t.Fatalf("status = %d, want failure when required outbox append fails", response.Code)
	}
	items, err := transactions.ListByCustomer(context.Background(), cust.ID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("transactions after outbox rollback = %d, want 0", len(items))
	}
}

func TestCreateFutureDatedTransactionIncludesItselfInRealtimeEvaluation(t *testing.T) {
	var evaluated []domain.Transaction
	monitoring := &engine.MockMonitoringEngine{EvaluateFunc: func(_ context.Context, _ string, _ domain.RiskTier, transactions []domain.Transaction, _ []string) ([]domain.Alert, error) {
		evaluated = append([]domain.Transaction(nil), transactions...)
		return nil, nil
	}}
	s := testServerWithEngine(&engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium}, monitoring)
	cust := createTestCustomer(t, s)
	future := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	body := `{"customer_id":"` + cust.ID + `","external_id":"TX-FUTURE","amount":100,"currency":"JPY","direction":"inbound","executed_at":"` + future.Format(time.RFC3339) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if len(evaluated) != 1 || evaluated[0].ExternalID != "TX-FUTURE" {
		t.Fatalf("evaluated transactions = %+v, want the newly created future-dated transaction", evaluated)
	}
}

type boundedHistoryMonitoring struct {
	*engine.MockMonitoringEngine
	window time.Duration
}

func (m *boundedHistoryMonitoring) RealtimeHistoryWindow() (time.Duration, bool) {
	return m.window, true
}

func TestRealtimeEvaluationLoadsOnlyDeclaredScenarioWindow(t *testing.T) {
	var evaluated []domain.Transaction
	monitoring := &boundedHistoryMonitoring{
		MockMonitoringEngine: &engine.MockMonitoringEngine{EvaluateFunc: func(_ context.Context, _ string, _ domain.RiskTier, transactions []domain.Transaction, _ []string) ([]domain.Alert, error) {
			evaluated = append([]domain.Transaction(nil), transactions...)
			return nil, nil
		}},
		window: 24 * time.Hour,
	}
	s := testServerWithEngine(&engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium}, monitoring)
	cust := createTestCustomer(t, s)
	now := time.Now().UTC()
	old := &domain.Transaction{ID: "old", CustomerID: cust.ID, ExternalID: "OLD", Amount: 100, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: now.Add(-25 * time.Hour), CreatedAt: now.Add(-25 * time.Hour)}
	inside := &domain.Transaction{ID: "inside", CustomerID: cust.ID, ExternalID: "INSIDE", Amount: 100, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: now.Add(-23 * time.Hour), CreatedAt: now.Add(-23 * time.Hour)}
	for _, txn := range []*domain.Transaction{old, inside} {
		if err := s.transactions.Create(context.Background(), txn); err != nil {
			t.Fatal(err)
		}
	}
	body := `{"customer_id":"` + cust.ID + `","external_id":"CURRENT","amount":100,"currency":"JPY","direction":"inbound","executed_at":"` + now.Format(time.RFC3339Nano) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if len(evaluated) != 2 || evaluated[0].ID != "inside" || evaluated[1].ExternalID != "CURRENT" {
		t.Fatalf("evaluated transactions = %+v, want only inside-window and current", evaluated)
	}
}

func TestRealtimeEvaluationUsesTwoBoundedRangesForBackdatedTransaction(t *testing.T) {
	var evaluated []domain.Transaction
	monitoring := &boundedHistoryMonitoring{
		MockMonitoringEngine: &engine.MockMonitoringEngine{EvaluateFunc: func(_ context.Context, _ string, _ domain.RiskTier, transactions []domain.Transaction, _ []string) ([]domain.Alert, error) {
			evaluated = append([]domain.Transaction(nil), transactions...)
			return nil, nil
		}},
		window: 24 * time.Hour,
	}
	s := testServerWithEngine(&engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium}, monitoring)
	cust := createTestCustomer(t, s)
	now := time.Now().UTC()
	backdated := now.Add(-30 * 24 * time.Hour)
	for _, txn := range []*domain.Transaction{
		{ID: "event-before", CustomerID: cust.ID, Amount: 100, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: backdated.Add(-time.Hour), CreatedAt: now.Add(-time.Hour)},
		{ID: "event-after", CustomerID: cust.ID, Amount: 100, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: backdated.Add(time.Hour), CreatedAt: now.Add(-time.Hour)},
		{ID: "middle", CustomerID: cust.ID, Amount: 100, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: now.Add(-15 * 24 * time.Hour), CreatedAt: now.Add(-time.Hour)},
		{ID: "current", CustomerID: cust.ID, Amount: 100, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour)},
	} {
		if err := s.transactions.Create(context.Background(), txn); err != nil {
			t.Fatal(err)
		}
	}
	body := `{"customer_id":"` + cust.ID + `","external_id":"BACKDATED","amount":100,"currency":"JPY","direction":"inbound","executed_at":"` + backdated.Format(time.RFC3339Nano) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var ids []string
	for _, txn := range evaluated {
		ids = append(ids, txn.ID)
	}
	if len(ids) != 4 || ids[0] != "event-before" || evaluated[1].ExternalID != "BACKDATED" || ids[2] != "event-after" || ids[3] != "current" {
		t.Fatalf("evaluated IDs = %v, want event-before, BACKDATED, event-after, current (without middle)", ids)
	}
}

func TestRealtimeMonitoringTimeoutQueuesPendingReview(t *testing.T) {
	pending := store.NewMemoryPendingEvaluationRepo()
	monitoring := &engine.MockMonitoringEngine{EvaluateFunc: func(ctx context.Context, _ string, _ domain.RiskTier, _ []domain.Transaction, _ []string) ([]domain.Alert, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(), Transactions: store.NewMemoryTransactionRepo(), Alerts: store.NewMemoryAlertRepo(),
		Scoring: &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium}, Monitoring: monitoring,
		PendingEvaluations: pending, RealtimeMonitorTimeout: 5 * time.Millisecond,
	})
	cust := createTestCustomer(t, s)
	body := `{"customer_id":"` + cust.ID + `","external_id":"TX-TIMEOUT","amount":100,"currency":"JPY","direction":"inbound"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	queued, err := pending.ListByStatus(context.Background(), domain.PendingEvaluationStatusPendingReview, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 || !strings.Contains(queued[0].Reason, context.DeadlineExceeded.Error()) {
		t.Fatalf("queued = %+v, want one timeout-backed pending review", queued)
	}
}

func TestCreateTransactionMissingCustomer(t *testing.T) {
	s := testServer()

	body := `{"customer_id":"nonexistent","external_id":"TX002","amount":100,"currency":"JPY","direction":"inbound"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateTransactionInvalidAmount(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX003","amount":-100,"direction":"inbound"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListTransactions(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	// Create 2 transactions
	for _, ext := range []string{"TX010", "TX011"} {
		body := `{"customer_id":"` + cust.ID + `","external_id":"` + ext + `","amount":50000,"currency":"JPY","direction":"inbound"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?customer_id="+cust.ID, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	txns, _ := decodeListResponse[domain.Transaction](t, rec.Body)

	if len(txns) != 2 {
		t.Errorf("len = %d, want 2", len(txns))
	}
}

func TestHandleListTransactions_CursorPagination(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	for _, ext := range []string{"TX030", "TX031", "TX032"} {
		body := `{"customer_id":"` + cust.ID + `","external_id":"` + ext + `","amount":1000,"currency":"JPY","direction":"inbound"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?customer_id="+cust.ID+"&limit=2", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	page1, meta1 := decodeListResponse[domain.Transaction](t, rec.Body)
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}
	if !meta1.HasMore || meta1.NextCursor == "" {
		t.Fatal("expected has_more with a next_cursor on first page")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?customer_id="+cust.ID+"&limit=2&cursor="+url.QueryEscape(meta1.NextCursor), nil)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec2.Code, http.StatusOK, rec2.Body.String())
	}

	page2, meta2 := decodeListResponse[domain.Transaction](t, rec2.Body)
	if len(page2) != 1 {
		t.Fatalf("page2 len = %d, want 1", len(page2))
	}
	if meta2.HasMore {
		t.Error("expected has_more = false on second page")
	}
}

func TestListTransactionsMissingCustomerID(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateTransactionInvalidDirection(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX004","amount":100,"direction":"bogus"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateTransactionMissingDirection(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX005","amount":100}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestCreateTransactionIdempotencyKeyConflict verifies the Idempotency-Key
// header (the HTTP API contract §4.1) rejects a resend with the same key, even with a
// different external_id, as a 409 rather than silently creating a second
// transaction.
func TestCreateTransactionIdempotencyKeyConflict(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body1 := `{"customer_id":"` + cust.ID + `","external_id":"TX-IDEMP-1","amount":100,"currency":"JPY","direction":"inbound"}`
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body1))
	req1.Header.Set("Idempotency-Key", "idem-key-1")
	rec1 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first request: status = %d, want %d, body: %s", rec1.Code, http.StatusCreated, rec1.Body.String())
	}

	body2 := `{"customer_id":"` + cust.ID + `","external_id":"TX-IDEMP-2","amount":200,"currency":"JPY","direction":"inbound"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body2))
	req2.Header.Set("Idempotency-Key", "idem-key-1")
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("resend: status = %d, want %d, body: %s", rec2.Code, http.StatusConflict, rec2.Body.String())
	}
}

// TestCreateTransactionWithoutIdempotencyKey verifies omitting the header
// entirely still works (it is optional, not required).
func TestCreateTransactionWithoutIdempotencyKey(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX-NOIDEMP","amount":100,"currency":"JPY","direction":"inbound"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

// TestTransactionAccountIDOptional verifies the pre-existing
// single-customer-account transaction model still works unchanged after
// WS-11 Task 4 adds an optional account_id (the data model §1.1.3).
func TestTransactionAccountIDOptional(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX-NOACCT","amount":1000,"currency":"JPY","direction":"inbound"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var tx domain.Transaction
	json.NewDecoder(rec.Body).Decode(&tx)
	if tx.AccountID != nil {
		t.Errorf("account_id = %v, want nil", *tx.AccountID)
	}
}

func TestTransactionWithAccountIDLinksToAccount(t *testing.T) {
	s, _ := testServerWithAccountsAndTransactions()
	cust := createTestCustomer(t, s)

	accBody := `{"external_id":"TX-ACC-001","account_type":"joint"}`
	accReq := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", strings.NewReader(accBody))
	accRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(accRec, accReq)
	if accRec.Code != http.StatusCreated {
		t.Fatalf("create account status = %d, body: %s", accRec.Code, accRec.Body.String())
	}
	var acc domain.Account
	json.NewDecoder(accRec.Body).Decode(&acc)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX-WITHACCT","amount":1000,"currency":"JPY","direction":"inbound","account_id":"` + acc.ID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var tx domain.Transaction
	json.NewDecoder(rec.Body).Decode(&tx)
	if tx.AccountID == nil || *tx.AccountID != acc.ID {
		t.Errorf("account_id = %v, want %q", tx.AccountID, acc.ID)
	}
}

// TestTransactionIncompleteTravelRuleDoesNotBlockTMEvaluation verifies
// the data model §1.3.1 / Fail-Alert: an incomplete travel-rule record must
// not block transaction creation. WS-4's PENDING_REVIEW queue integration
// (routing incomplete-travel-rule transactions there for TM) is a separate
// concern verified once that queue exists; this test only asserts creation
// itself succeeds.
func TestTransactionIncompleteTravelRuleDoesNotBlockTMEvaluation(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX-TRAVEL-INCOMPLETE","amount":500000,"currency":"JPY","direction":"outbound",` +
		`"counterparty":{"counterparty_type":"unhosted_wallet","originator":{"name":"Taro Yamada","account_number":"123"},"beneficiary":{"account_number":"unknown-wallet"},"travel_rule_status":"incomplete"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var tx domain.Transaction
	json.NewDecoder(rec.Body).Decode(&tx)
	if tx.Counterparty == nil || tx.Counterparty.TravelRuleStatus != domain.TravelRuleIncomplete {
		t.Errorf("counterparty.travel_rule_status = %+v, want incomplete", tx.Counterparty)
	}
}

func TestTransactionMetadataChainAnalysisResultOptional(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX-CHAIN-META","amount":1000,"currency":"JPY","direction":"outbound",` +
		`"metadata":{"chain_analysis_result":{"vendor":"example-vendor","risk_score":12.5,"flags":["mixer"]}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var tx domain.Transaction
	json.NewDecoder(rec.Body).Decode(&tx)
	if tx.Metadata == nil {
		t.Fatal("expected non-nil metadata")
	}
	result, ok := tx.Metadata["chain_analysis_result"].(map[string]any)
	if !ok {
		t.Fatalf("chain_analysis_result = %T, want map[string]any", tx.Metadata["chain_analysis_result"])
	}
	if result["vendor"] != "example-vendor" {
		t.Errorf("vendor = %v, want %q", result["vendor"], "example-vendor")
	}
}

func TestTransactionRejectsInvalidCounterpartyType(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX-BAD-CTYPE","amount":1000,"direction":"outbound",` +
		`"counterparty":{"counterparty_type":"bogus","originator":{},"beneficiary":{},"travel_rule_status":"complete"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestGetTransaction(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX020","amount":75000,"currency":"JPY","direction":"outbound"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Transaction
	json.NewDecoder(rec.Body).Decode(&created)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/transactions/"+created.ID, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got domain.Transaction
	json.NewDecoder(rec.Body).Decode(&got)

	if got.ID != created.ID {
		t.Errorf("id = %q, want %q", got.ID, created.ID)
	}
}
