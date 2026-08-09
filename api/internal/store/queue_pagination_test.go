package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/domain"
)

func TestMemoryAlertQueueCursorTraverses10001TiesWithoutGaps(t *testing.T) {
	repo := NewMemoryAlertRepo()
	now := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10001; i++ {
		alert := &domain.Alert{ID: fmt.Sprintf("queue-alert-%05d", i), CustomerID: "queue-customer", ScenarioID: "queue-target", Severity: domain.AlertSeverityHigh, Status: domain.AlertStatusOpen, DetectedAt: now, CreatedAt: now, UpdatedAt: now}
		if err := repo.Create(context.Background(), alert); err != nil {
			t.Fatalf("create alert %d: %v", i, err)
		}
	}
	filter := domain.AlertQueueFilter{ScenarioID: "queue-target", AsOf: now}
	offsetRows, err := repo.ListQueue(context.Background(), filter, 10001, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(offsetRows) != 10001 {
		t.Fatalf("offset row count = %d, want 10001", len(offsetRows))
	}
	traversed := make([]domain.Alert, 0, len(offsetRows))
	var cursor *domain.Cursor
	for len(traversed) < len(offsetRows) {
		page, err := repo.ListQueueCursor(context.Background(), filter, 257, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			t.Fatal("cursor traversal ended before the final page")
		}
		traversed = append(traversed, page...)
		last := page[len(page)-1]
		cursor = &domain.Cursor{Sort: "risk", Rank: domain.AlertSeverityRank(last.Severity), CreatedAt: last.UpdatedAt, ID: last.ID}
		if len(page) < 257 {
			break
		}
	}
	assertUniqueAlertIDs(t, traversed)
	if len(traversed) != len(offsetRows) {
		t.Fatalf("cursor row count = %d, want %d", len(traversed), len(offsetRows))
	}

	// The contract is a stable-dataset keyset traversal. A row inserted ahead
	// of the already-issued boundary is not retroactively added to later pages.
	inserted := &domain.Alert{ID: "queue-alert-zzzzz", CustomerID: "queue-customer", ScenarioID: "queue-target", Severity: domain.AlertSeverityHigh, Status: domain.AlertStatusOpen, DetectedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(context.Background(), inserted); err != nil {
		t.Fatal(err)
	}
	rest, err := repo.ListQueueCursor(context.Background(), filter, 10001, cursor)
	if err != nil {
		t.Fatal(err)
	}
	for _, alert := range rest {
		if alert.ID == inserted.ID {
			t.Fatal("row inserted before the cursor appeared in the already-issued traversal")
		}
	}
}

func TestPostgresAlertQueueCursorTraverses10001TiesAndFilters(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	now := time.Date(2099, 8, 4, 0, 0, 0, 0, time.UTC)
	rows := make([][]any, 0, 10001)
	ids := make([]string, 0, 10001)
	for i := 0; i < 10001; i++ {
		id := newTestUUID()
		ids = append(ids, id)
		scenario := "queue-other"
		if i%3 != 0 {
			scenario = "queue-target"
		}
		rows = append(rows, []any{id, customerID, scenario, "high", "open", 1, "", []string{}, now, now, now})
	}
	if _, err := pool.CopyFrom(ctx, pgx.Identifier{"alerts"}, []string{"id", "customer_id", "scenario_id", "severity", "status", "score", "description", "transaction_ids", "detected_at", "created_at", "updated_at"}, pgx.CopyFromRows(rows)); err != nil {
		t.Fatalf("copy alerts: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM alerts WHERE id = ANY($1::uuid[])`, ids) })
	repo := NewPgAlertRepo(pool)
	filter := domain.AlertQueueFilter{ScenarioID: "queue-target", AsOf: now}
	offsetRows, err := repo.ListQueue(ctx, filter, 10001, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantCount := 10001 - 3334
	if len(offsetRows) != wantCount {
		t.Fatalf("filtered offset row count = %d, want %d", len(offsetRows), wantCount)
	}
	traversed := make([]domain.Alert, 0, wantCount)
	var cursor *domain.Cursor
	for {
		page, err := repo.ListQueueCursor(ctx, filter, 251, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		traversed = append(traversed, page...)
		last := page[len(page)-1]
		cursor = &domain.Cursor{Sort: "risk", Rank: domain.AlertSeverityRank(last.Severity), CreatedAt: last.UpdatedAt, ID: last.ID}
		if len(page) < 251 {
			break
		}
	}
	assertUniqueAlertIDs(t, traversed)
	if len(traversed) != len(offsetRows) {
		t.Fatalf("filtered cursor row count = %d, want %d", len(traversed), len(offsetRows))
	}
}

func assertUniqueAlertIDs(t *testing.T, alerts []domain.Alert) {
	t.Helper()
	seen := make(map[string]struct{}, len(alerts))
	for _, alert := range alerts {
		if _, exists := seen[alert.ID]; exists {
			t.Fatalf("cursor traversal duplicated alert %s", alert.ID)
		}
		seen[alert.ID] = struct{}{}
	}
}
