package domain

import (
	"context"
	"time"
)

// ScreeningRun is the durable unit of one customer screening request.  A run
// exists even when it produces zero matches, which lets operators distinguish
// "screened and clear" from "never screened" after a restart.
type ScreeningRunStatus string

const (
	ScreeningRunRunning   ScreeningRunStatus = "running"
	ScreeningRunCompleted ScreeningRunStatus = "completed"
	ScreeningRunFailed    ScreeningRunStatus = "failed"
	ScreeningRunPartial   ScreeningRunStatus = "partial"
)

type ScreeningRun struct {
	ID            string             `json:"id"`
	CustomerID    string             `json:"customer_id"`
	ListIDs       []string           `json:"list_ids"`
	ConfigDigests map[string]string  `json:"config_digests"`
	Status        ScreeningRunStatus `json:"status"`
	ResultCount   int                `json:"result_count"`
	Error         string             `json:"error,omitempty"`
	Actor         string             `json:"actor"`
	// Degraded records that at least one required watchlist source was not
	// ready when the run started, so a clear result cannot be read as
	// evidence that the customer is absent from every list.
	Degraded        bool       `json:"degraded"`
	DegradedSources []string   `json:"degraded_sources,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// ScreeningDegradation is the readiness verdict applied to a screening run:
// which required sources were unusable, and therefore what the run's results
// cannot be taken to prove.
type ScreeningDegradation struct {
	Degraded bool     `json:"degraded"`
	Sources  []string `json:"sources,omitempty"`
}

type ScreeningResultFilter struct {
	CustomerID string
	Status     ScreeningResultStatus
	ListID     string
	From       *time.Time
	To         *time.Time
	// Suppressed selects only suppressed (true) or only unsuppressed (false)
	// results. Nil, the zero value, returns both, which keeps every caller
	// that predates suppression filtering unchanged.
	Suppressed *bool
	Cursor     *Cursor
}

type ScreeningResultHistoryEntry struct {
	ID                string                `json:"id"`
	ScreeningResultID string                `json:"screening_result_id"`
	FromStatus        ScreeningResultStatus `json:"from_status"`
	ToStatus          ScreeningResultStatus `json:"to_status"`
	Rationale         string                `json:"rationale"`
	Actor             string                `json:"actor"`
	Version           int                   `json:"version"`
	CreatedAt         time.Time             `json:"created_at"`
}

type ScreeningReviewOutcome struct {
	Result      *ScreeningResultRecord `json:"result"`
	CaseID      string                 `json:"case_id,omitempty"`
	CaseCreated bool                   `json:"case_created"`
}

type ScreeningSourceState string

const (
	ScreeningSourceNeverImported ScreeningSourceState = "never_imported"
	ScreeningSourceReady         ScreeningSourceState = "ready"
	ScreeningSourceStale         ScreeningSourceState = "stale"
	ScreeningSourceUnreadable    ScreeningSourceState = "unreadable"
	ScreeningSourceFailed        ScreeningSourceState = "failed"
	ScreeningSourceUnavailable   ScreeningSourceState = "unavailable"
)

type ScreeningSourceStatus struct {
	ListID                    string               `json:"list_id"`
	ListType                  string               `json:"list_type"`
	Configured                bool                 `json:"configured"`
	OperationalState          ScreeningSourceState `json:"operational_state"`
	LastAttemptAt             *time.Time           `json:"last_attempt_at,omitempty"`
	LastFailureAt             *time.Time           `json:"last_failure_at,omitempty"`
	LastSuccessAt             *time.Time           `json:"last_success_at,omitempty"`
	AgeSeconds                *int64               `json:"age_seconds,omitempty"`
	FreshnessThresholdSeconds int64                `json:"freshness_threshold_seconds"`
	ConsecutiveFailures       int                  `json:"consecutive_failures"`
	Diagnostic                string               `json:"diagnostic,omitempty"`
}

type BacktestMetadata struct {
	JobID             string         `json:"job_id"`
	Rationale         string         `json:"rationale"`
	CohortPreview     map[string]any `json:"cohort_preview"`
	BaselineSnapshot  map[string]any `json:"baseline_snapshot"`
	CandidateSnapshot map[string]any `json:"candidate_snapshot"`
	RerunOf           string         `json:"rerun_of,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
}

type TargetMode string

const (
	TargetModeSelected TargetMode = "selected"
	TargetModeFilter   TargetMode = "filter"
	TargetModeAll      TargetMode = "all"
)

type TargetManifest struct {
	ID                string            `json:"id"`
	Operation         string            `json:"operation"`
	TargetMode        TargetMode        `json:"target_mode"`
	CustomerIDs       []string          `json:"customer_ids"`
	Filter            map[string]any    `json:"filter"`
	SampleCustomerIDs []string          `json:"sample_customer_ids"`
	TargetCount       int               `json:"target_count"`
	Criteria          string            `json:"criteria"`
	RuleSetID         string            `json:"rule_set_id,omitempty"`
	RuleSetVersion    int               `json:"rule_set_version,omitempty"`
	ConfigDigests     map[string]string `json:"config_digests,omitempty"`
	Token             string            `json:"token,omitempty"`
	IdempotencyKey    string            `json:"idempotency_key,omitempty"`
	Rationale         string            `json:"rationale"`
	Status            string            `json:"status"`
	Version           int               `json:"version"`
	ExpiresAt         time.Time         `json:"expires_at"`
	CreatedBy         string            `json:"created_by"`
	CreatedAt         time.Time         `json:"created_at"`
	ConfirmedAt       *time.Time        `json:"confirmed_at,omitempty"`
	RunID             string            `json:"run_id,omitempty"`
}

