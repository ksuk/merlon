package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/engine"
	"github.com/merlon-aml/merlon/api/internal/store"
)

// newCustomerStatusTestServer wires a fresh in-memory Server with everything
// the customer-status webhook and its TM/screening side effects touch
// (customers, transactions, alerts, cases, audit).
func newCustomerStatusTestServer(monitoring engine.MonitoringEngine) (*Server, domain.CustomerRepository, domain.AlertRepository) {
	customers := store.NewMemoryCustomerRepo()
	alerts := store.NewMemoryAlertRepo()
	s := New(":0", Deps{
		Customers:    customers,
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       alerts,
		Scoring:      &engine.MockScoringEngine{},
		Monitoring:   monitoring,
		Screening:    &engine.MockScreeningEngine{},
		Backtest:     &engine.MockBacktestEngine{},
		Audit:        store.NewMemoryAuditRepo(),
		Cases:        store.NewMemoryCaseRepo(),
	})
	return s, customers, alerts
}

func newStatusTestCustomer(t *testing.T, repo domain.CustomerRepository, externalID string) *domain.Customer {
	t.Helper()
	c := &domain.Customer{
		ID:           generateID(),
		ExternalID:   externalID,
		CustomerType: domain.CustomerTypeIndividual,
		CountryCode:  "JP",
		Status:       domain.CustomerStatusActive,
		Attributes:   map[string]any{},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := repo.Create(t.Context(), c); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	return c
}

func postCustomerStatusWebhook(s *Server, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/inbound/customer-status", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestCustomerStatusChangeWebhookUpdatesStatus(t *testing.T) {
	s, customers, _ := newCustomerStatusTestServer(&engine.MockMonitoringEngine{})
	c := newStatusTestCustomer(t, customers, "EXT-STATUS-001")

	body := `{"external_id":"EXT-STATUS-001","status":"dormant","reason":"180 days without activity"}`
	rec := postCustomerStatusWebhook(s, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated, err := customers.Get(t.Context(), c.ID)
	if err != nil {
		t.Fatalf("get customer: %v", err)
	}
	if updated.Status != domain.CustomerStatusDormant {
		t.Errorf("status = %q, want %q", updated.Status, domain.CustomerStatusDormant)
	}
}

func TestCustomerStatusChangeRecordsAuditLog(t *testing.T) {
	s, customers, _ := newCustomerStatusTestServer(&engine.MockMonitoringEngine{})
	newStatusTestCustomer(t, customers, "EXT-STATUS-002")

	body := `{"external_id":"EXT-STATUS-002","status":"frozen","reason":"sanctions asset freeze"}`
	rec := postCustomerStatusWebhook(s, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entries, err := s.audit.List(t.Context(), domain.AuditListFilter{ResourceType: "customer", Limit: 50})
	if err != nil {
		t.Fatalf("list audit entries: %v", err)
	}

	var found *domain.AuditEntry
	for i := range entries {
		if entries[i].Action == "customer_status_change" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no customer_status_change audit entry found among %d entries", len(entries))
	}
	if found.Details["old_status"] != "active" {
		t.Errorf("old_status = %q, want %q", found.Details["old_status"], "active")
	}
	if found.Details["new_status"] != "frozen" {
		t.Errorf("new_status = %q, want %q", found.Details["new_status"], "frozen")
	}
	if found.Details["reason"] != "sanctions asset freeze" {
		t.Errorf("reason = %q, want %q", found.Details["reason"], "sanctions asset freeze")
	}
}

func TestCustomerDeathFrozenEscalatesAlertsToHigh(t *testing.T) {
	s, customers, alerts := newCustomerStatusTestServer(&engine.MockMonitoringEngine{})
	c := newStatusTestCustomer(t, customers, "EXT-STATUS-003")

	a := &domain.Alert{
		ID:         generateID(),
		CustomerID: c.ID,
		ScenarioID: "structuring",
		Severity:   domain.AlertSeverityLow,
		Status:     domain.AlertStatusOpen,
		DetectedAt: time.Now(),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := alerts.Create(t.Context(), a); err != nil {
		t.Fatalf("create alert: %v", err)
	}

	body := `{"external_id":"EXT-STATUS-003","status":"frozen","reason":"customer death confirmed by next of kin"}`
	rec := postCustomerStatusWebhook(s, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated, err := alerts.Get(t.Context(), a.ID)
	if err != nil {
		t.Fatalf("get alert: %v", err)
	}
	if updated.Severity != domain.AlertSeverityHigh {
		t.Errorf("severity = %q, want %q", updated.Severity, domain.AlertSeverityHigh)
	}
}

func TestClosedCustomerExcludedFromTMEvaluation(t *testing.T) {
	var evaluateCalls int32
	monitoring := &engine.MockMonitoringEngine{
		EvaluateFunc: func(_ context.Context, _ string, _ domain.RiskTier, _ []domain.Transaction, _ []string) ([]domain.Alert, error) {
			atomic.AddInt32(&evaluateCalls, 1)
			return nil, nil
		},
	}
	s, customers, _ := newCustomerStatusTestServer(monitoring)
	c := newStatusTestCustomer(t, customers, "EXT-STATUS-004")
	c.Status = domain.CustomerStatusClosed
	if err := customers.Update(t.Context(), c); err != nil {
		t.Fatalf("update customer: %v", err)
	}

	tx := &domain.Transaction{
		ID:         generateID(),
		CustomerID: c.ID,
		ExternalID: "TX-CLOSED-1",
		Amount:     1000,
		Currency:   "JPY",
		Direction:  domain.DirectionInbound,
		ExecutedAt: time.Now(),
		CreatedAt:  time.Now(),
	}
	if err := s.transactions.Create(t.Context(), tx); err != nil {
		t.Fatalf("create transaction: %v", err)
	}

	body := `{"customer_ids":["` + c.ID + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/monitor", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if atomic.LoadInt32(&evaluateCalls) != 0 {
		t.Errorf("EvaluateTransactions called %d times, want 0 for a closed customer", evaluateCalls)
	}
}

func TestFrozenCustomerContinuesTMAndAuditLogging(t *testing.T) {
	var evaluateCalls int32
	monitoring := &engine.MockMonitoringEngine{
		EvaluateFunc: func(_ context.Context, _ string, _ domain.RiskTier, _ []domain.Transaction, _ []string) ([]domain.Alert, error) {
			atomic.AddInt32(&evaluateCalls, 1)
			return nil, nil
		},
	}
	s, customers, _ := newCustomerStatusTestServer(monitoring)
	c := newStatusTestCustomer(t, customers, "EXT-STATUS-005")
	c.Status = domain.CustomerStatusFrozen
	if err := customers.Update(t.Context(), c); err != nil {
		t.Fatalf("update customer: %v", err)
	}

	tx := &domain.Transaction{
		ID:         generateID(),
		CustomerID: c.ID,
		ExternalID: "TX-FROZEN-1",
		Amount:     1000,
		Currency:   "JPY",
		Direction:  domain.DirectionInbound,
		ExecutedAt: time.Now(),
		CreatedAt:  time.Now(),
	}
	if err := s.transactions.Create(t.Context(), tx); err != nil {
		t.Fatalf("create transaction: %v", err)
	}

	body := `{"customer_ids":["` + c.ID + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/monitor", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if atomic.LoadInt32(&evaluateCalls) != 1 {
		t.Errorf("EvaluateTransactions called %d times, want 1 for a frozen customer with existing data", evaluateCalls)
	}
}

func TestCustomerStatusWebhookRejectsInvalidStatus(t *testing.T) {
	s, customers, _ := newCustomerStatusTestServer(&engine.MockMonitoringEngine{})
	newStatusTestCustomer(t, customers, "EXT-STATUS-006")

	body := `{"external_id":"EXT-STATUS-006","status":"deceased","reason":"typo"}`
	rec := postCustomerStatusWebhook(s, body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCustomerStatusWebhookUnknownExternalIDReturns404(t *testing.T) {
	s, _, _ := newCustomerStatusTestServer(&engine.MockMonitoringEngine{})

	body := `{"external_id":"does-not-exist","status":"active","reason":"n/a"}`
	rec := postCustomerStatusWebhook(s, body)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
