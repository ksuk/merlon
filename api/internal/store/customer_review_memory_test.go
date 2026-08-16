package store

import (
	"context"
	"errors"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestMemoryCustomerReviewCASIncrementsAndRejectsStaleVersion(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryCustomerReviewRepo()
	review := &domain.CustomerReview{ID: "review-cas", CustomerID: "customer-cas", Cycle: 1, Status: domain.CustomerReviewStatusScheduled}
	if err := repo.Create(ctx, review); err != nil {
		t.Fatal(err)
	}
	firstVersion := review.Version
	review.Status = domain.CustomerReviewStatusDue
	if err := repo.UpdateIfUnmodified(ctx, review, firstVersion); err != nil {
		t.Fatal(err)
	}
	if review.Version != firstVersion+1 {
		t.Fatalf("version = %d, want %d", review.Version, firstVersion+1)
	}
	stale := *review
	stale.Status = domain.CustomerReviewStatusOverdue
	if err := repo.UpdateIfUnmodified(ctx, &stale, firstVersion); err == nil {
		t.Fatal("stale update succeeded")
	} else {
		var conflict *domain.ErrConflict
		if !errors.As(err, &conflict) {
			t.Fatalf("error = %v, want conflict", err)
		}
	}
}

func TestMemoryCompletedCustomerReviewRejectsSameStatusRewrite(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryCustomerReviewRepo()
	review := &domain.CustomerReview{ID: "review-completed", CustomerID: "customer-completed", Cycle: 1, Status: domain.CustomerReviewStatusCompleted, Rationale: "original"}
	if err := repo.Create(ctx, review); err != nil {
		t.Fatal(err)
	}
	review.Rationale = "rewritten"
	if err := repo.UpdateIfUnmodified(ctx, review, review.Version); err == nil {
		t.Fatal("completed review rewrite succeeded")
	}
}
