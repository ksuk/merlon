package store

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// The affected-customer rows and the job's results are written together, so
// a completed job can never page an empty population.
func TestPostgresBacktestCompleteWritesAffectedCustomers(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	repo := NewPgBacktestJobRepo(pool)
	customerA, customerB := seedTestCustomer(t, pool), seedTestCustomer(t, pool)

	jobID := newTestUUID()
	job := &domain.BacktestJob{
		ID: jobID, Status: domain.BacktestJobQueued,
		From: time.Now().UTC().Add(-24 * time.Hour), To: time.Now().UTC(),
		BaselineRuleSetID: "active", CandidateRuleSetID: "candidate", SnapshotAt: time.Now().UTC(),
	}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM backtest_job_affected_customers WHERE job_id=$1`, jobID)
		_, _ = pool.Exec(ctx, `DELETE FROM backtest_jobs WHERE id=$1`, jobID)
	})
	if _, err := pool.Exec(ctx, `UPDATE backtest_jobs SET status='running' WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}

	candidate := &domain.BacktestResult{ScenarioResults: []domain.BacktestScenarioResult{
		{ScenarioID: "structuring", AffectedCustomerIDs: []string{customerA}},
	}}
	delta := &domain.BacktestResult{ScenarioResults: []domain.BacktestScenarioResult{
		{ScenarioID: "structuring", AddedCustomerIDs: []string{customerA}, RemovedCustomerIDs: []string{customerB}},
	}}
	if err := repo.Complete(ctx, jobID, &domain.BacktestResult{}, candidate, delta); err != nil {
		t.Fatal(err)
	}

	rows, err := repo.ListBacktestAffectedCustomers(ctx, domain.BacktestAffectedCustomerFilter{JobID: jobID}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want one per affected customer", rows)
	}
	kinds := map[string]domain.BacktestDeltaKind{}
	for _, row := range rows {
		kinds[row.CustomerID] = row.DeltaKind
	}
	// Stored ids are canonical (dash-free), so compare in that form.
	if kinds[domain.CanonicalUUID(customerA)] != domain.BacktestDeltaAdded {
		t.Errorf("customer A = %q, want added", kinds[domain.CanonicalUUID(customerA)])
	}
	if kinds[domain.CanonicalUUID(customerB)] != domain.BacktestDeltaRemoved {
		t.Errorf("customer B = %q, want removed", kinds[domain.CanonicalUUID(customerB)])
	}

	count, err := repo.CountBacktestAffectedCustomers(ctx, domain.BacktestAffectedCustomerFilter{JobID: jobID})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	// Keyset paging: everything after the first customer id.
	after, err := repo.ListBacktestAffectedCustomers(ctx, domain.BacktestAffectedCustomerFilter{JobID: jobID, AfterCustomerID: rows[0].CustomerID}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].CustomerID == rows[0].CustomerID {
		t.Fatalf("keyset page after %s = %+v, want only later customers", rows[0].CustomerID, after)
	}

	// A completed job is terminal: a second Complete is a no-op that must not
	// duplicate or wipe the rows.
	if err := repo.Complete(ctx, jobID, &domain.BacktestResult{}, &domain.BacktestResult{}, &domain.BacktestResult{}); err != nil {
		t.Fatal(err)
	}
	unchanged, err := repo.CountBacktestAffectedCustomers(ctx, domain.BacktestAffectedCustomerFilter{JobID: jobID})
	if err != nil {
		t.Fatal(err)
	}
	if unchanged != 2 {
		t.Fatalf("count after a repeated completion = %d, want 2", unchanged)
	}
}

// Purging the job removes its outcome rows: they have no meaning without it.
func TestPostgresBacktestAffectedCustomersCascadeWithTheJob(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	repo := NewPgBacktestJobRepo(pool)
	customer := seedTestCustomer(t, pool)
	jobID := newTestUUID()
	if err := repo.Create(ctx, &domain.BacktestJob{ID: jobID, Status: domain.BacktestJobQueued, From: time.Now().UTC().Add(-time.Hour), To: time.Now().UTC(), BaselineRuleSetID: "active", CandidateRuleSetID: "candidate", SnapshotAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE backtest_jobs SET status='running' WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Complete(ctx, jobID, nil, &domain.BacktestResult{ScenarioResults: []domain.BacktestScenarioResult{{ScenarioID: "s", AffectedCustomerIDs: []string{customer}}}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM backtest_jobs WHERE id=$1`, jobID); err != nil {
		t.Fatalf("deleting the job was blocked by its outcome rows: %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM backtest_job_affected_customers WHERE job_id=$1`, jobID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("rows left after the job was purged = %d, want 0", remaining)
	}
}
