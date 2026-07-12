package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/domain"
)

func newTestWhitelistEntry(id, customerID string, status domain.WhitelistEntryStatus, at time.Time) *domain.WhitelistEntry {
	return &domain.WhitelistEntry{
		ID:          id,
		CustomerID:  customerID,
		Status:      status,
		Reason:      "long-standing trusted customer",
		ValidFrom:   at,
		ValidUntil:  at.Add(180 * 24 * time.Hour),
		RequestedBy: "analyst01",
		Version:     1,
		CreatedAt:   at,
		UpdatedAt:   at,
	}
}

func TestMemoryWhitelistRepo_CreateAndGet(t *testing.T) {
	repo := NewMemoryWhitelistRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	e := newTestWhitelistEntry("wl-1", "cust-1", domain.WhitelistEntryStatusPendingApproval, base)
	if err := repo.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, "wl-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CustomerID != "cust-1" || got.Status != domain.WhitelistEntryStatusPendingApproval {
		t.Errorf("got %+v", got)
	}
}

func TestMemoryWhitelistRepo_GetActiveByCustomer_NoneFound(t *testing.T) {
	repo := NewMemoryWhitelistRepo()
	if _, err := repo.GetActiveByCustomer(context.Background(), "nonexistent"); err == nil {
		t.Fatal("expected ErrNotFound")
	}
}

// TestMemoryWhitelistRepo_ActiveUniqueConstraint mirrors the Postgres
// partial unique index UNIQUE(customer_id) WHERE status = 'active'
// (whitelist.md §3.1). It is the memory-side equivalent the WS-6 task
// document requires when a real Postgres integration test cannot run in CI.
func TestMemoryWhitelistRepo_ActiveUniqueConstraint(t *testing.T) {
	repo := NewMemoryWhitelistRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	first := newTestWhitelistEntry("wl-1", "cust-1", domain.WhitelistEntryStatusPendingApproval, base)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create first: %v", err)
	}
	// Approve first: pending_approval -> active.
	first.Status = domain.WhitelistEntryStatusActive
	if err := repo.UpdateWithVersion(ctx, first, 1); err != nil {
		t.Fatalf("approve first: %v", err)
	}

	second := newTestWhitelistEntry("wl-2", "cust-1", domain.WhitelistEntryStatusPendingApproval, base.Add(time.Minute))
	if err := repo.Create(ctx, second); err != nil {
		t.Fatalf("Create second: %v", err)
	}

	// Approving the second entry while the first is still active must
	// conflict (WL-003/§3.1 concurrency control).
	second.Status = domain.WhitelistEntryStatusActive
	err := repo.UpdateWithVersion(ctx, second, 1)
	var conflict *domain.ErrConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("approve second: err = %v, want *domain.ErrConflict", err)
	}
}

func TestMemoryWhitelistRepo_UpdateWithVersion_ConflictOnStaleVersion(t *testing.T) {
	repo := NewMemoryWhitelistRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	e := newTestWhitelistEntry("wl-1", "cust-1", domain.WhitelistEntryStatusPendingApproval, base)
	if err := repo.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.Status = domain.WhitelistEntryStatusActive
	err := repo.UpdateWithVersion(ctx, e, 99) // stale/expected version wrong
	var conflict *domain.ErrConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want *domain.ErrConflict", err)
	}
}

func TestMemoryWhitelistRepo_ListExpiringSoon(t *testing.T) {
	repo := NewMemoryWhitelistRepo()
	ctx := context.Background()
	now := time.Now()

	soon := newTestWhitelistEntry("wl-soon", "cust-1", domain.WhitelistEntryStatusActive, now.Add(-170*24*time.Hour))
	soon.ValidUntil = now.Add(10 * 24 * time.Hour)
	if err := repo.Create(ctx, soon); err != nil {
		t.Fatalf("Create soon: %v", err)
	}

	far := newTestWhitelistEntry("wl-far", "cust-2", domain.WhitelistEntryStatusActive, now)
	far.ValidUntil = now.Add(300 * 24 * time.Hour)
	if err := repo.Create(ctx, far); err != nil {
		t.Fatalf("Create far: %v", err)
	}

	out, err := repo.ListExpiringSoon(ctx, 30)
	if err != nil {
		t.Fatalf("ListExpiringSoon: %v", err)
	}
	if len(out) != 1 || out[0].ID != "wl-soon" {
		t.Errorf("ListExpiringSoon = %+v, want exactly [wl-soon]", out)
	}
}

func TestMemoryWhitelistRepo_ReviewsRoundTrip(t *testing.T) {
	repo := NewMemoryWhitelistRepo()
	ctx := context.Background()

	rev := &domain.WhitelistReview{
		ID:               "rev-1",
		WhitelistEntryID: "wl-1",
		ReviewedBy:       "admin01",
		Decision:         domain.WhitelistReviewDecisionRenewed,
		CreatedAt:        time.Now(),
	}
	if err := repo.CreateReview(ctx, rev); err != nil {
		t.Fatalf("CreateReview: %v", err)
	}

	reviews, err := repo.ListReviews(ctx, "wl-1")
	if err != nil {
		t.Fatalf("ListReviews: %v", err)
	}
	if len(reviews) != 1 || reviews[0].ID != "rev-1" {
		t.Errorf("reviews = %+v", reviews)
	}
}

// TestPostgres_WhitelistActiveUniqueConstraint is a real Postgres
// integration test for the partial unique index (whitelist.md §3.1). It
// requires MERLON_DATABASE_URL to point at a live database with migrations
// applied, and is skipped otherwise (Docker-based verification is not
// available in this environment, per WS-6 task instructions).
func TestPostgres_WhitelistActiveUniqueConstraint(t *testing.T) {
	dsn := os.Getenv("MERLON_DATABASE_URL")
	if dsn == "" {
		t.Skip("MERLON_DATABASE_URL not set; skipping Postgres integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	repo := NewPostgresWhitelistRepo(pool)
	base := time.Now()
	customerID := "pg-integration-cust-" + base.Format("20060102150405")

	first := newTestWhitelistEntry("pg-wl-1-"+base.Format("20060102150405"), customerID, domain.WhitelistEntryStatusActive, base)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create first (active): %v", err)
	}

	second := newTestWhitelistEntry("pg-wl-2-"+base.Format("20060102150405"), customerID, domain.WhitelistEntryStatusActive, base.Add(time.Minute))
	err = repo.Create(ctx, second)
	var conflict *domain.ErrConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("Create second (active): err = %v, want *domain.ErrConflict", err)
	}
}
