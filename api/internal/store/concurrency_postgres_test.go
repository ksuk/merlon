package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/domain"
)

func TestPostgresTwoClientOptimisticLockingAllowsExactlyOneCaseUpdate(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	caseID := "two-client-case-" + newTestUUID()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO cases (id, customer_id, alert_ids, status, priority, summary, created_at, updated_at)
		VALUES ($1,$2,'{}','investigating','medium','original',$3,$3)`, caseID, customerID, now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM cases WHERE id=$1`, caseID) })

	conn1 := acquirePgConn(t, pool)
	conn2 := acquirePgConn(t, pool)
	caseRepo1 := NewPgCaseRepo(conn1)
	caseRepo2 := NewPgCaseRepo(conn2)
	first, err := caseRepo1.Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := caseRepo2.Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if !first.UpdatedAt.Equal(second.UpdatedAt) {
		t.Fatalf("clients did not read the same version: %s vs %s", first.UpdatedAt, second.UpdatedAt)
	}
	first.Summary = "client-one"
	second.Summary = "client-two"
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, item := range []struct {
		repo       *PgCaseRepo
		caseRecord *domain.Case
	}{
		{caseRepo1, first},
		{caseRepo2, second},
	} {
		wg.Add(1)
		go func(repo *PgCaseRepo, record *domain.Case) {
			defer wg.Done()
			errs <- repo.UpdateIfUnmodified(ctx, record, now)
		}(item.repo, item.caseRecord)
	}
	wg.Wait()
	close(errs)
	var successes, conflicts int
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		var conflict *domain.ErrConflict
		if errors.As(err, &conflict) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected optimistic-lock error: %v", err)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("two-client results = successes %d conflicts %d, want 1/1", successes, conflicts)
	}
}

func TestPostgresEvidenceCorrectionHasExactlyOneConcurrentVersion(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	caseID := "two-client-evidence-" + newTestUUID()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO cases (id, customer_id, alert_ids, status, priority, summary, created_at, updated_at)
		VALUES ($1,$2,'{}','investigating','medium','evidence case',$3,$3)`, caseID, customerID, now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM cases WHERE id=$1`, caseID) })

	conn1 := acquirePgConn(t, pool)
	conn2 := acquirePgConn(t, pool)
	repo1 := NewPgCaseInvestigationRepo(conn1)
	repo2 := NewPgCaseInvestigationRepo(conn2)
	v1 := &domain.CaseEvidence{ID: "evidence-v1-" + newTestUUID(), CaseID: caseID, Description: "v1", Source: "source", EvidenceType: "document", CollectedAt: now, CollectedBy: "analyst", Version: 1, CreatedAt: now}
	if err := repo1.AddEvidence(ctx, v1); err != nil {
		t.Fatal(err)
	}
	corrections := []*domain.CaseEvidence{
		{ID: "evidence-v2-a-" + newTestUUID(), CaseID: caseID, RootID: v1.ID, SupersedesID: v1.ID, Description: "v2-a", Source: "source", EvidenceType: "document", CollectedAt: now, CollectedBy: "analyst-a", Version: 2, CreatedAt: now.Add(time.Second)},
		{ID: "evidence-v2-b-" + newTestUUID(), CaseID: caseID, RootID: v1.ID, SupersedesID: v1.ID, Description: "v2-b", Source: "source", EvidenceType: "document", CollectedAt: now, CollectedBy: "analyst-b", Version: 2, CreatedAt: now.Add(time.Second)},
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, correction := range corrections {
		wg.Add(1)
		go func(i int, item *domain.CaseEvidence) {
			defer wg.Done()
			if i == 0 {
				errs <- repo1.CorrectEvidence(ctx, item, v1.ID)
				return
			}
			errs <- repo2.CorrectEvidence(ctx, item, v1.ID)
		}(i, correction)
	}
	wg.Wait()
	close(errs)
	var successes, conflicts int
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		var conflict *domain.ErrConflict
		if errors.As(err, &conflict) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected evidence correction error: %v", err)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent corrections = successes %d conflicts %d, want 1/1", successes, conflicts)
	}
	items, err := repo1.ListEvidence(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Version != 1 || items[1].Version != 2 {
		t.Fatalf("evidence after concurrent correction = %+v, want exactly v1 and v2", items)
	}
	winner := items[1]
	v3 := &domain.CaseEvidence{ID: "evidence-v3-" + newTestUUID(), CaseID: caseID, RootID: v1.ID, SupersedesID: winner.ID, Description: "v3", Source: "source", EvidenceType: "document", CollectedAt: now, CollectedBy: "analyst", Version: 3, CreatedAt: now.Add(2 * time.Second)}
	if err := repo2.CorrectEvidence(ctx, v3, winner.ID); err != nil {
		t.Fatalf("retry correction: %v", err)
	}
	items, err = repo1.ListEvidence(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[2].Version != 3 || items[2].SupersedesID != winner.ID {
		t.Fatalf("evidence after retry = %+v, want v1/v2/v3 lineage", items)
	}
}

func acquirePgConn(t *testing.T, pool *pgxpool.Pool) *pgxpool.Conn {
	t.Helper()
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Release)
	return conn
}
