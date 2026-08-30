package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func dashboardWorkloadTestTx(t *testing.T) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := newTestPgPool(t).Begin(ctx)
	if err != nil {
		t.Fatalf("begin dashboard workload transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	if _, err := tx.Exec(ctx, `TRUNCATE TABLE alerts, cases CASCADE`); err != nil {
		t.Fatalf("truncate dashboard queues: %v", err)
	}
	return tx
}

type dashboardWorkloadTestQueue struct {
	name      string
	query     workloadQuery
	insertSQL string
	newID     func() string
}

func dashboardWorkloadTestQueues() []dashboardWorkloadTestQueue {
	return []dashboardWorkloadTestQueue{
		{
			name: "alerts",
			query: workloadQuery{
				table:      "alerts",
				openedCol:  "detected_at",
				openFilter: "status NOT IN ('closed_true_positive', 'closed_false_positive')",
			},
			insertSQL: `
				INSERT INTO alerts (
					id, customer_id, scenario_id, severity, status, detected_at,
					assigned_to, assigned_team, due_at
				) VALUES ($1, $2, 'dashboard-boundary', 'medium', 'open', $3, $4, $5, $6)`,
			newID: newTestUUID,
		},
		{
			name: "cases",
			query: workloadQuery{
				table:      "cases",
				openedCol:  "created_at",
				openFilter: "status NOT IN ('closed', 'str_filed')",
			},
			insertSQL: `
				INSERT INTO cases (
					id, customer_id, status, priority, summary, created_at,
					assigned_to, assigned_team, due_at
				) VALUES ($1, $2, 'open', 'medium', 'dashboard boundary', $3, $4, $5, $6)`,
			newID: func() string { return "dashboard-case-" + newTestUUID() },
		},
	}
}

func TestPgDashboardWorkload_EmptyQueuesReturnZeroBuckets(t *testing.T) {
	now := time.Date(2026, time.August, 29, 3, 0, 0, 0, time.UTC)

	for _, test := range dashboardWorkloadTestQueues() {
		t.Run(test.name, func(t *testing.T) {
			tx := dashboardWorkloadTestTx(t)
			counts, err := pgDashboardWorkload(context.Background(), tx, test.query, "analyst", now, 24*time.Hour, true)
			if err != nil {
				t.Fatalf("DashboardWorkload: %v", err)
			}
			if counts.Open != 0 || counts.Mine != 0 || counts.Unassigned != 0 {
				t.Fatalf("empty ownership counts = %+v, want zero", counts)
			}
			if counts.OldestOpenAt != nil || counts.OldestAgeSeconds != nil {
				t.Fatalf("empty oldest values = %v/%v, want nil", counts.OldestOpenAt, counts.OldestAgeSeconds)
			}
			if len(counts.AgeBuckets) != 4 {
				t.Fatalf("age bucket count = %d, want 4", len(counts.AgeBuckets))
			}
			for _, bucket := range counts.AgeBuckets {
				if bucket.Count != 0 {
					t.Errorf("empty age bucket %q = %d, want 0", bucket.Label, bucket.Count)
				}
			}
			if counts.Overdue == nil || *counts.Overdue != 0 {
				t.Errorf("empty overdue = %v, want 0", counts.Overdue)
			}
			if counts.DueSoon == nil || *counts.DueSoon != 0 {
				t.Errorf("empty due soon = %v, want 0", counts.DueSoon)
			}
		})
	}
}

func TestPgDashboardWorkload_AlertAndCaseBoundariesMatch(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 29, 3, 0, 0, 0, time.UTC)

	type fixture struct {
		opened       time.Time
		assignedTo   string
		assignedTeam string
		due          time.Time
	}
	fixtures := []fixture{
		{opened: now.Add(-24*time.Hour + time.Microsecond), assignedTo: "analyst", due: now.Add(-time.Microsecond)},
		{opened: now.Add(-24 * time.Hour), due: now},
		{opened: now.Add(-72 * time.Hour), assignedTo: "ANALYST", due: now.Add(24 * time.Hour)},
		{opened: now.Add(-168 * time.Hour), assignedTeam: "investigations", due: now.Add(24*time.Hour + time.Microsecond)},
	}

	for _, test := range dashboardWorkloadTestQueues() {
		t.Run(test.name, func(t *testing.T) {
			tx := dashboardWorkloadTestTx(t)
			var customerID string
			if err := tx.QueryRow(ctx, `
				INSERT INTO customers (external_id, customer_type, attributes)
				VALUES ($1, 'individual', '{}')
				RETURNING id`, "dashboard-workload-"+newTestUUID()).Scan(&customerID); err != nil {
				t.Fatalf("insert dashboard customer: %v", err)
			}
			for i, fixture := range fixtures {
				if _, err := tx.Exec(ctx, test.insertSQL,
					test.newID(), customerID, fixture.opened, fixture.assignedTo, fixture.assignedTeam, fixture.due,
				); err != nil {
					t.Fatalf("insert %s fixture %d: %v", test.name, i, err)
				}
			}

			counts, err := pgDashboardWorkload(ctx, tx, test.query, "analyst", now, 24*time.Hour, true)
			if err != nil {
				t.Fatalf("DashboardWorkload: %v", err)
			}
			if counts.Open != 4 || counts.Mine != 2 || counts.Unassigned != 1 {
				t.Errorf("ownership counts = open:%d mine:%d unassigned:%d, want 4/2/1", counts.Open, counts.Mine, counts.Unassigned)
			}
			if counts.OldestOpenAt == nil || !counts.OldestOpenAt.Equal(now.Add(-168*time.Hour)) {
				t.Errorf("oldest open = %v, want %v", counts.OldestOpenAt, now.Add(-168*time.Hour))
			}
			if counts.OldestAgeSeconds == nil || *counts.OldestAgeSeconds != int64((168*time.Hour).Seconds()) {
				t.Errorf("oldest age = %v, want %d", counts.OldestAgeSeconds, int64((168 * time.Hour).Seconds()))
			}
			if len(counts.AgeBuckets) != 4 {
				t.Fatalf("age bucket count = %d, want 4", len(counts.AgeBuckets))
			}
			for i, bucket := range counts.AgeBuckets {
				if bucket.Count != 1 {
					t.Errorf("age bucket %d (%s) = %d, want 1", i, bucket.Label, bucket.Count)
				}
			}
			if counts.Overdue == nil || *counts.Overdue != 1 {
				t.Errorf("overdue = %v, want 1", counts.Overdue)
			}
			if counts.DueSoon == nil || *counts.DueSoon != 2 {
				t.Errorf("due soon = %v, want 2", counts.DueSoon)
			}
		})
	}
}
