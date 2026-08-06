package engine

import (
	"context"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
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

type ConfigValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ConfigValidationResult struct {
	Valid  bool                    `json:"valid"`
	Errors []ConfigValidationError `json:"errors"`
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
