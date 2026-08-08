package store

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestPostgresAtomicMutationRollsBackBusinessRowWhenEventInsertFails(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	caseID := "atomic-pg-" + newTestUUID()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO cases (id, customer_id, alert_ids, status, priority, summary, created_at, updated_at)
		VALUES ($1,$2,'{}','investigating','medium','before',$3,$3)`, caseID, customerID, now); err != nil {
		t.Fatal(err)
	}
	eventID := "atomic-pg-event-" + newTestUUID()
	if err := NewPgCaseInvestigationRepo(pool).AppendEvent(ctx, &domain.CaseEvent{ID: eventID, CaseID: caseID, EventType: "seed", Actor: "test", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM cases WHERE id=$1`, caseID)
	})

	err := NewPgAtomicMutationRepo(pool).RunAtomic(ctx, func(repos domain.AtomicMutationRepositories) error {
		caseRecord, err := repos.Cases.Get(ctx, caseID)
		if err != nil {
			return err
		}
		caseRecord.Summary = "must rollback"
		if err := repos.Cases.Update(ctx, caseRecord); err != nil {
			return err
		}
		// Duplicate immutable event ID is a real database constraint failure
		// after the business UPDATE has already executed in this transaction.
		return repos.Investigation.AppendEvent(ctx, &domain.CaseEvent{ID: eventID, CaseID: caseID, EventType: "duplicate", Actor: "test", CreatedAt: now})
	})
	if err == nil {
		t.Fatal("atomic mutation with duplicate event unexpectedly committed")
	}
	stored, err := NewPgCaseRepo(pool).Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Summary != "before" {
		t.Fatalf("case summary after event failure = %q, want before", stored.Summary)
	}
	events, err := NewPgCaseInvestigationRepo(pool).ListEvents(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != eventID {
		t.Fatalf("case events after rollback = %+v, want only seed event", events)
	}
}

func TestPostgresAtomicMutationRollsBackBusinessRowWhenOutboxInsertFails(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	caseID := "atomic-outbox-pg-" + newTestUUID()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO cases (id, customer_id, alert_ids, status, priority, summary, created_at, updated_at)
		VALUES ($1,$2,'{}','investigating','medium','before',$3,$3)`, caseID, customerID, now); err != nil {
		t.Fatal(err)
	}
	eventID := "atomic-outbox-event-" + newTestUUID()
	if err := NewPgEventOutboxRepo(pool).Enqueue(ctx, &domain.DurableEvent{
		ID: eventID, Topic: "test", Payload: []byte(`{"kind":"seed"}`), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM domain_event_outbox WHERE id=$1`, eventID)
		_, _ = pool.Exec(ctx, `DELETE FROM cases WHERE id=$1`, caseID)
	})

	err := NewPgAtomicMutationRepo(pool).RunAtomic(ctx, func(repos domain.AtomicMutationRepositories) error {
		caseRecord, err := repos.Cases.Get(ctx, caseID)
		if err != nil {
			return err
		}
		caseRecord.Summary = "must rollback"
		if err := repos.Cases.Update(ctx, caseRecord); err != nil {
			return err
		}
		return repos.EventOutbox.Enqueue(ctx, &domain.DurableEvent{
			ID: eventID, Topic: "test", Payload: []byte(`{"kind":"duplicate"}`), CreatedAt: now,
		})
	})
	if err == nil {
		t.Fatal("atomic mutation with duplicate outbox event unexpectedly committed")
	}
	stored, err := NewPgCaseRepo(pool).Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Summary != "before" {
		t.Fatalf("case summary after outbox failure = %q, want before", stored.Summary)
	}
	pending, err := NewPgEventOutboxRepo(pool).ListPending(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range pending {
		if event.ID == eventID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("outbox rows after rollback = %d, want one seed event", count)
	}
}
