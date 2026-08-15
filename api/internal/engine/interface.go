package engine

import (
	"context"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/outcome"
)

type ScoringEngine interface {
	ScoreCustomer(ctx context.Context, customer *domain.Customer, ruleSetID string) (*domain.ScoreRecord, error)
}

// VersionedScoringEngine evaluates an explicitly selected, immutable CDD rule
// definition.  The optional interface keeps legacy adapters source-compatible
// while preventing the native path from merely relabelling a score produced by
// a different configuration.
type VersionedScoringEngine interface {
	ScoreCustomerWithRuleSet(ctx context.Context, customer *domain.Customer, ruleSetID string, definition []byte) (*domain.ScoreRecord, error)
}

// TierThresholdReporter exposes the score bands the engine used to assign a
// tier. The score explanation needs them: reporting the tier without the
// boundaries that decided it leaves a customer one hundredth of a point from
// Medium looking the same as one in the middle of Low. Optional, so adapters
// that cannot answer simply omit the bands.
type TierThresholdReporter interface {
	TierThresholds() map[string][2]float64
}

type MonitoringEngine interface {
	// EvaluateTransactions runs the realtime evaluation pass (mode_filter
	// unset on the wire, which the engine treats as REALTIME;
	// the transaction-monitoring design「評価モード」).
	EvaluateTransactions(
		ctx context.Context,
		customerID string,
		riskTier domain.RiskTier,
		transactions []domain.Transaction,
		scenarioIDs []string,
	) ([]domain.Alert, error)
	// EvaluateTransactionsBatch runs the daily TM batch evaluation pass
	// (mode_filter=BATCH), so evaluation_mode=batch/both scenarios (e.g.
	// aggregation-heavy structuring) are included even though they're
	// excluded from EvaluateTransactions's realtime pass (WS-5 Task6/7).
	EvaluateTransactionsBatch(
		ctx context.Context,
		customerID string,
		riskTier domain.RiskTier,
		transactions []domain.Transaction,
		scenarioIDs []string,
	) ([]domain.Alert, error)
}

// MonitoringRequest is the PH9 canonical evaluation contract. The legacy
// methods above remain source-compatible adapters for existing callers; new
// callers should provide the customer type, explicit mode, evaluation
// timestamp, and bounded event window together so history cannot be guessed
// from an arbitrary row limit.
type MonitoringRequest struct {
	CustomerID    string
	CustomerType  domain.CustomerType
	RiskTier      domain.RiskTier
	Transactions  []domain.Transaction
	ScenarioIDs   []string
	Mode          EvaluationMode
	EvaluatedAt   time.Time
	WindowFrom    *time.Time
	WindowTo      *time.Time
	ConfigDigests map[string]string
}

type EvaluationMode string

const (
	EvaluationModeRealtime EvaluationMode = "realtime"
	EvaluationModeBatch    EvaluationMode = "batch"
	EvaluationModeBoth     EvaluationMode = "both"
)

type MonitoringEngineV2 interface {
	Evaluate(ctx context.Context, req MonitoringRequest) ([]domain.Alert, error)
}

// TMContractInfo describes the interpreted transaction-monitoring vocabulary
// exposed by an engine. It is intentionally additive and optional so older
// adapters can continue to satisfy the evaluation interfaces while the system
// status endpoint still reports the native contract when it is available.
type TMContractInfo struct {
	ContractVersion       string   `json:"contract_version"`
	SupportedDetectors    []string `json:"supported_detectors"`
	CompatibilityWarnings []string `json:"compatibility_warnings,omitempty"`
	DefaultDigest         string   `json:"default_digest,omitempty"`
}

// DefaultTMContractInfo is the contract advertised before an engine is wired.
// This keeps setup-mode /system/status responses useful without claiming that
// evaluation is available.
func DefaultTMContractInfo() TMContractInfo {
	return TMContractInfo{
		ContractVersion: "2.1",
		SupportedDetectors: []string{
			"structuring",
			"rapid_movement",
			"high_frequency_small_amount",
			"dormant_account_reactivation",
			"high_risk_country_transfer",
		},
	}
}

// TMContractReporter is implemented by engines that can report the exact
// interpreted contract and compatibility warnings they loaded at startup.
type TMContractReporter interface {
	TMContract() TMContractInfo
}

// RealtimeHistoryWindowProvider lets an engine declare the largest event-time
// window needed by its realtime scenarios. Servers can then avoid loading a
// customer's entire history for every newly accepted transaction. Engines
// without this capability retain the legacy unbounded-history behavior.
type RealtimeHistoryWindowProvider interface {
	RealtimeHistoryWindow() (window time.Duration, bounded bool)
}

