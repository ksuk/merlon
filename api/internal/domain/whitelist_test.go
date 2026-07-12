package domain

import (
	"context"
	"testing"
)

func TestWhitelistEntryStatus_Valid(t *testing.T) {
	cases := map[WhitelistEntryStatus]string{
		WhitelistEntryStatusPendingApproval: "pending_approval",
		WhitelistEntryStatusActive:          "active",
		WhitelistEntryStatusExpired:         "expired",
		WhitelistEntryStatusRevoked:         "revoked",
	}
	for status, want := range cases {
		if string(status) != want {
			t.Errorf("WhitelistEntryStatus %v = %q, want %q", status, string(status), want)
		}
	}
}

func TestWhitelistReviewDecision_Valid(t *testing.T) {
	cases := map[WhitelistReviewDecision]string{
		WhitelistReviewDecisionRenewed: "renewed",
		WhitelistReviewDecisionRevoked: "revoked",
	}
	for decision, want := range cases {
		if string(decision) != want {
			t.Errorf("WhitelistReviewDecision %v = %q, want %q", decision, string(decision), want)
		}
	}
}

// TestWhitelistRepository_interface is a compile-time check, via a minimal
// fake implementation, that the WhitelistRepository method set is
// implementable as intended (the real check is store.MemoryWhitelistRepo /
// store.PgWhitelistRepo satisfying this interface, exercised in store tests).
func TestWhitelistRepository_interface(t *testing.T) {
	var _ WhitelistRepository = (*fakeWhitelistRepo)(nil)
}

type fakeWhitelistRepo struct{}

func (f *fakeWhitelistRepo) Get(ctx context.Context, id string) (*WhitelistEntry, error) {
	return nil, nil
}
func (f *fakeWhitelistRepo) GetActiveByCustomer(ctx context.Context, customerID string) (*WhitelistEntry, error) {
	return nil, nil
}
func (f *fakeWhitelistRepo) List(ctx context.Context, status WhitelistEntryStatus, limit, offset int) ([]WhitelistEntry, error) {
	return nil, nil
}
func (f *fakeWhitelistRepo) ListExpiringSoon(ctx context.Context, withinDays int) ([]WhitelistEntry, error) {
	return nil, nil
}
func (f *fakeWhitelistRepo) Create(ctx context.Context, e *WhitelistEntry) error { return nil }
func (f *fakeWhitelistRepo) UpdateWithVersion(ctx context.Context, e *WhitelistEntry, expectedVersion int) error {
	return nil
}
func (f *fakeWhitelistRepo) CreateReview(ctx context.Context, r *WhitelistReview) error { return nil }
func (f *fakeWhitelistRepo) ListReviews(ctx context.Context, entryID string) ([]WhitelistReview, error) {
	return nil, nil
}
func (f *fakeWhitelistRepo) CreateReviewAndApply(ctx context.Context, review *WhitelistReview, entry *WhitelistEntry, expectedVersion int) error {
	return nil
}
