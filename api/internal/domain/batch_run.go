package domain

import (
	"context"
	"time"
)

type BatchRunStatus string

const (
	BatchRunStatusRunning   BatchRunStatus = "running"
	BatchRunStatusCompleted BatchRunStatus = "completed"
	BatchRunStatusFailed    BatchRunStatus = "failed"
)

// BatchRun tracks one execution of a periodic batch job (e.g.
// "tm_batch_evaluation") so it can be resumed idempotently after a mid-run
// failure or process kill (the operational design §4.4 バッチジョブ障害復旧).
// ProcessedCustomerIDs records progress: a resumed run skips any customer ID
// already present here instead of re-evaluating it.
type BatchRun struct {
	ID                   string
	JobType              string
	Status               BatchRunStatus
	StartedAt            time.Time
	CompletedAt          *time.Time
	ProcessedCustomerIDs []string
}

type BatchRunRepository interface {
	Create(ctx context.Context, r *BatchRun) error
	Get(ctx context.Context, id string) (*BatchRun, error)
	// GetLatestRunning returns the most recent run of jobType still in
	// status=running, or nil if none exists. A restarted process uses this
	// to resume a run left behind by a kill instead of starting a duplicate
	// one (the operational design §4.4「再起動時は未処理分のみを再開」).
	GetLatestRunning(ctx context.Context, jobType string) (*BatchRun, error)
	// AppendProcessedCustomer records that customerID has been evaluated
	// under run id, so a subsequent resume of the same run skips it.
	AppendProcessedCustomer(ctx context.Context, id string, customerID string) error
	Complete(ctx context.Context, id string) error
	Fail(ctx context.Context, id string) error
}
