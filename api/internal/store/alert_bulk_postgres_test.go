package store

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestPostgresAlertRepoBulkCloseAcceptsOpenAlert(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	now := time.Now().UTC()
	alert := &domain.Alert{
		ID: newTestUUID(), CustomerID: customerID, ScenarioID: "bulk_close_test",
		Severity: domain.AlertSeverityHigh, Status: domain.AlertStatusOpen,
		Score: 1, TransactionIDs: []string{}, DetectedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	repo := NewPgAlertRepo(pool)
	if err := repo.Create(ctx, alert); err != nil {
		t.Fatalf("create alert: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM alerts WHERE id = $1`, alert.ID)
	})

	if err := repo.CloseFalsePositive(ctx, alert.ID, "bulk-reviewer"); err != nil {
		t.Fatalf("bulk close open alert: %v", err)
	}
	got, err := repo.Get(ctx, alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.AlertStatusClosedFalsePositive || got.ResolvedAt == nil || got.ResolvedBy != "bulk-reviewer" {
		t.Fatalf("bulk-closed alert = %+v", got)
	}
}
