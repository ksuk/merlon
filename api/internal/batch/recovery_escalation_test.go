package batch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

// failingMonitoringEngine always fails, which is what a permanently broken
// record looks like to the recovery job.
type failingMonitoringEngine struct{ calls int }

func (e *failingMonitoringEngine) EvaluateTransactions(context.Context, string, domain.RiskTier, []domain.Transaction, []string) ([]domain.Alert, error) {
	e.calls++
	return nil, errors.New("monitoring engine unavailable")
}

func (e *failingMonitoringEngine) EvaluateTransactionsBatch(context.Context, string, domain.RiskTier, []domain.Transaction, []string) ([]domain.Alert, error) {
	e.calls++
	return nil, errors.New("monitoring engine unavailable")
}

func newAtomicRecoveryFixture(t *testing.T) (*store.MemoryPendingEvaluationRepo, *store.MemoryAuditRepo, *store.MemoryEventOutboxRepo, domain.AtomicMutationRepository, *store.MemoryCustomerRepo, *store.MemoryTransactionRepo, *store.MemoryAlertRepo) {
	t.Helper()
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	alerts := store.NewMemoryAlertRepo()
	pending := store.NewMemoryPendingEvaluationRepo()
	audit := store.NewMemoryAuditRepo()
	outbox := store.NewMemoryEventOutboxRepo()
	atomic, err := store.NewMemoryAtomicMutationRepo(domain.AtomicMutationRepositories{
		Customers: customers, Transactions: transactions, Alerts: alerts, Audit: audit,
		EventOutbox: outbox, PendingEvaluations: pending,
	})
	if err != nil {
		t.Fatal(err)
	}
	return pending, audit, outbox, atomic, customers, transactions, alerts
}

