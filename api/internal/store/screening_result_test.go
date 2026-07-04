package store

import (
	"context"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

func newTestScreeningResult(id, customerID, entryID string, status domain.ScreeningResultStatus, at time.Time) *domain.ScreeningResultRecord {
	return &domain.ScreeningResultRecord{
		ID:          id,
		CustomerID:  customerID,
		ListID:      "mof_japan_sample",
		ListType:    "sanctions",
		EntryID:     entryID,
		MatchedName: "Kim Jong Un",
		Similarity:  0.97,
		Status:      status,
		ScreenedAt:  at,
		CreatedAt:   at,
	}
}

func TestMemoryScreeningResultRepo_CreateAndGet(t *testing.T) {
	repo := NewMemoryScreeningResultRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	r := newTestScreeningResult("sr-1", "cust-1", "MOF-001", domain.ScreeningResultStatusNew, base)
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, "sr-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CustomerID != "cust-1" || got.Status != domain.ScreeningResultStatusNew {
		t.Errorf("got %+v", got)
	}
}

func TestMemoryScreeningResultRepo_Update(t *testing.T) {
	repo := NewMemoryScreeningResultRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	r := newTestScreeningResult("sr-2", "cust-1", "MOF-001", domain.ScreeningResultStatusNew, base)
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}

	r.Status = domain.ScreeningResultStatusReviewing
	if err := repo.Update(ctx, r); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.Get(ctx, "sr-2")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.ScreeningResultStatusReviewing {
		t.Errorf("status = %q, want REVIEWING", got.Status)
	}
}

func TestMemoryScreeningResultRepo_ListByCustomer(t *testing.T) {
	repo := NewMemoryScreeningResultRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	if err := repo.Create(ctx, newTestScreeningResult("sr-3", "cust-1", "MOF-001", domain.ScreeningResultStatusNew, base)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, newTestScreeningResult("sr-4", "cust-2", "MOF-002", domain.ScreeningResultStatusNew, base.Add(time.Minute))); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.ListByCustomer(ctx, "cust-1", 10, 0)
	if err != nil {
		t.Fatalf("ListByCustomer: %v", err)
	}
	if len(got) != 1 || got[0].ID != "sr-3" {
		t.Errorf("got %+v, want single sr-3 result", got)
	}
}

func TestMemoryScreeningResultRepo_ListByStatus(t *testing.T) {
	repo := NewMemoryScreeningResultRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	if err := repo.Create(ctx, newTestScreeningResult("sr-5", "cust-1", "MOF-001", domain.ScreeningResultStatusNew, base)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, newTestScreeningResult("sr-6", "cust-2", "MOF-002", domain.ScreeningResultStatusReviewing, base.Add(time.Minute))); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.ListByStatus(ctx, domain.ScreeningResultStatusNew, 10, 0)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(got) != 1 || got[0].ID != "sr-5" {
		t.Errorf("got %+v, want single sr-5 result", got)
	}
}

func TestMemoryScreeningResultRepo_ListPastFalsePositives(t *testing.T) {
	repo := NewMemoryScreeningResultRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	fp := newTestScreeningResult("sr-7", "cust-1", "MOF-001", domain.ScreeningResultStatusReviewing, base)
	if err := repo.Create(ctx, fp); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := fp.ApplyStatusTransition(domain.ScreeningResultStatusFalsePositive, "different date of birth"); err != nil {
		t.Fatalf("ApplyStatusTransition: %v", err)
	}
	if err := repo.Update(ctx, fp); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// A new hit against the same list entry for a different customer.
	other := newTestScreeningResult("sr-8", "cust-2", "MOF-001", domain.ScreeningResultStatusNew, base.Add(time.Hour))
	if err := repo.Create(ctx, other); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.ListPastFalsePositives(ctx, "MOF-001")
	if err != nil {
		t.Fatalf("ListPastFalsePositives: %v", err)
	}
	if len(got) != 1 || got[0].ID != "sr-7" {
		t.Errorf("got %+v, want single sr-7 false positive", got)
	}
}

func TestMemoryScreeningResultRepo_GetNotFound(t *testing.T) {
	repo := NewMemoryScreeningResultRepo()
	if _, err := repo.Get(context.Background(), "missing"); err == nil {
		t.Error("expected error for missing screening result")
	}
}