type PendingEvaluationFilter struct {
	Status      []PendingEvaluationStatus
	CustomerID  string
	BatchRunID  string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Cursor      *Cursor
}

type PendingEvaluationHistoryEntry struct {
	ID                  string                  `json:"id"`
	PendingEvaluationID string                  `json:"pending_evaluation_id"`
	FromStatus          PendingEvaluationStatus `json:"from_status"`
	ToStatus            PendingEvaluationStatus `json:"to_status"`
	Action              string                  `json:"action"`
	Reason              string                  `json:"reason"`
	Actor               string                  `json:"actor"`
	RetryCount          int                     `json:"retry_count"`
	CreatedAt           time.Time               `json:"created_at"`
}

type CustomerIdentityHistoryEntry struct {
	ID            string         `json:"id"`
	CustomerID    string         `json:"customer_id"`
	ChangedFields map[string]any `json:"changed_fields"`
	Actor         string         `json:"actor"`
	Rationale     string         `json:"rationale"`
	CreatedAt     time.Time      `json:"created_at"`
}

// ScreeningWorkflowRepository is deliberately additive to
// ScreeningResultRepository.  Existing adapters can continue serving the
// legacy result contract while Wave 3-aware adapters opt into durable runs,
// cursor reads and CAS review in one unit of work.
type ScreeningWorkflowRepository interface {
	PersistScreeningRun(ctx context.Context, run *ScreeningRun, results []ScreeningResultRecord) error
	GetScreeningRun(ctx context.Context, id string) (*ScreeningRun, error)
	GetScreeningResult(ctx context.Context, id string) (*ScreeningResultRecord, error)
	ListScreeningRuns(ctx context.Context, customerID string, limit int, after *Cursor) ([]ScreeningRun, error)
	ListScreeningResults(ctx context.Context, filter ScreeningResultFilter, limit int) ([]ScreeningResultRecord, error)
	ReviewScreeningResult(ctx context.Context, id string, to ScreeningResultStatus, reason, actor string, expectedVersion int) (*ScreeningReviewOutcome, error)
	ListScreeningResultHistory(ctx context.Context, id string, limit int) ([]ScreeningResultHistoryEntry, error)
	// ListScreeningSources takes a per-source freshness window rather than one
	// global duration: a daily sanctions feed and a monthly PEP refresh are
	// not stale at the same age, and the screening_readiness policy expresses
	// that difference. thresholdFor must be non-nil.
	ListScreeningSources(ctx context.Context, configuredIDs []string, thresholdFor func(listID string) time.Duration) ([]ScreeningSourceStatus, error)
}

type BacktestMetadataRepository interface {
	SaveBacktestMetadata(ctx context.Context, metadata *BacktestMetadata) error
	GetBacktestMetadata(ctx context.Context, jobID string) (*BacktestMetadata, error)
}

type TargetManifestRepository interface {
	CreateTargetManifest(ctx context.Context, manifest *TargetManifest) error
	GetTargetManifest(ctx context.Context, id string) (*TargetManifest, error)
	ConfirmTargetManifest(ctx context.Context, id, token, actor, rationale, idempotencyKey string, expectedVersion int) (*TargetManifest, error)
	ClaimTargetManifest(ctx context.Context, id, runID string, expectedVersion int) (*TargetManifest, error)
}

type PendingEvaluationWorkflowRepository interface {
	ListPendingEvaluations(ctx context.Context, filter PendingEvaluationFilter, limit int) ([]PendingEvaluation, error)
	ListPendingHistory(ctx context.Context, id string, limit int) ([]PendingEvaluationHistoryEntry, error)
	TransitionPendingEvaluation(ctx context.Context, id, action, actor, reason string, expectedVersion int) (*PendingEvaluation, error)
	SetPendingEvaluationAlertIDs(ctx context.Context, id string, alertIDs []string, expectedVersion int) error
}

type BatchRunFilter struct {
	Operation string
	Status    BatchRunStatus
	Cursor    *Cursor
}

type BatchRunWorkflowRepository interface {
	ListBatchRuns(ctx context.Context, filter BatchRunFilter, limit int) ([]BatchRun, error)
	UpdateBatchRun(ctx context.Context, id string, status BatchRunStatus, resultCounts map[string]int, failure string) error
	RecordBatchRunOutcome(ctx context.Context, runID string, outcome BatchRunCustomerOutcome) error
	FindBatchRunByIdempotency(ctx context.Context, operation, key string) (*BatchRun, error)
}

type CustomerIdentityHistoryRepository interface {
	AppendCustomerIdentityHistory(ctx context.Context, entry *CustomerIdentityHistoryEntry) error
	ListCustomerIdentityHistory(ctx context.Context, customerID string, limit int, after *Cursor) ([]CustomerIdentityHistoryEntry, error)
}

// Wave3Repository groups the new capabilities for dependency injection while
// each narrower interface remains available for small adapters and tests.
type Wave3Repository interface {
	ScreeningWorkflowRepository
	BacktestMetadataRepository
	TargetManifestRepository
	CustomerIdentityHistoryRepository
}
