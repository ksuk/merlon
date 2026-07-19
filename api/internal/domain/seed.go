package domain

import (
	"context"
	"time"
)

// SeedDatasetKind records the provenance of the initial dataset. It is kept
// separately from the customer rows so an existing non-demo database cannot
// be mistaken for synthetic data on restart.
type SeedDatasetKind string

const (
	SeedDatasetDemo      SeedDatasetKind = "demo"
	SeedDatasetHardcoded SeedDatasetKind = "hardcoded"
)

type SeedState struct {
	DatasetKind SeedDatasetKind
	CompletedAt time.Time
}

type SeedStateRepository interface {
	Get(ctx context.Context) (*SeedState, error)
	MarkCompleted(ctx context.Context, kind SeedDatasetKind) error
}
