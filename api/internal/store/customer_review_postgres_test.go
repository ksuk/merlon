package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestPostgresCustomerReviewRepoLifecycleRiskTiers(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil {
			t.Errorf("rollback transaction: %v", err)
		}
	})

	var customerID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
		VALUES ($1, 'individual', 'JP', '{}', '{}') RETURNING id`,
		"customer-review-test-"+newTestUUID(),
	).Scan(&customerID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	repo := NewPgCustomerReviewRepo(tx)
	now := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	review := &domain.CustomerReview{
		ID:            newTestUUID(),
		CustomerID:    customerID,
		Cycle:         1,
		Status:        domain.CustomerReviewStatusScheduled,
		Tier:          domain.RiskTierHigh,
		Priority:      domain.CasePriorityHigh,
		DueAt:         now.Add(30 * 24 * time.Hour),
		GraceUntil:    now.Add(37 * 24 * time.Hour),
		PolicyVersion: "test-v1",
		PolicyDigest:  "test-digest",
		Scope:         map[string]any{},
		EvidenceRefs:  []string{},
		ScheduledAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := repo.Create(ctx, review); err != nil {
		t.Fatalf("Create scheduled review without optional tiers: %v", err)
	}

	created, err := repo.Get(ctx, review.ID)
	if err != nil {
		t.Fatalf("Get created review: %v", err)
	}
	if created.Tier != domain.RiskTierHigh || created.PreviousTier != "" || created.ResultingTier != "" {
		t.Fatalf("created tiers = (%q, %q, %q), want (high, empty, empty)", created.Tier, created.PreviousTier, created.ResultingTier)
	}
	nextReviewAt := now.Add(60 * 24 * time.Hour)
	if err := repo.UpdateReviewProjection(ctx, customerID, &nextReviewAt, nil, domain.RiskTierHigh, "test-v1", "test-digest"); err != nil {
		t.Fatalf("UpdateReviewProjection risk tier: %v", err)
	}
	customer, err := NewPgCustomerRepo(tx, nil).Get(ctx, customerID)
	if err != nil {
		t.Fatalf("Get projected customer: %v", err)
	}
	if customer.ReviewTier == nil || *customer.ReviewTier != domain.RiskTierHigh {
		t.Fatalf("projected review tier = %v, want high", customer.ReviewTier)
	}

	stale := *created
	updated := *created
	updated.Status = domain.CustomerReviewStatusInProgress
	updated.PreviousTier = domain.RiskTierHigh
	if err := repo.UpdateIfUnmodified(ctx, &updated, created.Version); err != nil {
		t.Fatalf("Update previous tier: %v", err)
	}

	stale.Status = domain.CustomerReviewStatusDue
	if err := repo.UpdateIfUnmodified(ctx, &stale, created.Version); err == nil {
		t.Fatal("stale update succeeded")
	} else {
		var conflict *domain.ErrConflict
		if !errors.As(err, &conflict) {
			t.Fatalf("stale update error = %v, want conflict", err)
		}
	}

	completedAt := now.Add(time.Hour)
	updated.Status = domain.CustomerReviewStatusCompleted
	updated.Outcome = domain.CustomerReviewOutcomeRatingChanged
	updated.ResultingTier = domain.RiskTierMedium
	updated.CompletedAt = &completedAt
	if err := repo.UpdateIfUnmodified(ctx, &updated, updated.Version); err != nil {
		t.Fatalf("Update resulting tier: %v", err)
	}

	completed, err := repo.Get(ctx, review.ID)
	if err != nil {
		t.Fatalf("Get completed review: %v", err)
	}
	if completed.PreviousTier != domain.RiskTierHigh || completed.ResultingTier != domain.RiskTierMedium {
		t.Fatalf("completed tiers = (%q, %q), want (high, medium)", completed.PreviousTier, completed.ResultingTier)
	}
	if completed.Version != created.Version+2 {
		t.Fatalf("completed version = %d, want %d", completed.Version, created.Version+2)
	}
}
