package store

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// The memory store matches transaction_ids in Go and PostgreSQL matches it in
// SQL. Two implementations of one filter is how a queue and its production
// backend come to disagree, so this asserts the PostgreSQL side answers the
// same question -- including the case that matters most, an id that matches
// nothing returning nothing rather than everything.
func TestPostgresQueueFiltersByTransactionID(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	var customerID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
		 VALUES ($1, 'individual', 'JP', '{}', '{}') RETURNING id`, "txn-filter-"+newTestUUID(),
	).Scan(&customerID); err != nil {
		t.Fatalf("create customer: %v", err)
	}

	created := time.Date(2099, 8, 1, 0, 0, 0, 0, time.UTC)
	wantedTxn := newTestUUID()
	otherTxn := newTestUUID()
	matchingAlert := newTestUUID()
	unrelatedAlert := newTestUUID()
	prefix := newTestUUID()
	linkedCase := "txn-filter-" + prefix + "-linked"
	unlinkedCase := "txn-filter-" + prefix + "-unlinked"

	for _, row := range []struct {
		alertID string
		txnID   string
	}{{matchingAlert, wantedTxn}, {unrelatedAlert, otherTxn}} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO alerts (id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, created_at, updated_at)
			 VALUES ($1, $2, 'txn_filter_test', 'high', 'open', 1, '', ARRAY[$3]::uuid[], $4, $4, $4)`,
			row.alertID, customerID, row.txnID, created); err != nil {
			t.Fatalf("create alert: %v", err)
		}
	}
	for _, row := range []struct {
		caseID  string
		alertID string
	}{{linkedCase, matchingAlert}, {unlinkedCase, unrelatedAlert}} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO cases (id, customer_id, alert_ids, status, priority, assigned_to, summary, created_at, updated_at)
			 VALUES ($1, $2, ARRAY[$3]::text[], 'investigating', 'high', '', 'txn filter test', $4, $4)`,
			row.caseID, customerID, row.alertID, created); err != nil {
			t.Fatalf("create case: %v", err)
		}
	}
	t.Cleanup(func() {
		bg := context.Background()
		pool.Exec(bg, `DELETE FROM cases WHERE id = ANY($1)`, []string{linkedCase, unlinkedCase})
		pool.Exec(bg, `DELETE FROM alerts WHERE id = ANY($1::uuid[])`, []string{matchingAlert, unrelatedAlert})
		pool.Exec(bg, `DELETE FROM customers WHERE id = $1`, customerID)
	})

	alertRepo := NewPgAlertRepo(pool)
	alerts, err := alertRepo.ListQueue(ctx, domain.AlertQueueFilter{TransactionID: wantedTxn, AsOf: time.Now().UTC()}, 100, 0)
	if err != nil {
		t.Fatalf("ListQueue by transaction: %v", err)
	}
	if len(alerts) != 1 || alerts[0].ID != matchingAlert {
		t.Fatalf("alerts for transaction = %v, want only %s", alertIDsOf(alerts), matchingAlert)
	}

	// The keyset variant shares the builder; assert it agrees rather than
	// trusting that it still does.
	cursored, err := alertRepo.ListQueueCursor(ctx, domain.AlertQueueFilter{TransactionID: wantedTxn, AsOf: time.Now().UTC()}, 100, nil)
	if err != nil {
		t.Fatalf("ListQueueCursor by transaction: %v", err)
	}
	if len(cursored) != 1 || cursored[0].ID != matchingAlert {
		t.Fatalf("cursored alerts for transaction = %v, want only %s", alertIDsOf(cursored), matchingAlert)
	}

	unknown, err := alertRepo.ListQueue(ctx, domain.AlertQueueFilter{TransactionID: newTestUUID(), AsOf: time.Now().UTC()}, 100, 0)
	if err != nil {
		t.Fatalf("ListQueue by unknown transaction: %v", err)
	}
	if len(unknown) != 0 {
		t.Fatalf("alerts for an unknown transaction = %v, want none", alertIDsOf(unknown))
	}

	caseRepo := NewPgCaseRepo(pool)
	cases, err := caseRepo.ListQueue(ctx, domain.CaseQueueFilter{AlertIDs: []string{matchingAlert}, AsOf: time.Now().UTC()}, 100, 0)
	if err != nil {
		t.Fatalf("ListQueue by alert ids: %v", err)
	}
	if len(cases) != 1 || cases[0].ID != linkedCase {
		t.Fatalf("cases for alert = %v, want only %s", caseIDsOf(cases), linkedCase)
	}

	cursoredCases, err := caseRepo.ListQueueCursor(ctx, domain.CaseQueueFilter{AlertIDs: []string{matchingAlert}, AsOf: time.Now().UTC()}, 100, nil)
	if err != nil {
		t.Fatalf("ListQueueCursor by alert ids: %v", err)
	}
	if len(cursoredCases) != 1 || cursoredCases[0].ID != linkedCase {
		t.Fatalf("cursored cases for alert = %v, want only %s", caseIDsOf(cursoredCases), linkedCase)
	}
}

func alertIDsOf(alerts []domain.Alert) []string {
	ids := make([]string, 0, len(alerts))
	for _, a := range alerts {
		ids = append(ids, a.ID)
	}
	return ids
}

func caseIDsOf(cases []domain.Case) []string {
	ids := make([]string, 0, len(cases))
	for _, c := range cases {
		ids = append(ids, c.ID)
	}
	return ids
}

// The memory store aggregates in Go and PostgreSQL aggregates in SQL. The
// backlog is a stop condition an operator acts on, so the two must agree.
//
// The aggregate is global by design and the test database is shared, so the
// assertion is on the delta this test's own rows introduce.
func TestPostgresPendingEvaluationStatsMatchMemory(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	pgRepo := NewPgPendingEvaluationRepo(pool)

	var customerID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
		 VALUES ($1, 'individual', 'JP', '{}', '{}') RETURNING id`, "pending-stats-"+newTestUUID(),
	).Scan(&customerID); err != nil {
		t.Fatalf("create customer: %v", err)
	}

	now := time.Now().UTC()
	before, err := pgRepo.PendingEvaluationStats(ctx, now)
	if err != nil {
		t.Fatalf("baseline stats: %v", err)
	}

	seed := []struct {
		id      string
		status  domain.PendingEvaluationStatus
		age     time.Duration
		retries int
	}{
		{newTestUUID(), domain.PendingEvaluationStatusPendingReview, 72 * time.Hour, 0},
		{newTestUUID(), domain.PendingEvaluationStatusPendingReview, 2 * time.Hour, 5},
		{newTestUUID(), domain.PendingEvaluationStatusFailed, 48 * time.Hour, 5},
		{newTestUUID(), domain.PendingEvaluationStatusResolved, 96 * time.Hour, 1},
	}
	ids := make([]string, 0, len(seed))
	memory := NewMemoryPendingEvaluationRepo()
	for _, row := range seed {
		ids = append(ids, row.id)
		created := now.Add(-row.age)
		if _, err := pool.Exec(ctx,
			`INSERT INTO pending_evaluations (id, customer_id, transaction_ids, status, reason, retry_count, created_at, updated_at)
			 VALUES ($1, $2, '{}', $3, 'engine unavailable', $4, $5, $5)`,
			row.id, customerID, string(row.status), row.retries, created); err != nil {
			t.Fatalf("create pending evaluation: %v", err)
		}
		if err := memory.Create(ctx, &domain.PendingEvaluation{
			ID: row.id, CustomerID: customerID, Status: row.status,
			Reason: "engine unavailable", RetryCount: row.retries,
			CreatedAt: created, UpdatedAt: created,
		}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		bg := context.Background()
		pool.Exec(bg, `DELETE FROM pending_evaluations WHERE id = ANY($1::uuid[])`, ids)
		pool.Exec(bg, `DELETE FROM customers WHERE id = $1`, customerID)
	})

	after, err := pgRepo.PendingEvaluationStats(ctx, now)
	if err != nil {
		t.Fatalf("postgres stats: %v", err)
	}
	memStats, err := memory.PendingEvaluationStats(ctx, now)
	if err != nil {
		t.Fatalf("memory stats: %v", err)
	}

	if got := after.Backlog - before.Backlog; got != memStats.Backlog || got != 3 {
		t.Errorf("backlog delta postgres=%d memory=%d, want 3 (2 pending + 1 failed, resolved excluded)", got, memStats.Backlog)
	}
	if got := after.Failed - before.Failed; got != memStats.Failed || got != 1 {
		t.Errorf("failed delta postgres=%d memory=%d, want 1", got, memStats.Failed)
	}
	if got := after.Exhausted - before.Exhausted; got != memStats.Exhausted || got != 2 {
		t.Errorf("exhausted delta postgres=%d memory=%d, want 2", got, memStats.Exhausted)
	}
	// The global oldest is at least as old as the oldest row this test added.
	if after.OldestAgeSeconds < memStats.OldestAgeSeconds {
		t.Errorf("oldest_age_seconds postgres=%d is younger than this test's oldest unresolved row (%d)", after.OldestAgeSeconds, memStats.OldestAgeSeconds)
	}
	if after.OldestCreatedAt == nil {
		t.Error("oldest_created_at is nil with unresolved rows present")
	}
}