// EvaluateCompat uses the canonical V2 request when supported and otherwise
// adapts it to the legacy realtime/batch methods. Keeping this fallback here
// prevents serving and recovery call sites from drifting apart.
func EvaluateCompat(ctx context.Context, monitoring MonitoringEngine, req MonitoringRequest) ([]domain.Alert, error) {
	if v2, ok := monitoring.(MonitoringEngineV2); ok {
		return v2.Evaluate(ctx, req)
	}
	if req.Mode == EvaluationModeBatch {
		return monitoring.EvaluateTransactionsBatch(ctx, req.CustomerID, req.RiskTier, req.Transactions, req.ScenarioIDs)
	}
	return monitoring.EvaluateTransactions(ctx, req.CustomerID, req.RiskTier, req.Transactions, req.ScenarioIDs)
}

type ScreeningEngine interface {
	ScreenCustomer(
		ctx context.Context,
		customer *domain.Customer,
		listIDs []string,
	) (*domain.ScreenResult, error)
}

type BacktestEngine interface {
	RunBacktest(
		ctx context.Context,
		customers []domain.Customer,
		transactions []domain.Transaction,
		scenarioIDs []string,
		description string,
	) (*domain.BacktestResult, error)
}

// VersionedBacktestEngine can construct an isolated replay engine from an
// auditable rule-definition snapshot. Implementations that do not support
// database-backed rule loading remain valid BacktestEngine implementations for
// deployments that use a fixed configuration root.
type VersionedBacktestEngine interface {
	RunBacktestWithRuleSet(ctx context.Context, customers []domain.Customer, transactions []domain.Transaction, scenarioIDs []string, description, ruleSetID string, definition []byte) (*domain.BacktestResult, error)
}

// DetailedBacktestEngine exposes the alert-shaped detections produced by the
// same replay pass as the aggregate. This prevents outcome analysis from
// rerunning a potentially different rule/configuration snapshot.
type DetailedBacktestEngine interface {
	RunBacktestDetailed(ctx context.Context, customers []domain.Customer, transactions []domain.Transaction, scenarioIDs []string, description string) (*domain.BacktestResult, []outcome.Detection, error)
}

type VersionedDetailedBacktestEngine interface {
	RunBacktestWithRuleSetDetailed(ctx context.Context, customers []domain.Customer, transactions []domain.Transaction, scenarioIDs []string, description, ruleSetID string, definition []byte) (*domain.BacktestResult, []outcome.Detection, error)
}

// ConfigValidationErrorClass separates mistakes that need different fixes
// (ADR-0025, DR-18). "parse error" and "this scenario does not exist" were both
// reported as a bare message, leaving an operator to guess whether the document
// was malformed, structurally wrong, or referring to something absent.
type ConfigValidationErrorClass string

const (
	// ConfigErrorSyntax is a document the parser could not read at all. It
	// always carries a line, because that is the only actionable thing to say.
	ConfigErrorSyntax ConfigValidationErrorClass = "syntax"
	// ConfigErrorSchema is a document that parses but does not have the shape
	// or values the engine requires.
	ConfigErrorSchema ConfigValidationErrorClass = "schema"
	// ConfigErrorCrossReference is a well-formed document naming something that
	// does not exist in this deployment.
	ConfigErrorCrossReference ConfigValidationErrorClass = "cross_reference"
	// ConfigErrorActivation is a refusal to make an otherwise valid version
	// effective, such as the separation-of-duties check in ADR-0014. It has no
	// position: nothing in the document is wrong.
	ConfigErrorActivation ConfigValidationErrorClass = "activation"
)

// ConfigValidationSeverity decides whether a finding blocks. Only "error" does.
// A warning that blocks becomes a warning every operator overrides, and the
// override stops carrying information (ADR-0025).
type ConfigValidationSeverity string

const (
	ConfigSeverityError   ConfigValidationSeverity = "error"
	ConfigSeverityWarning ConfigValidationSeverity = "warning"
)

// ConfigValidationError is additive: Field and Message keep their meaning and
// their place, so a client written against the previous contract still works.
type ConfigValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	// Class, Severity, Line, Column and Path are omitted when unknown rather
	// than defaulted. A zero line would read as "the first line".
	Class    ConfigValidationErrorClass `json:"class,omitempty"`
	Severity ConfigValidationSeverity   `json:"severity,omitempty"`
	Line     int                        `json:"line,omitempty"`
	Column   int                        `json:"column,omitempty"`
	Path     string                     `json:"path,omitempty"`
}

type ConfigValidationResult struct {
	Valid  bool                    `json:"valid"`
	Errors []ConfigValidationError `json:"errors"`
	// Warnings never affect Valid. They are carried separately so a client
	// written against the previous contract cannot mistake one for a rejection.
	Warnings []ConfigValidationError `json:"warnings,omitempty"`
}

type ConfigEngine interface {
	ValidateConfig(ctx context.Context, configType, yamlContent string) (*ConfigValidationResult, error)
}

// HealthChecker reports whether the configured in-process engine is ready.
// This is independent of WS-1's /healthz/ready readiness judgement (which
// also covers whether this API process itself has completed initial setup).
type HealthChecker interface {
	CheckHealth(ctx context.Context) error
}
