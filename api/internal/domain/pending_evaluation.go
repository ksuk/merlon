package domain

import (
	"context"
	"time"
)

type PendingEvaluationStatus string

const (
	PendingEvaluationStatusPendingReview PendingEvaluationStatus = "PENDING_REVIEW"
	PendingEvaluationStatusProcessing    PendingEvaluationStatus = "PROCESSING"
	PendingEvaluationStatusResolved      PendingEvaluationStatus = "RESOLVED"
	PendingEvaluationStatusFailed        PendingEvaluationStatus = "FAILED"
)

// PendingEvaluation records a transaction batch that could not be evaluated
// by the evaluation engine (OPS-005, the operational design §4.4 Fail-Alert). It is queued
// rather than dropped so that detection resumes automatically once the
// engine recovers (Task 5's RecoveryJob).
type PendingEvaluation struct {
	ID             string                  `json:"id"`
	CustomerID     string                  `json:"customer_id"`
	TransactionIDs []string                `json:"transaction_ids"`
	Status         PendingEvaluationStatus `json:"status"`
	Reason         string                  `json:"reason"`
	BatchRunID     *string                 `json:"batch_run_id,omitempty"`
	AlertIDs       []string                `json:"alert_ids,omitempty"`
	RetryCount     int                     `json:"retry_count"`
	ResolvedAt     *time.Time              `json:"resolved_at,omitempty"`
	LastAttemptAt  *time.Time              `json:"last_attempt_at,omitempty"`
	NextRetryAt    *time.Time              `json:"next_retry_at,omitempty"`
	EscalatedAt    *time.Time              `json:"escalated_at,omitempty"`
	Version        int                     `json:"version"`
	CreatedAt      time.Time               `json:"created_at"`
	UpdatedAt      time.Time               `json:"updated_at"`
}

type PendingEvaluationRepository interface {
	Create(ctx context.Context, pe *PendingEvaluation) error
	Get(ctx context.Context, id string) (*PendingEvaluation, error)
	ListByStatus(ctx context.Context, status PendingEvaluationStatus, limit, offset int) ([]PendingEvaluation, error)
	UpdateStatus(ctx context.Context, id string, status PendingEvaluationStatus) error
	IncrementRetry(ctx context.Context, id string) error
}

// PendingEvaluationBulkLookup is an optional repository capability used by
// batch fail-alert queueing to avoid one lookup per failed customer while
// preserving transaction-level deduplication semantics.
type PendingEvaluationBulkLookup interface {
	ListPendingByCustomers(ctx context.Context, customerIDs []string, status PendingEvaluationStatus) ([]PendingEvaluation, error)
}
