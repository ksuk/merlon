package domain

import (
	"context"
	"strconv"
	"time"
)

// RetentionPolicy governs how long a data category is kept before automatic
// purge. Deployments may configure positive retention periods. The optional
// MinRetentionDays field is retained for API and schema compatibility.
type RetentionPolicy struct {
	ID               string    `json:"id"`
	DataCategory     string    `json:"data_category"`
	RetentionDays    int       `json:"retention_days"`
	MinRetentionDays *int      `json:"min_retention_days,omitempty"`
	UpdatedBy        string    `json:"updated_by,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ErrRetentionShorten is retained for legacy/custom databases that configure
// MinRetentionDays. Current defaults are deployment-controlled and have no
// built-in minimum. Callers translate this validation error to HTTP 400.
type ErrRetentionShorten struct {
	DataCategory  string
	RequestedDays int
	MinDays       int
}

func (e *ErrRetentionShorten) Error() string {
	return "retention_shorten_forbidden: " + e.DataCategory + " retention may not be shortened below " +
		strconv.Itoa(e.MinDays) + " days (requested " + strconv.Itoa(e.RequestedDays) + ")"
}

type ErrInvalidRetentionDays struct{ Days int }

func (e *ErrInvalidRetentionDays) Error() string {
	return "retention_days must be greater than zero (requested " + strconv.Itoa(e.Days) + ")"
}

type RetentionRepository interface {
	List(ctx context.Context) ([]RetentionPolicy, error)
	Get(ctx context.Context, dataCategory string) (*RetentionPolicy, error)
	// Update sets a positive retention_days value. Returns *ErrNotFound if the
	// data category is unknown.
	Update(ctx context.Context, dataCategory string, retentionDays int, updatedBy string) (*RetentionPolicy, error)
}
