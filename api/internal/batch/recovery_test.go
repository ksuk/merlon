package batch

import (
	"context"
	"errors"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

func seedPendingCustomerAndTransaction(t *testing.T, customers domain.CustomerRepository, transactions domain.TransactionRepository, externalID string) (*domain.Customer, *domain.Transaction) {
	t.Helper()
	riskTier := domain.RiskTierMedium
	c := &domain.Customer{
		ID:           "cust-" + externalID,
		ExternalID:   externalID,
		CustomerType: domain.CustomerTypeIndividual,
		CountryCode:  "JP",
		RiskTier:     &riskTier,
	}
	if err := customers.Create(context.Background(), c); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	tx := &domain.Transaction{
		ID:         "tx-" + externalID,
		CustomerID: c.ID,
		ExternalID: "TX_" + externalID,
		Amount:     100000,
		Currency:   "JPY",
		Direction:  domain.DirectionInbound,
	}
	if err := transactions.Create(context.Background(), tx); err != nil {
		t.Fatalf("seed transaction: %v", err)
	}
	return c, tx
}

// TestRecoveryJob_ProcessesPendingReviewOnEngineRecovery verifies the
// acceptance criterion "engine outage queues PENDING_REVIEW, recovery
// re-evaluates automatically": a PENDING_REVIEW record created while the
// engine was down is resolved and generates alerts once RunOnce is called
// against a now-healthy engine.
func TestRecoveryJob_ProcessesPendingReviewOnEngineRecovery(t *testing.T) {
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	alerts := store.NewMemoryAlertRepo()
	pending := store.NewMemoryPendingEvaluationRepo()

	c, tx := seedPendingCustomerAndTransaction(t, customers, transactions, "RJ001")

	pe := &domain.PendingEvaluation{
		ID:             "pe1",
		CustomerID:     c.ID,
		TransactionIDs: []string{tx.ID},
		Status:         domain.PendingEvaluationStatusPendingReview,
		Reason:         "engine unavailable: deadline exceeded",
	}
	if err := pending.Create(ctx, pe); err != nil {
		t.Fatalf("Create: %v", err)
	}

	monitoring := &engine.MockMonitoringEngine{
		Alerts: []domain.Alert{{CustomerID: c.ID, ScenarioID: "S1", Severity: domain.AlertSeverityHigh, Description: "test"}},
	}

	job := NewRecoveryJob(pending, monitoring, alerts, transactions, customers)

	processed, err := job.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if processed != 1 {
		t.Errorf("processed = %d, want 1", processed)
	}

	got, err := pending.Get(ctx, pe.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.PendingEvaluationStatusResolved {
		t.Errorf("status = %s, want %s", got.Status, domain.PendingEvaluationStatusResolved)
	}
	if got.ResolvedAt == nil {
		t.Error("expected ResolvedAt to be set")
	}

	createdAlerts, err := alerts.ListByCustomer(ctx, c.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListByCustomer: %v", err)
	}
	if len(createdAlerts) != 1 {
		t.Fatalf("alerts count = %d, want 1", len(createdAlerts))
	}
}

// TestRecoveryJob_LeavesFailedStatusOnRepeatedFailure verifies that repeated
// re-evaluation failures increment retry_count and, past the retry limit,
// transition the record to FAILED rather than retrying forever.
func TestRecoveryJob_LeavesFailedStatusOnRepeatedFailure(t *testing.T) {
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	alerts := store.NewMemoryAlertRepo()
	pending := store.NewMemoryPendingEvaluationRepo()

	c, tx := seedPendingCustomerAndTransaction(t, customers, transactions, "RJ002")

	pe := &domain.PendingEvaluation{
		ID:             "pe2",
		CustomerID:     c.ID,
		TransactionIDs: []string{tx.ID},
		Status:         domain.PendingEvaluationStatusPendingReview,
		Reason:         "engine unavailable: deadline exceeded",
	}
	if err := pending.Create(ctx, pe); err != nil {
		t.Fatalf("Create: %v", err)
	}

	monitoring := &engine.MockMonitoringEngine{Err: errors.New("engine still unavailable")}
	job := NewRecoveryJob(pending, monitoring, alerts, transactions, customers)

	for i := 0; i < maxPendingRetries; i++ {
		if _, err := job.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce iteration %d: %v", i, err)
		}
		got, err := pending.Get(ctx, pe.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if i < maxPendingRetries-1 && got.Status != domain.PendingEvaluationStatusPendingReview {
			t.Fatalf("iteration %d: status = %s, want %s (not yet at retry limit)", i, got.Status, domain.PendingEvaluationStatusPendingReview)
		}
	}

	got, err := pending.Get(ctx, pe.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.PendingEvaluationStatusFailed {
		t.Errorf("status = %s, want %s after exceeding retry limit", got.Status, domain.PendingEvaluationStatusFailed)
	}
	if got.RetryCount != maxPendingRetries {
		t.Errorf("RetryCount = %d, want %d", got.RetryCount, maxPendingRetries)
	}
}
