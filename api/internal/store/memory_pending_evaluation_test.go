package store

import (
	"context"
	"errors"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestMemoryPendingEvaluationRepo_CreateAndGet(t *testing.T) {
	repo := NewMemoryPendingEvaluationRepo()
	ctx := context.Background()

	pe := &domain.PendingEvaluation{
		ID:             "pe1",
		CustomerID:     "cust1",
		TransactionIDs: []string{"tx1", "tx2"},
		Status:         domain.PendingEvaluationStatusPendingReview,
		Reason:         "engine unavailable: deadline exceeded",
	}
	if err := repo.Create(ctx, pe); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, "pe1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CustomerID != "cust1" {
		t.Errorf("CustomerID = %s, want cust1", got.CustomerID)
	}
	if got.Status != domain.PendingEvaluationStatusPendingReview {
		t.Errorf("Status = %s, want PENDING_REVIEW", got.Status)
	}
	if len(got.TransactionIDs) != 2 {
		t.Errorf("TransactionIDs = %v, want 2 entries", got.TransactionIDs)
	}
}

func TestMemoryPendingEvaluationRepo_Get_NotFound(t *testing.T) {
	repo := NewMemoryPendingEvaluationRepo()
	ctx := context.Background()

	_, err := repo.Get(ctx, "missing")
	if err == nil {
		t.Fatal("expected error for missing record")
	}
	var notFound *domain.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("expected ErrNotFound, got %T: %v", err, err)
	}
}

func TestMemoryPendingEvaluationRepo_ListByStatus(t *testing.T) {
	repo := NewMemoryPendingEvaluationRepo()
	ctx := context.Background()

	must := func(pe *domain.PendingEvaluation) {
		t.Helper()
		if err := repo.Create(ctx, pe); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	must(&domain.PendingEvaluation{ID: "pe1", CustomerID: "c1", Status: domain.PendingEvaluationStatusPendingReview, Reason: "r1"})
	must(&domain.PendingEvaluation{ID: "pe2", CustomerID: "c2", Status: domain.PendingEvaluationStatusPendingReview, Reason: "r2"})
	must(&domain.PendingEvaluation{ID: "pe3", CustomerID: "c3", Status: domain.PendingEvaluationStatusResolved, Reason: "r3"})

	pending, err := repo.ListByStatus(ctx, domain.PendingEvaluationStatusPendingReview, 50, 0)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("ListByStatus(PENDING_REVIEW) = %d records, want 2", len(pending))
	}

	resolved, err := repo.ListByStatus(ctx, domain.PendingEvaluationStatusResolved, 50, 0)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("ListByStatus(RESOLVED) = %d records, want 1", len(resolved))
	}
}

func TestMemoryPendingEvaluationRepo_UpdateStatus(t *testing.T) {
	repo := NewMemoryPendingEvaluationRepo()
	ctx := context.Background()

	pe := &domain.PendingEvaluation{ID: "pe1", CustomerID: "c1", Status: domain.PendingEvaluationStatusPendingReview, Reason: "r1"}
	if err := repo.Create(ctx, pe); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.UpdateStatus(ctx, "pe1", domain.PendingEvaluationStatusResolved); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := repo.Get(ctx, "pe1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.PendingEvaluationStatusResolved {
		t.Errorf("Status = %s, want RESOLVED", got.Status)
	}
	if got.ResolvedAt == nil {
		t.Error("ResolvedAt should be set when status becomes RESOLVED")
	}
}

func TestMemoryPendingEvaluationRepo_IncrementRetry(t *testing.T) {
	repo := NewMemoryPendingEvaluationRepo()
	ctx := context.Background()

	pe := &domain.PendingEvaluation{ID: "pe1", CustomerID: "c1", Status: domain.PendingEvaluationStatusPendingReview, Reason: "r1"}
	if err := repo.Create(ctx, pe); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.IncrementRetry(ctx, "pe1"); err != nil {
		t.Fatalf("IncrementRetry: %v", err)
	}
	if err := repo.IncrementRetry(ctx, "pe1"); err != nil {
		t.Fatalf("IncrementRetry: %v", err)
	}

	got, err := repo.Get(ctx, "pe1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RetryCount != 2 {
		t.Errorf("RetryCount = %d, want 2", got.RetryCount)
	}
}
