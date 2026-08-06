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
	BatchRunStatusPartial   BatchRunStatus = "partial"
	BatchRunStatusCancelled BatchRunStatus = "cancelled"
)

type BatchRunCustomerOutcomeStatus string

const (
	BatchRunCustomerSucceeded BatchRunCustomerOutcomeStatus = "succeeded"
	BatchRunCustomerFailed    BatchRunCustomerOutcomeStatus = "failed"
	BatchRunCustomerQueued    BatchRunCustomerOutcomeStatus = "queued"
	BatchRunCustomerError     BatchRunCustomerOutcomeStatus = "error"
)

type BatchRunCustomerOutcome struct {
	CustomerID string                        `json:"customer_id"`
	Status     BatchRunCustomerOutcomeStatus `json:"status"`
	AlertIDs   []string                      `json:"alert_ids,omitempty"`
	Error      string                        `json:"error,omitempty"`
	Attempt    int                           `json:"attempt"`
	UpdatedAt  time.Time                     `json:"updated_at"`
}

// BatchRun tracks one execution of a periodic batch job (e.g.
// "tm_batch_evaluation") so it can be resumed idempotently after a mid-run
// failure or process kill (the operational design §4.4 バッチジョブ障害復旧).
// ProcessedCustomerIDs records progress: a resumed run skips any customer ID
// already present here instead of re-evaluating it.
type BatchRun struct {
	ID                   string                             `json:"id"`
	JobType              string                             `json:"job_type"`
	Status               BatchRunStatus                     `json:"status"`
	StartedAt            time.Time                          `json:"started_at"`
	CompletedAt          *time.Time                         `json:"completed_at,omitempty"`
	ProcessedCustomerIDs []string                           `json:"processed_customer_ids"`
	Operation            string                             `json:"operation"`
	Parameters           map[string]any                     `json:"parameters"`
	TargetManifestID     string                             `json:"target_manifest_id"`
	ConfigDigests        map[string]string                  `json:"config_digests"`
	Actor                string                             `json:"actor"`
	ResultCounts         map[string]int                     `json:"result_counts"`
	Error                string                             `json:"error,omitempty"`
	RerunOf              string                             `json:"rerun_of,omitempty"`
	CustomerOutcomes     map[string]BatchRunCustomerOutcome `json:"customer_outcomes,omitempty"`
	UpdatedAt            time.Time                          `json:"updated_at"`
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

// BatchRunProgressRepository atomically claims one customer for a run. A
// worker claims before applying the customer's mutation, so two recovery
// workers cannot both commit work for the same run/customer pair.
type BatchRunProgressRepository interface {
	AppendProcessedCustomerIfAbsent(ctx context.Context, id string, customerID string) (bool, error)
}
