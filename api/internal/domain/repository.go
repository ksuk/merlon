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
	// (edd_requested_at set), for RunEDDEscalationJob (the case-management workflow §EDD
	// 未実施継続時の段階的措置). The job itself computes elapsed days per
	// customer and decides which stage (if any) applies.
	ListEDDPending(ctx context.Context) ([]Customer, error)
	// UpdateStatus reflects a customer_status_changed notification from the
	// core system (the data model §1.1.2). This system does not judge
	// transition validity; it records whatever status it is told (Adapter
	// Isolation). reason is stored by the caller for the audit log entry,
	// not persisted on the customer row itself.
	UpdateStatus(ctx context.Context, id string, status CustomerStatus, reason string) (*Customer, error)
}

// CustomerSearchRepository is the optional cursor/offset search capability
// used by the operator list. Keeping search separate preserves the stable
// CustomerRepository contract for adapters that do not expose server-side
// search yet.
type CustomerSearchRepository interface {
	ListByCursorSearch(ctx context.Context, limit int, after *Cursor, search string) ([]Customer, error)
	ListSearch(ctx context.Context, search string, limit, offset int) ([]Customer, error)
}

// CustomerDashboardRepository exposes aggregate reads used by the dashboard.
// It is optional so adapters can adopt the read model without changing the
// CRUD CustomerRepository contract.
type CustomerDashboardRepository interface {
	DashboardRiskTierCounts(ctx context.Context) (map[string]int, error)
}

type TransactionRepository interface {
	Get(ctx context.Context, id string) (*Transaction, error)
	ListByCustomer(ctx context.Context, customerID string, limit, offset int) ([]Transaction, error)
	ListByCustomerCursor(ctx context.Context, customerID string, limit int, after *Cursor) ([]Transaction, error)
	Create(ctx context.Context, t *Transaction) error
}

// TransactionHistoryRepository is an optional capability used by PH9 batch
// and realtime evaluators. It orders by event time (not ingestion time), pins
// a created_at snapshot, and carries an explicit half-open event window.
type TransactionHistoryRepository interface {
	ListByCustomerEventRange(ctx context.Context, customerID string, from, to, createdBefore time.Time, limit int, after *TransactionEventCursor) ([]Transaction, error)
}

// TransactionDashboardRepository provides a bounded aggregate for the
// dashboard's documented recent-transaction window.
type TransactionDashboardRepository interface {
	CountExecutedSince(ctx context.Context, since time.Time) (int, error)
}

type TransactionEventCursor struct {
	ExecutedAt time.Time
	ID         string
}

// AlertBulkFilter narrows ListByFilter's results for bulk alert operations
// (the case-management workflow §アラートの一括処理: "フィルタ条件（シナリオID、期間、
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
	// UpdateStatusIfUnmodified applies the same update as UpdateStatus, but
	// only if the alert's stored updated_at still equals expectedUpdatedAt
	// (optimistic locking, the data model §3.9). Returns *ErrConflict on
	// mismatch.
	UpdateStatusIfUnmodified(ctx context.Context, id string, status AlertStatus, resolvedBy string, expectedUpdatedAt time.Time) error
	// CreateIfNotDuplicate inserts a unless another alert already exists for
	// the same (customer_id, scenario_id, aggregation_window_start) tuple
	// (the transaction-monitoring design「バッチ/リアルタイム評価の重複アラート防止」).
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
	// EscalateSeverity raises a single alert's severity (the data model
	// §1.1.2: customer death -> frozen escalates all of that customer's
	// alerts to HIGH), independent of its status.
	EscalateSeverity(ctx context.Context, id string, severity AlertSeverity) error
}

// AlertDashboardRepository provides unresolved alert aggregates without
// materializing a capped list in the request handler.
type AlertDashboardRepository interface {
	DashboardUnresolvedCounts(ctx context.Context) (byStatus, bySeverity map[string]int, err error)
}

// CaseDashboardRepository provides unresolved case aggregates without
// materializing a capped list in the request handler.
type CaseDashboardRepository interface {
	DashboardUnresolvedCounts(ctx context.Context) (map[string]int, error)
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