// The dead-letter branch had no test at all: the existing failure test drives
// the legacy non-atomic path, which never reaches the escalate transition.
func TestRecoveryJob_EscalatesAfterTheAutomaticRetryBudgetIsSpent(t *testing.T) {
	ctx := context.Background()
	pending, audit, outbox, atomic, customers, transactions, alerts := newAtomicRecoveryFixture(t)
	c, tx := seedPendingCustomerAndTransaction(t, customers, transactions, "ESC001")
	pe := &domain.PendingEvaluation{ID: "pe-escalate", CustomerID: c.ID, TransactionIDs: []string{tx.ID}, Status: domain.PendingEvaluationStatusPendingReview, Reason: "engine unavailable"}
	if err := pending.Create(ctx, pe); err != nil {
		t.Fatal(err)
	}
	monitoring := &failingMonitoringEngine{}
	job := NewRecoveryJob(pending, monitoring, alerts, transactions, customers)
	job.SetPersistence(atomic, audit, outbox)

	for attempt := 1; attempt <= maxPendingRetries; attempt++ {
		if _, err := job.RunOnce(ctx); err == nil {
			t.Fatalf("attempt %d succeeded; the engine always fails", attempt)
		}
		got, err := pending.Get(ctx, pe.ID)
		if err != nil {
			t.Fatal(err)
		}
		if attempt < maxPendingRetries {
			if got.Status != domain.PendingEvaluationStatusPendingReview {
				t.Fatalf("attempt %d status = %q, want the record still queued", attempt, got.Status)
			}
			if got.EscalatedAt != nil {
				t.Fatalf("attempt %d escalated early", attempt)
			}
		}
	}

	failed, err := pending.Get(ctx, pe.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != domain.PendingEvaluationStatusFailed {
		t.Fatalf("status after %d failures = %q, want FAILED", maxPendingRetries, failed.Status)
	}
	if failed.EscalatedAt == nil {
		t.Fatal("EscalatedAt was not recorded; a dead-lettered gap must be visibly escalated")
	}
	if failed.RetryCount < maxPendingRetries {
		t.Fatalf("retry_count = %d, want at least %d", failed.RetryCount, maxPendingRetries)
	}

	// Once dead-lettered, the sweep must stop picking it up -- otherwise the
	// budget means nothing.
	callsBefore := monitoring.calls
	processed, err := job.RunOnce(ctx)
	if err != nil {
		t.Fatalf("sweep after escalation returned %v, want no work and no error", err)
	}
	if processed != 0 || monitoring.calls != callsBefore {
		t.Fatalf("a FAILED record was retried again: processed=%d engine calls %d -> %d", processed, callsBefore, monitoring.calls)
	}

	// The escalation is evidence, so it is audited and published exactly once
	// per failure, and the last one records the terminal state.
	entries, err := audit.List(ctx, domain.AuditListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	escalationAudits := 0
	for _, entry := range entries {
		if entry.Action == "pending_evaluation_recovery_failed" && entry.Details["status"] == string(domain.PendingEvaluationStatusFailed) {
			escalationAudits++
		}
	}
	if escalationAudits != 1 {
		t.Fatalf("audit records naming the FAILED transition = %d, want 1", escalationAudits)
	}
	events, err := outbox.ListPending(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	failureEvents := 0
	for _, event := range events {
		if event.Topic == "pending.evaluation.recovery_failed" {
			failureEvents++
		}
	}
	if failureEvents != maxPendingRetries {
		t.Fatalf("recovery_failed events = %d, want one per failed attempt (%d)", failureEvents, maxPendingRetries)
	}

	history, err := pending.ListPendingHistory(ctx, pe.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if history[len(history)-1].Action != "escalate" {
		t.Fatalf("last history action = %q, want escalate", history[len(history)-1].Action)
	}
}

// A backlog deeper than one page used to leave its tail unprocessed forever:
// the sweep read a fixed first page of a newest-first list, so the oldest
// records -- the ones that had waited longest -- were never reached.
func TestRecoveryJob_DrainsABacklogDeeperThanOnePage(t *testing.T) {
	ctx := context.Background()
	pending, audit, outbox, atomic, customers, transactions, alerts := newAtomicRecoveryFixture(t)

	const total = 250
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	oldest := ""
	for i := range total {
		c, tx := seedPendingCustomerAndTransaction(t, customers, transactions, fmt.Sprintf("BULK%04d", i))
		id := fmt.Sprintf("pe-bulk-%04d", i)
		if i == 0 {
			oldest = id
		}
		if err := pending.Create(ctx, &domain.PendingEvaluation{
			ID: id, CustomerID: c.ID, TransactionIDs: []string{tx.ID},
			Status: domain.PendingEvaluationStatusPendingReview, Reason: "engine unavailable",
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}

	// The queue must hand back the oldest record first: a monitoring gap that
	// has waited longest is the most urgent, not the least.
	head, err := pending.ListByStatus(ctx, domain.PendingEvaluationStatusPendingReview, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(head) != 1 || head[0].ID != oldest {
		t.Fatalf("queue head = %+v, want the oldest record %s", head, oldest)
	}

	job := NewRecoveryJob(pending, &engine.MockMonitoringEngine{}, alerts, transactions, customers)
	job.SetPersistence(atomic, audit, outbox)

	processed, err := job.RunOnce(ctx)
	if err != nil {
		t.Fatalf("sweep returned %v", err)
	}
	if processed != total {
		t.Fatalf("processed = %d, want all %d queued records in one sweep", processed, total)
	}
	remaining, err := pending.ListByStatus(ctx, domain.PendingEvaluationStatusPendingReview, 1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("%d records left queued after the sweep", len(remaining))
	}
}

// A backlog beyond one sweep's bound is drained across successive sweeps,
// oldest first, rather than growing a permanently unreachable tail.
func TestRecoveryJob_BoundsOneSweepAndResumesOnTheNext(t *testing.T) {
	ctx := context.Background()
	pending, audit, outbox, atomic, customers, transactions, alerts := newAtomicRecoveryFixture(t)

	total := recoveryPageSize*maxRecoveryPagesPerRun + 5
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	for i := range total {
		c, tx := seedPendingCustomerAndTransaction(t, customers, transactions, fmt.Sprintf("DEEP%05d", i))
		if err := pending.Create(ctx, &domain.PendingEvaluation{
			ID: fmt.Sprintf("pe-deep-%05d", i), CustomerID: c.ID, TransactionIDs: []string{tx.ID},
			Status: domain.PendingEvaluationStatusPendingReview, Reason: "engine unavailable",
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}

	job := NewRecoveryJob(pending, &engine.MockMonitoringEngine{}, alerts, transactions, customers)
	job.SetPersistence(atomic, audit, outbox)

	first, err := job.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first != recoveryPageSize*maxRecoveryPagesPerRun {
		t.Fatalf("first sweep processed %d, want the per-sweep bound %d", first, recoveryPageSize*maxRecoveryPagesPerRun)
	}
	second, err := job.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second != 5 {
		t.Fatalf("second sweep processed %d, want the remaining 5", second)
	}
}
