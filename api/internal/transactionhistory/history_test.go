package transactionhistory

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

type legacyTransactionRepo struct {
	inner *store.MemoryTransactionRepo
}

type looseCutoffTransactionRepo struct {
	*store.MemoryTransactionRepo
}

func (r *looseCutoffTransactionRepo) ListByCustomerEventRange(ctx context.Context, customerID string, from, to, createdBefore time.Time, limit int, after *domain.TransactionEventCursor) ([]domain.Transaction, error) {
	// Simulate an adapter with coarser cutoff precision that may return a row
	// at the exclusive boundary. The shared helper must enforce exact parity.
	return r.MemoryTransactionRepo.ListByCustomerEventRange(ctx, customerID, from, to, createdBefore.Add(time.Nanosecond), limit, after)
}

func (r *legacyTransactionRepo) Get(ctx context.Context, id string) (*domain.Transaction, error) {
	return r.inner.Get(ctx, id)
}
func (r *legacyTransactionRepo) ListByCustomer(ctx context.Context, customerID string, limit, offset int) ([]domain.Transaction, error) {
	return r.inner.ListByCustomer(ctx, customerID, limit, offset)
}
func (r *legacyTransactionRepo) ListByCustomerCursor(ctx context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.Transaction, error) {
	return r.inner.ListByCustomerCursor(ctx, customerID, limit, after)
}
func (r *legacyTransactionRepo) Create(ctx context.Context, txn *domain.Transaction) error {
	return r.inner.Create(ctx, txn)
}

func TestListCustomerTransactionsAsOf_KeysetAndFallbackParity(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	inner := store.NewMemoryTransactionRepo()
	for _, txn := range []domain.Transaction{
		{ID: "before", CustomerID: "c1", ExecutedAt: base.Add(-time.Nanosecond), CreatedAt: base.Add(-time.Hour)},
		{ID: "a", CustomerID: "c1", ExecutedAt: base, CreatedAt: base.Add(-time.Hour)},
		{ID: "b", CustomerID: "c1", ExecutedAt: base, CreatedAt: base},
		{ID: "late-ingest", CustomerID: "c1", ExecutedAt: base.Add(time.Minute), CreatedAt: base.Add(time.Nanosecond)},
		{ID: "at-end", CustomerID: "c1", ExecutedAt: base.Add(2 * time.Minute), CreatedAt: base.Add(-time.Hour)},
	} {
		txn := txn
		if err := inner.Create(ctx, &txn); err != nil {
			t.Fatal(err)
		}
	}
	query := Query{From: base, To: base.Add(2 * time.Minute), CreatedThrough: base, PageSize: 1}
	keyset, err := ListCustomerTransactionsAsOf(ctx, &looseCutoffTransactionRepo{MemoryTransactionRepo: inner}, "c1", query)
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := ListCustomerTransactionsAsOf(ctx, &legacyTransactionRepo{inner: inner}, "c1", query)
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]domain.Transaction{"keyset": keyset, "fallback": fallback} {
		if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
			t.Fatalf("%s IDs = %+v, want [a b]", name, got)
		}
	}
}

func TestListCustomerTransactionsAsOf_HonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ListCustomerTransactionsAsOf(ctx, store.NewMemoryTransactionRepo(), "c1", Query{
		From: time.Time{}, To: time.Now().UTC(), CreatedThrough: time.Now().UTC(),
	})
	if err != context.Canceled {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestListCustomerTransactionsAsOf_ExclusiveCreatedCutoff(t *testing.T) {
	ctx := context.Background()
	cutoff := time.Date(2026, 7, 5, 2, 0, 0, 0, time.UTC)
	inner := store.NewMemoryTransactionRepo()
	for _, txn := range []domain.Transaction{
		{ID: "before", CustomerID: "c1", ExecutedAt: cutoff.Add(-time.Hour), CreatedAt: cutoff.Add(-time.Nanosecond)},
		{ID: "at", CustomerID: "c1", ExecutedAt: cutoff.Add(-time.Hour), CreatedAt: cutoff},
	} {
		txn := txn
		if err := inner.Create(ctx, &txn); err != nil {
			t.Fatal(err)
		}
	}
	query := Query{From: time.Time{}, To: cutoff, CreatedThrough: cutoff, CreatedBeforeExclusive: true}

	keyset, err := ListCustomerTransactionsAsOf(ctx, inner, "c1", query)
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := ListCustomerTransactionsAsOf(ctx, &legacyTransactionRepo{inner: inner}, "c1", query)
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]domain.Transaction{"keyset": keyset, "fallback": fallback} {
		if len(got) != 1 || got[0].ID != "before" {
			t.Fatalf("%s result = %#v, want only before", name, got)
		}
	}
}
