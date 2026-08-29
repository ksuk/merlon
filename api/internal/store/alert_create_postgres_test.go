package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestPostgresAlertRepoCreateResolutionMetadata(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgAlertRepo(pool)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	createdAt := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	resolvedAt := createdAt.Add(30 * time.Minute)

	for _, status := range []domain.AlertStatus{
		domain.AlertStatusClosedTruePositive,
		domain.AlertStatusClosedFalsePositive,
	} {
		t.Run("persists "+string(status), func(t *testing.T) {
			alert := newTestAlert(newTestUUID(), customerID, "demo_terminal", nil, createdAt)
			alert.Status = status
			alert.ResolvedAt = &resolvedAt
			alert.ResolvedBy = "demo-seed"
			t.Cleanup(func() {
				pool.Exec(context.Background(), `DELETE FROM alerts WHERE id = $1`, alert.ID)
			})

			if err := repo.Create(ctx, alert); err != nil {
				t.Fatalf("Create(%s): %v", status, err)
			}

			got, err := repo.Get(ctx, alert.ID)
			if err != nil {
				t.Fatalf("Get(%s): %v", status, err)
			}
			if got.ResolvedAt == nil || !got.ResolvedAt.Equal(resolvedAt) {
				t.Fatalf("ResolvedAt = %v, want %v", got.ResolvedAt, resolvedAt)
			}
			if got.ResolvedBy != "demo-seed" {
				t.Fatalf("ResolvedBy = %q, want demo-seed", got.ResolvedBy)
			}
		})
	}

	for _, tc := range []struct {
		name       string
		resolvedAt *time.Time
		resolvedBy string
	}{
		{name: "missing resolution time", resolvedBy: "demo-seed"},
		{name: "missing resolution actor", resolvedAt: &resolvedAt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			alert := newTestAlert(newTestUUID(), customerID, "demo_invalid_terminal", nil, createdAt)
			alert.Status = domain.AlertStatusClosedFalsePositive
			alert.ResolvedAt = tc.resolvedAt
			alert.ResolvedBy = tc.resolvedBy

			err := repo.Create(ctx, alert)
			if err == nil || !strings.Contains(err.Error(), "resolved_at and resolved_by are required for terminal alert status") {
				t.Fatalf("Create invalid terminal error = %v, want pre-persistence resolution validation", err)
			}

			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM alerts WHERE id = $1`, alert.ID).Scan(&count); err != nil {
				t.Fatalf("count invalid terminal row: %v", err)
			}
			if count != 0 {
				t.Fatalf("invalid terminal row count = %d, want 0", count)
			}
		})
	}

	t.Run("clears resolution metadata from active alert", func(t *testing.T) {
		alert := newTestAlert(newTestUUID(), customerID, "demo_active", nil, createdAt)
		alert.Status = domain.AlertStatusInvestigating
		alert.ResolvedAt = &resolvedAt
		alert.ResolvedBy = "stale-demo-actor"
		t.Cleanup(func() {
			pool.Exec(context.Background(), `DELETE FROM alerts WHERE id = $1`, alert.ID)
		})

		if err := repo.Create(ctx, alert); err != nil {
			t.Fatalf("Create(active): %v", err)
		}
		if alert.ResolvedAt != nil || alert.ResolvedBy != "" {
			t.Fatalf("normalized active input retained resolution metadata: resolved_at=%v resolved_by=%q", alert.ResolvedAt, alert.ResolvedBy)
		}
		got, err := repo.Get(ctx, alert.ID)
		if err != nil {
			t.Fatalf("Get(active): %v", err)
		}
		if got.ResolvedAt != nil || got.ResolvedBy != "" {
			t.Fatalf("active row retained resolution metadata: resolved_at=%v resolved_by=%q", got.ResolvedAt, got.ResolvedBy)
		}
	})
}
