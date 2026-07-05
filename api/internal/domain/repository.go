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
	// ListEDDPending returns High-tier customers with an open EDD requirement
	// (edd_requested_at set), for RunEDDEscalationJob (case-management.md §EDD
	// 未実施継続時の段階的措置). The job itself computes elapsed days per
	// customer and decides which stage (if any) applies.
	ListEDDPending(ctx context.Context) ([]Customer, error)
}

type TransactionRepository interface {
	Get(ctx context.Context, id string) (*Transaction, error)
	ListByCustomer(ctx context.Context, customerID string, limit, offset int) ([]Transaction, error)
	ListByCustomerCursor(ctx context.Context, customerID string, limit int, after *Cursor) ([]Transaction, error)
	Create(ctx context.Context, t *Transaction) error
}

// AlertBulkFilter narrows ListByFilter's results for bulk alert operations
// (case-management.md §アラートの一括処理: "フィルタ条件（シナリオID、期間、
// severity）"). Zero-value fields are wildcards (no restriction on that axis).
type AlertBulkFilter struct {
	ScenarioID string
	PeriodFrom *time.Time
	PeriodTo   *time.Time
	Severity   AlertSeverity
}

type AlertRepository interface {
	Get(ctx context.Context, id string) (*Alert, error)
	ListByCustomer(ctx context.Context, customerID string, limit, offset int) ([]Alert, error)
	ListByCustomerCursor(ctx context.Context, customerID string, limit int, after *Cursor) ([]Alert, error)
	ListOpen(ctx context.Context, limit, offset int) ([]Alert, error)
	ListOpenByCursor(ctx context.Context, limit int, after *Cursor) ([]Alert, error)
	Create(ctx context.Context, a *Alert) error
	UpdateStatus(ctx context.Context, id string, status AlertStatus, resolvedBy string) error
	// CreateIfNotDuplicate inserts a unless another alert already exists for
	// the same (customer_id, scenario_id, aggregation_window_start) tuple
	// (transaction-monitoring.md「バッチ/リアルタイム評価の重複アラート防止」).
	// A nil AggregationWindowStart is exempt from the constraint (scenarios
	// with no aggregation window), so it always creates. On conflict,
	// created=false and existing holds the pre-existing alert; on success,
	// created=true and existing is nil.
	CreateIfNotDuplicate(ctx context.Context, a *Alert) (created bool, existing *Alert, err error)
	// AnnotateBatchReviewed records that batchRunID's batch pass reviewed
	// alertID after finding it already exists for the same scenario/window
	// (Task4/Task7 dedup routing), without altering its status or severity.
	AnnotateBatchReviewed(ctx context.Context, alertID string, batchRunID string) error
	// ListByFilter returns alerts matching f, for bulk operations (WS-8 Task 7).
	ListByFilter(ctx context.Context, f AlertBulkFilter) ([]Alert, error)
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
