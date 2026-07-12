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
// by the Rust engine (OPS-005, the operational design §4.4 Fail-Alert). It is queued
// rather than dropped so that detection resumes automatically once the
// engine recovers (Task 5's RecoveryJob).
type PendingEvaluation struct {
	ID             string                   `json:"id"`
	CustomerID     string                   `json:"customer_id"`
	TransactionIDs []string                 `json:"transaction_ids"`
	Status         PendingEvaluationStatus  `json:"status"`
	Reason         string                   `json:"reason"`
	BatchRunID     *string                  `json:"batch_run_id,omitempty"`
	RetryCount     int                      `json:"retry_count"`
	ResolvedAt     *time.Time               `json:"resolved_at,omitempty"`
	CreatedAt      time.Time                `json:"created_at"`
	UpdatedAt      time.Time                `json:"updated_at"`
}

type PendingEvaluationRepository interface {
	Create(ctx context.Context, pe *PendingEvaluation) error
	Get(ctx context.Context, id string) (*PendingEvaluation, error)
	ListByStatus(ctx context.Context, status PendingEvaluationStatus, limit, offset int) ([]PendingEvaluation, error)
	UpdateStatus(ctx context.Context, id string, status PendingEvaluationStatus) error
	IncrementRetry(ctx context.Context, id string) error
}
