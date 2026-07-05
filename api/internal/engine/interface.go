package engine

import (
	"context"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

type ScoringEngine interface {
	ScoreCustomer(ctx context.Context, customer *domain.Customer, ruleSetID string) (*domain.ScoreRecord, error)
}

type MonitoringEngine interface {
	// EvaluateTransactions runs the realtime evaluation pass (mode_filter
	// unset on the wire, which the engine treats as REALTIME;
	// transaction-monitoring.md「評価モード」).
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

// HealthChecker reports whether the Rust engine is reachable and serving,
// via the standard grpc.health.v1 protocol (OPS-002). This is independent
// of WS-1's /healthz/ready readiness judgement (which also covers whether
// this API process itself has completed initial setup); HealthChecker only
// answers "can we reach the engine".
type HealthChecker interface {
	CheckHealth(ctx context.Context) error
}
