package engine

import (
	"context"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

type MockScoringEngine struct {
	Score float64
	Tier  domain.RiskTier
	Err   error
}

func (m *MockScoringEngine) ScoreCustomer(_ context.Context, customer *domain.Customer, ruleSetID string) (*domain.ScoreRecord, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return &domain.ScoreRecord{
		CustomerID:     customer.ID,
		Score:          m.Score,
		Tier:           m.Tier,
		Factors:        []domain.Factor{{Name: "mock_factor", Score: m.Score, Description: "mock"}},
		RuleSetID:      ruleSetID,
		RuleSetVersion: 1,
		ScoredAt:       time.Now(),
	}, nil
}

type MockMonitoringEngine struct {
	Alerts []domain.Alert
	Err    error
}

func (m *MockMonitoringEngine) EvaluateTransactions(
	_ context.Context,
	_ string,
	_ domain.RiskTier,
	_ []domain.Transaction,
	_ []string,
) ([]domain.Alert, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Alerts, nil
}
