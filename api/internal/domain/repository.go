package domain

import "context"

type CustomerRepository interface {
	Get(ctx context.Context, id string) (*Customer, error)
	GetByExternalID(ctx context.Context, externalID string) (*Customer, error)
	List(ctx context.Context, limit, offset int) ([]Customer, error)
	Create(ctx context.Context, c *Customer) error
	Update(ctx context.Context, c *Customer) error
	SaveScoreRecord(ctx context.Context, r *ScoreRecord) error
	ListScoreHistory(ctx context.Context, customerID string, limit int) ([]ScoreRecord, error)
}

type TransactionRepository interface {
	Get(ctx context.Context, id string) (*Transaction, error)
	ListByCustomer(ctx context.Context, customerID string, limit, offset int) ([]Transaction, error)
	Create(ctx context.Context, t *Transaction) error
}

type AlertRepository interface {
	Get(ctx context.Context, id string) (*Alert, error)
	ListByCustomer(ctx context.Context, customerID string, limit, offset int) ([]Alert, error)
	ListOpen(ctx context.Context, limit, offset int) ([]Alert, error)
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
