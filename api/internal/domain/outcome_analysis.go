package domain

import (
	"context"
	"time"
)

type OutcomeVariant string

const (
	OutcomeVariantBaseline  OutcomeVariant = "baseline"
	OutcomeVariantCandidate OutcomeVariant = "candidate"
	OutcomeVariantDelta     OutcomeVariant = "delta"
)

// OutcomeSummary is intentionally additive to BacktestResult. Counts include
// every label, while Denominator is only TP+FP; callers can therefore show
// the evidence boundary without treating unlabeled rows as false positives.
type OutcomeSummary struct {
	TP           int     `json:"tp"`
	FP           int     `json:"fp"`
	Unlabeled    int     `json:"unlabeled"`
	Unevaluable  int     `json:"unevaluable"`
	Investigated int     `json:"investigated"`
	Rate         float64 `json:"rate"`
	Denominator  int     `json:"denominator"`
}

type BacktestOutcomeAnalysis struct {
	MatcherVersion string                    `json:"matcher_version"`
	SnapshotAt     time.Time                 `json:"snapshot_at"`
	Assumptions    []string                  `json:"assumptions"`
	Baseline       OutcomeSummary            `json:"baseline"`
	Candidate      OutcomeSummary            `json:"candidate"`
	Delta          OutcomeSummary            `json:"delta"`
	ByScenario     map[string]OutcomeSummary `json:"by_scenario,omitempty"`
	CustomerPeriod []CustomerPeriodOutcome   `json:"customer_period,omitempty"`
	GeneratedAt    time.Time                 `json:"generated_at"`
}

type CustomerPeriodOutcome struct {
	CustomerID string         `json:"customer_id"`
	ScenarioID string         `json:"scenario_id"`
	From       time.Time      `json:"from"`
	To         time.Time      `json:"to"`
	Baseline   OutcomeSummary `json:"baseline"`
	Candidate  OutcomeSummary `json:"candidate"`
	Delta      OutcomeSummary `json:"delta"`
}

type BacktestOutcomeDetail struct {
	ID             string            `json:"id"`
	JobID          string            `json:"job_id"`
	Variant        OutcomeVariant    `json:"variant"`
	ChangeKind     string            `json:"change_kind,omitempty"`
	CandidateID    string            `json:"candidate_id"`
	ReferenceID    string            `json:"reference_id,omitempty"`
	CustomerID     string            `json:"customer_id"`
	ScenarioID     string            `json:"scenario_id,omitempty"`
	Label          string            `json:"label"`
	Metric         string            `json:"metric,omitempty"`
	Score          float64           `json:"score,omitempty"`
	Investigated   bool              `json:"investigated"`
	MatchedAlertID string            `json:"matched_alert_id,omitempty"`
	MatchedCaseID  string            `json:"matched_case_id,omitempty"`
	MatcherVersion string            `json:"matcher_version"`
	Assumptions    []string          `json:"assumptions"`
	SnapshotAt     time.Time         `json:"snapshot_at"`
	Provenance     map[string]string `json:"provenance,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
}

type BacktestOutcomeFilter struct {
	JobID      string
	Variant    OutcomeVariant
	ScenarioID string
	Label      string
	Cursor     *Cursor
	Limit      int
}

type BacktestOutcomeRepository interface {
	SaveBacktestOutcomeAnalysis(ctx context.Context, jobID string, analysis *BacktestOutcomeAnalysis, details []BacktestOutcomeDetail) error
	GetBacktestOutcomeAnalysis(ctx context.Context, jobID string) (*BacktestOutcomeAnalysis, error)
	ListBacktestOutcomeDetails(ctx context.Context, filter BacktestOutcomeFilter) ([]BacktestOutcomeDetail, error)
}

type CoverageAnalysisStatus string

const (
	CoverageAnalysisQueued    CoverageAnalysisStatus = "queued"
	CoverageAnalysisRunning   CoverageAnalysisStatus = "running"
	CoverageAnalysisCompleted CoverageAnalysisStatus = "completed"
	CoverageAnalysisFailed    CoverageAnalysisStatus = "failed"
)

const CoverageAnalysisKind = "comparison/known_matter_coverage"

type CoverageSummary struct {
	KnownMatter int     `json:"known_matter"`
	Covered     int     `json:"covered"`
	NotCovered  int     `json:"not_covered"`
	Unevaluable int     `json:"unevaluable"`
	Rate        float64 `json:"rate"`
	Denominator int     `json:"denominator"`
}

type CoverageAnalysis struct {
	ID             string                     `json:"id"`
	Kind           string                     `json:"kind"`
	Status         CoverageAnalysisStatus     `json:"status"`
	ScenarioIDs    []string                   `json:"scenario_ids,omitempty"`
	CustomerIDs    []string                   `json:"customer_ids,omitempty"`
	From           time.Time                  `json:"from"`
	To             time.Time                  `json:"to"`
	RuleSetID      string                     `json:"rule_set_id"`
	SnapshotAt     time.Time                  `json:"snapshot_at"`
	MatcherVersion string                     `json:"matcher_version"`
	Assumptions    []string                   `json:"assumptions"`
	Summary        CoverageSummary            `json:"summary"`
	ByScenario     map[string]CoverageSummary `json:"by_scenario,omitempty"`
	Error          string                     `json:"error,omitempty"`
	CreatedAt      time.Time                  `json:"created_at"`
	StartedAt      *time.Time                 `json:"started_at,omitempty"`
	CompletedAt    *time.Time                 `json:"completed_at,omitempty"`
	UpdatedAt      time.Time                  `json:"updated_at"`
}

type CoverageMatterResult struct {
	ID             string            `json:"id"`
	AnalysisID     string            `json:"analysis_id"`
	MatterID       string            `json:"matter_id"`
	CustomerID     string            `json:"customer_id"`
	ScenarioIDs    []string          `json:"scenario_ids,omitempty"`
	Source         string            `json:"source"`
	Label          string            `json:"label"`
	Covered        bool              `json:"covered"`
	Unevaluable    bool              `json:"unevaluable"`
	MatchedAlertID string            `json:"matched_alert_id,omitempty"`
	MatcherVersion string            `json:"matcher_version"`
	Assumptions    []string          `json:"assumptions"`
	SnapshotAt     time.Time         `json:"snapshot_at"`
	Provenance     map[string]string `json:"provenance,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
}

type CoverageAnalysisFilter struct {
	Status CoverageAnalysisStatus
	Limit  int
	Offset int
}

type CoverageMatterFilter struct {
	AnalysisID string
	ScenarioID string
	Label      string
	Cursor     *Cursor
	Limit      int
}

type CoverageAnalysisRepository interface {
	CreateCoverageAnalysis(ctx context.Context, analysis *CoverageAnalysis) error
	GetCoverageAnalysis(ctx context.Context, id string) (*CoverageAnalysis, error)
	ListCoverageAnalyses(ctx context.Context, filter CoverageAnalysisFilter) ([]CoverageAnalysis, error)
	StartCoverageAnalysis(ctx context.Context, id string) (*CoverageAnalysis, error)
	CompleteCoverageAnalysis(ctx context.Context, id string, summary CoverageSummary, byScenario map[string]CoverageSummary) error
	FailCoverageAnalysis(ctx context.Context, id, reason string) error
	SaveCoverageMatterResults(ctx context.Context, id string, results []CoverageMatterResult) error
	ListCoverageMatterResults(ctx context.Context, filter CoverageMatterFilter) ([]CoverageMatterResult, error)
}
