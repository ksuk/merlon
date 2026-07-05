package domain

import (
	"context"
	"strconv"
	"time"
)

// RetentionPolicy governs how long a data category is kept before automatic
// purge (audit.md RET-001/RET-002, §6 保持期間表). Statutory categories
// (everything but audit_log) carry a non-nil MinRetentionDays equal to their
// seeded RetentionDays, so RetentionDays may only be extended, never
// shortened (migrations/017_retention.sql retention_no_shorten CHECK).
type RetentionPolicy struct {
	ID               string    `json:"id"`
	DataCategory     string    `json:"data_category"`
	RetentionDays    int       `json:"retention_days"`
	MinRetentionDays *int      `json:"min_retention_days,omitempty"`
	UpdatedBy        string    `json:"updated_by,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ErrRetentionShorten signals an attempted update that would reduce a
// statutory data category's retention below its legally required minimum
// (RET-002: 延長のみ可, 設計原則5). Callers translate this to HTTP 400 (not
// 409 — this is a validation failure of the request body, not a concurrent
// modification conflict).
type ErrRetentionShorten struct {
	DataCategory  string
	RequestedDays int
	MinDays       int
}

func (e *ErrRetentionShorten) Error() string {
	return "retention_shorten_forbidden: " + e.DataCategory + " retention may not be shortened below " +
		strconv.Itoa(e.MinDays) + " days (requested " + strconv.Itoa(e.RequestedDays) + ")"
}

type RetentionRepository interface {
	List(ctx context.Context) ([]RetentionPolicy, error)
	Get(ctx context.Context, dataCategory string) (*RetentionPolicy, error)
	// Update sets retention_days (延長方向のみ想定). Returns *ErrRetentionShorten
	// if retentionDays is below the category's MinRetentionDays (defense in
	// depth alongside the DB CHECK constraint and the server-layer
	// pre-check); *ErrNotFound if dataCategory is unknown.
	Update(ctx context.Context, dataCategory string, retentionDays int, updatedBy string) (*RetentionPolicy, error)
}
