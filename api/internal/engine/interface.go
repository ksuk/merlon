package engine

import (
	"context"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

type ScoringEngine interface {
	ScoreCustomer(ctx context.Context, customer *domain.Customer, ruleSetID string) (*domain.ScoreRecord, error)
}

type MonitoringEngine interface {
	EvaluateTransactions(
		ctx context.Context,
		customerID string,
		riskTier domain.RiskTier,
		transactions []domain.Transaction,
		scenarioIDs []string,
	) ([]domain.Alert, error)
}
