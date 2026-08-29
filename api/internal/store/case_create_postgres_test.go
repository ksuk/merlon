package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestPostgresCaseRepoCreateClosedAt(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgCaseRepo(pool)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	createdAt := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	closedAt := createdAt.Add(30 * time.Minute)

	newCase := func(status domain.CaseStatus) *domain.Case {
		return &domain.Case{
			ID:         newTestUUID(),
			CustomerID: customerID,
			AlertIDs:   []string{},
			Status:     status,
			Priority:   domain.CasePriorityMedium,
			Summary:    "synthetic terminal case",
			CreatedAt:  createdAt,
			UpdatedAt:  closedAt,
		}
	}

	for _, status := range []domain.CaseStatus{
		domain.CaseStatusClosed,
		domain.CaseStatusStrFiled,
	} {
		t.Run("persists "+string(status), func(t *testing.T) {
			kase := newCase(status)
			kase.ClosedAt = &closedAt
			t.Cleanup(func() {
				pool.Exec(context.Background(), `DELETE FROM cases WHERE id = $1`, kase.ID)
			})

			if err := repo.Create(ctx, kase); err != nil {
				t.Fatalf("Create(%s): %v", status, err)
			}

			got, err := repo.Get(ctx, kase.ID)
			if err != nil {
				t.Fatalf("Get(%s): %v", status, err)
			}
			if got.ClosedAt == nil || !got.ClosedAt.Equal(closedAt) {
				t.Fatalf("ClosedAt = %v, want %v", got.ClosedAt, closedAt)
			}
		})
	}

	t.Run("rejects terminal case without close time before persistence", func(t *testing.T) {
		kase := newCase(domain.CaseStatusClosed)

		err := repo.Create(ctx, kase)
		if err == nil || !strings.Contains(err.Error(), "closed_at is required for terminal case status") {
			t.Fatalf("Create invalid terminal error = %v, want pre-persistence closed_at validation", err)
		}

		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM cases WHERE id = $1`, kase.ID).Scan(&count); err != nil {
			t.Fatalf("count invalid terminal row: %v", err)
		}
		if count != 0 {
			t.Fatalf("invalid terminal row count = %d, want 0", count)
		}
	})

	t.Run("clears close time from active case", func(t *testing.T) {
		kase := newCase(domain.CaseStatusInvestigating)
		kase.ClosedAt = &closedAt
		t.Cleanup(func() {
			pool.Exec(context.Background(), `DELETE FROM cases WHERE id = $1`, kase.ID)
		})

		if err := repo.Create(ctx, kase); err != nil {
			t.Fatalf("Create(active): %v", err)
		}
		if kase.ClosedAt != nil {
			t.Fatalf("normalized active input retained closed_at=%v", kase.ClosedAt)
		}
		got, err := repo.Get(ctx, kase.ID)
		if err != nil {
			t.Fatalf("Get(active): %v", err)
		}
		if got.ClosedAt != nil {
			t.Fatalf("active row retained closed_at=%v", got.ClosedAt)
		}
	})
}
