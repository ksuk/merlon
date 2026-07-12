package store

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func newTestAlert(id, customerID, scenarioID string, windowStart *time.Time, at time.Time) *domain.Alert {
	return &domain.Alert{
		ID:                     id,
		CustomerID:             customerID,
		ScenarioID:             scenarioID,
		Severity:               domain.AlertSeverityMedium,
		Status:                 domain.AlertStatusOpen,
		Score:                  1.5,
		Description:            "test alert",
		TransactionIDs:         []string{},
		DetectedAt:             at,
		AggregationWindowStart: windowStart,
		CreatedAt:              at,
		UpdatedAt:              at,
	}
}

// Postgres integration tests (require MERLON_DATABASE_URL; see newTestPgPool).

func TestPostgresAlertRepo_DuplicateWindowRejected(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgAlertRepo(pool)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	window := base.Truncate(24 * time.Hour)

	first := newTestAlert(newTestUUID(), customerID, "tm_structuring_v2", &window, base)
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM alerts WHERE id = $1`, first.ID) })
	created, existing, err := repo.CreateIfNotDuplicate(ctx, first)
	if err != nil {
		t.Fatalf("CreateIfNotDuplicate(first): %v", err)
	}
	if !created || existing != nil {
		t.Fatalf("first insert: created=%v existing=%v, want created=true existing=nil", created, existing)
	}

	second := newTestAlert(newTestUUID(), customerID, "tm_structuring_v2", &window, base.Add(time.Hour))
	created, existing, err = repo.CreateIfNotDuplicate(ctx, second)
	if err != nil {
		t.Fatalf("CreateIfNotDuplicate(second): %v", err)
	}
	if created {
		t.Fatal("second insert with identical (customer_id, scenario_id, aggregation_window_start) must not create")
	}
	if existing == nil || existing.ID != first.ID {
		t.Fatalf("existing = %v, want the first alert (%s)", existing, first.ID)
	}
}

func TestPostgresAlertRepo_DifferentWindowAllowed(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgAlertRepo(pool)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	window1 := base.Truncate(24 * time.Hour)
	window2 := window1.Add(24 * time.Hour)

	first := newTestAlert(newTestUUID(), customerID, "tm_structuring_v2", &window1, base)
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM alerts WHERE id = $1`, first.ID) })
	if created, _, err := repo.CreateIfNotDuplicate(ctx, first); err != nil || !created {
		t.Fatalf("CreateIfNotDuplicate(first): created=%v err=%v", created, err)
	}

	second := newTestAlert(newTestUUID(), customerID, "tm_structuring_v2", &window2, base.Add(24*time.Hour))
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM alerts WHERE id = $1`, second.ID) })
	created, existing, err := repo.CreateIfNotDuplicate(ctx, second)
	if err != nil {
		t.Fatalf("CreateIfNotDuplicate(second): %v", err)
	}
	if !created || existing != nil {
		t.Fatalf("different window: created=%v existing=%v, want created=true existing=nil", created, existing)
	}
}

func TestPostgresAlertRepo_NullWindowAllowsMultiple(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgAlertRepo(pool)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	first := newTestAlert(newTestUUID(), customerID, "tm_rapid_movement_v2", nil, base)
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM alerts WHERE id = $1`, first.ID) })
	if created, _, err := repo.CreateIfNotDuplicate(ctx, first); err != nil || !created {
		t.Fatalf("CreateIfNotDuplicate(first): created=%v err=%v", created, err)
	}

	// Same customer_id/scenario_id, both with a NULL aggregation_window_start
	// (e.g. a non-aggregation realtime scenario): the partial index excludes
	// NULLs, so both must succeed.
	second := newTestAlert(newTestUUID(), customerID, "tm_rapid_movement_v2", nil, base.Add(time.Minute))
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM alerts WHERE id = $1`, second.ID) })
	created, existing, err := repo.CreateIfNotDuplicate(ctx, second)
	if err != nil {
		t.Fatalf("CreateIfNotDuplicate(second): %v", err)
	}
	if !created || existing != nil {
		t.Fatalf("null window: created=%v existing=%v, want created=true existing=nil", created, existing)
	}
}

// Memory-side equivalents (WS-6 established this pattern for whitelist_test.go).

func TestMemoryAlertRepo_DuplicateWindowRejected(t *testing.T) {
	repo := NewMemoryAlertRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	window := base.Truncate(24 * time.Hour)

	first := newTestAlert("alert-1", "cust-1", "tm_structuring_v2", &window, base)
	created, existing, err := repo.CreateIfNotDuplicate(ctx, first)
	if err != nil || !created || existing != nil {
		t.Fatalf("first insert: created=%v existing=%v err=%v", created, existing, err)
	}

	second := newTestAlert("alert-2", "cust-1", "tm_structuring_v2", &window, base.Add(time.Hour))
	created, existing, err = repo.CreateIfNotDuplicate(ctx, second)
	if err != nil {
		t.Fatalf("CreateIfNotDuplicate(second): %v", err)
	}
	if created {
		t.Fatal("second insert with identical dedup key must not create")
	}
	if existing == nil || existing.ID != "alert-1" {
		t.Fatalf("existing = %v, want alert-1", existing)
	}
}

func TestMemoryAlertRepo_DifferentWindowAllowed(t *testing.T) {
	repo := NewMemoryAlertRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	window1 := base.Truncate(24 * time.Hour)
	window2 := window1.Add(24 * time.Hour)

	first := newTestAlert("alert-1", "cust-1", "tm_structuring_v2", &window1, base)
	if created, _, err := repo.CreateIfNotDuplicate(ctx, first); err != nil || !created {
		t.Fatalf("CreateIfNotDuplicate(first): created=%v err=%v", created, err)
	}

	second := newTestAlert("alert-2", "cust-1", "tm_structuring_v2", &window2, base.Add(24*time.Hour))
	created, existing, err := repo.CreateIfNotDuplicate(ctx, second)
	if err != nil || !created || existing != nil {
		t.Fatalf("different window: created=%v existing=%v err=%v", created, existing, err)
	}
}

func TestMemoryAlertRepo_NullWindowAllowsMultiple(t *testing.T) {
	repo := NewMemoryAlertRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	first := newTestAlert("alert-1", "cust-1", "tm_rapid_movement_v2", nil, base)
	if created, _, err := repo.CreateIfNotDuplicate(ctx, first); err != nil || !created {
		t.Fatalf("CreateIfNotDuplicate(first): created=%v err=%v", created, err)
	}

	second := newTestAlert("alert-2", "cust-1", "tm_rapid_movement_v2", nil, base.Add(time.Minute))
	created, existing, err := repo.CreateIfNotDuplicate(ctx, second)
	if err != nil || !created || existing != nil {
		t.Fatalf("null window: created=%v existing=%v err=%v", created, existing, err)
	}
}

func TestMemoryAlertRepo_AnnotateBatchReviewed(t *testing.T) {
	repo := NewMemoryAlertRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	window := base.Truncate(24 * time.Hour)

	a := newTestAlert("alert-1", "cust-1", "tm_structuring_v2", &window, base)
	if _, _, err := repo.CreateIfNotDuplicate(ctx, a); err != nil {
		t.Fatalf("CreateIfNotDuplicate: %v", err)
	}

	if err := repo.AnnotateBatchReviewed(ctx, "alert-1", "run-1"); err != nil {
		t.Fatalf("AnnotateBatchReviewed: %v", err)
	}

	got, err := repo.Get(ctx, "alert-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.BatchReviewedAt == nil {
		t.Error("BatchReviewedAt should be set")
	}
	if got.BatchRunID != "run-1" {
		t.Errorf("BatchRunID = %q, want run-1", got.BatchRunID)
	}
	if got.Status != domain.AlertStatusOpen {
		t.Errorf("Status changed to %s, want unchanged (open)", got.Status)
	}
}
