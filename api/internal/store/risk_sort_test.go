package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestMemoryCaseRepo_RiskSortUsesAllPriorities(t *testing.T) {
	repo := NewMemoryCaseRepo()
	ctx := context.Background()
	created := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	priorities := []domain.CasePriority{
		domain.CasePriorityLow,
		domain.CasePriorityMedium,
		domain.CasePriorityHigh,
		domain.CasePriorityCritical,
	}
	for i, priority := range priorities {
		if err := repo.Create(ctx, &domain.Case{
			ID: fmt.Sprintf("risk-case-%d", i), Status: domain.CaseStatusInvestigating,
			Priority: priority, CreatedAt: created, UpdatedAt: created,
		}); err != nil {
			t.Fatalf("create %s: %v", priority, err)
		}
	}
	page, err := repo.ListOpenByRisk(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListOpenByRisk: %v", err)
	}
	want := []string{"risk-case-3", "risk-case-2", "risk-case-1", "risk-case-0"}
	for i, id := range want {
		if page[i].ID != id {
			t.Errorf("page[%d].ID = %q, want %q", i, page[i].ID, id)
		}
	}
}
