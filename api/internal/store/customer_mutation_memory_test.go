package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestMemoryCustomerUpdateStatusAdvancesTimestampWhenClockDoesNotAdvance(t *testing.T) {
	repo := NewMemoryCustomerRepo()
	ctx := context.Background()
	frozen := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	customer := &domain.Customer{
		ID:           newTestUUID(),
		ExternalID:   "memory-customer-status-monotonic-" + newTestUUID(),
		CustomerType: domain.CustomerTypeIndividual,
		CountryCode:  "JP",
		Status:       domain.CustomerStatusActive,
		CreatedAt:    frozen,
		UpdatedAt:    frozen,
	}
	if err := repo.Create(ctx, customer); err != nil {
		t.Fatalf("create customer: %v", err)
	}

	updated, err := repo.UpdateStatus(ctx, customer.ID, domain.CustomerStatusFrozen, "test status update")
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if !updated.UpdatedAt.After(frozen) {
		t.Errorf("status updated_at = %s, want strictly after %s", updated.UpdatedAt, frozen)
	}

	stale := *updated
	stale.CountryCode = "US"
	err = repo.UpdateIfUnmodified(ctx, &stale, frozen)
	var conflict *domain.ErrConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("reuse pre-status token error = %v, want ErrConflict", err)
	}
}

func TestMemoryCustomerUpdateAdvancesBeyondCurrentStoredTimestamp(t *testing.T) {
	repo := NewMemoryCustomerRepo()
	ctx := context.Background()
	frozen := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	customer := &domain.Customer{
		ID:           newTestUUID(),
		ExternalID:   "memory-customer-current-token-" + newTestUUID(),
		CustomerType: domain.CustomerTypeIndividual,
		CountryCode:  "JP",
		Status:       domain.CustomerStatusActive,
		CreatedAt:    frozen,
		UpdatedAt:    frozen,
	}
	if err := repo.Create(ctx, customer); err != nil {
		t.Fatalf("create customer: %v", err)
	}

	stale, err := repo.Get(ctx, customer.ID)
	if err != nil {
		t.Fatalf("get stale customer: %v", err)
	}
	current, err := repo.UpdateStatus(ctx, customer.ID, domain.CustomerStatusFrozen, "intervening status update")
	if err != nil {
		t.Fatalf("intervening status update: %v", err)
	}

	stale.CountryCode = "US"
	if err := repo.Update(ctx, stale); err != nil {
		t.Fatalf("update stale object: %v", err)
	}
	if !stale.UpdatedAt.After(current.UpdatedAt) {
		t.Errorf("updated_at = %s, want strictly after current stored %s", stale.UpdatedAt, current.UpdatedAt)
	}
	persisted, err := repo.Get(ctx, customer.ID)
	if err != nil {
		t.Fatalf("get updated customer: %v", err)
	}
	if !persisted.UpdatedAt.Equal(stale.UpdatedAt) {
		t.Errorf("persisted updated_at = %s, returned %s", persisted.UpdatedAt, stale.UpdatedAt)
	}
}
