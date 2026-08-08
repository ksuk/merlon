package domain

import (
	"context"
	"encoding/json"
	"time"
)

type BacktestJobStatus string

const (
	BacktestJobQueued    BacktestJobStatus = "queued"
	BacktestJobRunning   BacktestJobStatus = "running"
	BacktestJobCompleted BacktestJobStatus = "completed"
	BacktestJobFailed    BacktestJobStatus = "failed"
	BacktestJobCancelled BacktestJobStatus = "cancelled"
)

// BacktestCustomerFilter is intentionally narrow and composable. It is
// snapshotted when a job is created; later customer changes do not alter the
// historical run's population.
type BacktestCustomerFilter struct {
	RiskTier    RiskTier       `json:"risk_tier,omitempty"`
	Status      CustomerStatus `json:"status,omitempty"`
	CountryCode string         `json:"country_code,omitempty"`
}

// Matches reports whether a customer belongs to the filtered cohort. A nil
// filter matches everyone.
//
// This lives on the domain type so the pre-execution preview and the worker
// that actually runs the job cannot disagree about who is in the cohort --
// a preview computed by different rules than the run is worse than no preview.
func (f *BacktestCustomerFilter) Matches(c Customer) bool {
	if f == nil {
		return true
	}
	if f.RiskTier != "" && (c.RiskTier == nil || *c.RiskTier != f.RiskTier) {
		return false
	}
	if f.Status != "" && c.EffectiveStatus() != f.Status {
		return false
	}
	if f.CountryCode != "" && c.CountryCode != f.CountryCode {
		return false
	}
	return true
}

type BacktestJob struct {
	ID                   string                  `json:"id"`
	Status               BacktestJobStatus       `json:"status"`
	From                 time.Time               `json:"from"`
	To                   time.Time               `json:"to"`
	CustomerIDs          []string                `json:"customer_ids,omitempty"`
	CustomerFilter       *BacktestCustomerFilter `json:"customer_filter,omitempty"`
	ScenarioIDs          []string                `json:"scenario_ids,omitempty"`
	BaselineRuleSetID    string                  `json:"baseline_rule_set_id"`
	CandidateRuleSetID   string                  `json:"candidate_rule_set_id"`
	BaselineRuleVersion  int                     `json:"baseline_rule_version,omitempty"`
	CandidateRuleVersion int                     `json:"candidate_rule_version,omitempty"`
	// Definitions are immutable job inputs. They are intentionally omitted from
	// the public job representation because rule bodies can contain sensitive
	// operational policy; the version and digest are sufficient for audit APIs.
	BaselineRuleDefinition  json.RawMessage   `json:"-"`
	CandidateRuleDefinition json.RawMessage   `json:"-"`
	ConfigDigests           map[string]string `json:"config_digests,omitempty"`
	SnapshotAt              time.Time         `json:"snapshot_at"`
	TotalCustomers          int               `json:"total_customers"`
	ProcessedCustomers      int               `json:"processed_customers"`
	Progress                float64           `json:"progress"`
	ETASeconds              *int64            `json:"eta_seconds,omitempty"`
	Baseline                *BacktestResult   `json:"baseline,omitempty"`
	Candidate               *BacktestResult   `json:"candidate,omitempty"`
	Delta                   *BacktestResult   `json:"delta,omitempty"`
	Error                   string            `json:"error,omitempty"`
	CreatedAt               time.Time         `json:"created_at"`
	StartedAt               *time.Time        `json:"started_at,omitempty"`
	CompletedAt             *time.Time        `json:"completed_at,omitempty"`
	UpdatedAt               time.Time         `json:"updated_at"`
	Metadata                *BacktestMetadata `json:"metadata,omitempty"`
}

// MarshalJSON keeps empty backtest result collections as arrays. This applies
// both to the immediate backtest response and to completed durable jobs whose
// candidate/baseline/delta results contain no scenario findings.
func (r BacktestResult) MarshalJSON() ([]byte, error) {
	type backtestResult BacktestResult
	normalized := backtestResult(r)
	if normalized.ScenarioResults == nil {
		normalized.ScenarioResults = []BacktestScenarioResult{}
	}
	return json.Marshal(normalized)
}

// MarshalJSON applies the same collection contract to each scenario result
// when it is serialized as part of a backtest result.
func (r BacktestScenarioResult) MarshalJSON() ([]byte, error) {
	type backtestScenarioResult BacktestScenarioResult
	normalized := backtestScenarioResult(r)
	if normalized.AffectedCustomerIDs == nil {
		normalized.AffectedCustomerIDs = []string{}
	}
	return json.Marshal(normalized)
}

type BacktestJobRepository interface {
	Create(ctx context.Context, job *BacktestJob) error
	Get(ctx context.Context, id string) (*BacktestJob, error)
	List(ctx context.Context, limit, offset int) ([]BacktestJob, error)
	Cancel(ctx context.Context, id string) error
	ClaimNext(ctx context.Context) (*BacktestJob, error)
	UpdateProgress(ctx context.Context, id string, processed, total int, etaSeconds *int64) error
	Complete(ctx context.Context, id string, baseline, candidate, delta *BacktestResult) error
	Fail(ctx context.Context, id, reason string) error
}

// BacktestCustomerSnapshotRepository persists the resolved customer population
// separately from the job selector. A filter is evaluated once at claim time,
// then restarts/retries replay the same IDs instead of observing later customer
// mutations.
type BacktestCustomerSnapshotRepository interface {
	SaveCustomerSnapshot(ctx context.Context, jobID string, customerIDs []string) error
	GetCustomerSnapshot(ctx context.Context, jobID string) (customerIDs []string, found bool, err error)
}
