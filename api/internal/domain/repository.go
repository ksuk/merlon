package domain

import (
	"context"
	"time"
)

// Cursor is the keyset sort position (created_at, id) used for cursor-based
// pagination. It mirrors server.Cursor without introducing a dependency from
// domain on the server package.
type Cursor struct {
	CreatedAt time.Time
	ID        string
}

type CustomerRepository interface {
	Get(ctx context.Context, id string) (*Customer, error)
	GetByExternalID(ctx context.Context, externalID string) (*Customer, error)
	List(ctx context.Context, limit, offset int) ([]Customer, error)
	ListByCursor(ctx context.Context, limit int, after *Cursor) ([]Customer, error)
	Create(ctx context.Context, c *Customer) error
	Update(ctx context.Context, c *Customer) error
	SaveScoreRecord(ctx context.Context, r *ScoreRecord) error
	ListScoreHistory(ctx context.Context, customerID string, limit int) ([]ScoreRecord, error)
}

type TransactionRepository interface {
	Get(ctx context.Context, id string) (*Transaction, error)
	ListByCustomer(ctx context.Context, customerID string, limit, offset int) ([]Transaction, error)
	ListByCustomerCursor(ctx context.Context, customerID string, limit int, after *Cursor) ([]Transaction, error)
	Create(ctx context.Context, t *Transaction) error
}

type AlertRepository interface {
	Get(ctx context.Context, id string) (*Alert, error)
	ListByCustomer(ctx context.Context, customerID string, limit, offset int) ([]Alert, error)
	ListByCustomerCursor(ctx context.Context, customerID string, limit int, after *Cursor) ([]Alert, error)
	ListOpen(ctx context.Context, limit, offset int) ([]Alert, error)
	ListOpenByCursor(ctx context.Context, limit int, after *Cursor) ([]Alert, error)
	Create(ctx context.Context, a *Alert) error
	UpdateStatus(ctx context.Context, id string, status AlertStatus, resolvedBy string) error
}

type ErrNotFound struct {
	Entity string
	ID     string
}

func (e *ErrNotFound) Error() string {
	return e.Entity + " not found: " + e.ID
}

// ErrConflict signals a version mismatch (optimistic locking) or a unique
// constraint violation (e.g. the whitelist_entries partial unique index on
// active entries per customer, whitelist.md §3.1). Callers translate this to
// HTTP 409.
type ErrConflict struct {
	Entity string
	ID     string
	Reason string
}

func (e *ErrConflict) Error() string {
	return e.Entity + " conflict: " + e.ID + ": " + e.Reason
}
