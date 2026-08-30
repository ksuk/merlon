package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestPgCustomerUpdateIfUnmodifiedAdvancesTimestampWhenClockDoesNotAdvance(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	repo := NewPgCustomerRepo(pool, nil)

	// A stored value ahead of the process clock deterministically models a
	// coarse or frozen clock that has not advanced since the prior mutation.
	frozen := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	customer := &domain.Customer{
		ID:           newTestUUID(),
		ExternalID:   "customer-monotonic-" + newTestUUID(),
		CustomerType: domain.CustomerTypeIndividual,
		CountryCode:  "JP",
		Status:       domain.CustomerStatusActive,
		CreatedAt:    frozen,
		UpdatedAt:    frozen,
	}
	if err := repo.Create(ctx, customer); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, customer.ID)
	})

	stored, err := repo.Get(ctx, customer.ID)
	if err != nil {
		t.Fatalf("get created customer: %v", err)
	}
	originalToken := stored.UpdatedAt

	first := *stored
	first.CountryCode = "US"
	if err := repo.UpdateIfUnmodified(ctx, &first, originalToken); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if !first.UpdatedAt.After(originalToken) {
		t.Errorf("first updated_at = %s, want strictly after %s", first.UpdatedAt, originalToken)
	}

	persisted, err := repo.Get(ctx, customer.ID)
	if err != nil {
		t.Fatalf("get updated customer: %v", err)
	}
	if !persisted.UpdatedAt.Equal(first.UpdatedAt) {
		t.Errorf("persisted updated_at = %s, returned %s", persisted.UpdatedAt, first.UpdatedAt)
	}

	stale := *persisted
	stale.CountryCode = "GB"
	err = repo.UpdateIfUnmodified(ctx, &stale, originalToken)
	var conflict *domain.ErrConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("reuse original token error = %v, want ErrConflict", err)
	}

	afterConflict, err := repo.Get(ctx, customer.ID)
	if err != nil {
		t.Fatalf("get after conflict: %v", err)
	}
	if afterConflict.CountryCode != "US" {
		t.Fatalf("stale update changed country_code to %q", afterConflict.CountryCode)
	}
}

func TestPgCustomerUpdateStatusAdvancesTimestampWhenClockDoesNotAdvance(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	repo := NewPgCustomerRepo(pool, nil)

	frozen := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	customer := &domain.Customer{
		ID:           newTestUUID(),
		ExternalID:   "customer-status-monotonic-" + newTestUUID(),
		CustomerType: domain.CustomerTypeIndividual,
		CountryCode:  "JP",
		Status:       domain.CustomerStatusActive,
		CreatedAt:    frozen,
		UpdatedAt:    frozen,
	}
	if err := repo.Create(ctx, customer); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, customer.ID)
	})

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

func TestPgCustomerUpdateAdvancesBeyondCurrentStoredTimestamp(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	repo := NewPgCustomerRepo(pool, nil)

	frozen := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	customer := &domain.Customer{
		ID:           newTestUUID(),
		ExternalID:   "customer-current-token-" + newTestUUID(),
		CustomerType: domain.CustomerTypeIndividual,
		CountryCode:  "JP",
		Status:       domain.CustomerStatusActive,
		CreatedAt:    frozen,
		UpdatedAt:    frozen,
	}
	if err := repo.Create(ctx, customer); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, customer.ID)
	})

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
