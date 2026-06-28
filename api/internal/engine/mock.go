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

type MockScreeningEngine struct {
	Result *domain.ScreenResult
	Err    error
}

func (m *MockScreeningEngine) ScreenCustomer(
	_ context.Context,
	customer *domain.Customer,
	_ []string,
) (*domain.ScreenResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if m.Result != nil {
		return m.Result, nil
	}
	return &domain.ScreenResult{
		CustomerID:   customer.ID,
		Hit:          false,
		Matches:      nil,
		ListsChecked: 1,
		ScreenedAt:   time.Now(),
	}, nil
}

type MockBacktestEngine struct {
	Result *domain.BacktestResult
	Err    error
}

func (m *MockBacktestEngine) RunBacktest(
	_ context.Context,
	_ []domain.Customer,
	_ []domain.Transaction,
	_ []string,
	_ string,
) (*domain.BacktestResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if m.Result != nil {
		return m.Result, nil
	}
	return &domain.BacktestResult{
		BacktestID:        "bt_mock",
		TotalTransactions: 0,
		TotalCustomers:    0,
		TotalAlerts:       0,
		ScenarioResults:   nil,
		ExecutionTimeMs:   0.1,
	}, nil
}

type MockConfigEngine struct {
	Result *ConfigValidationResult
	Err    error
}

func (m *MockConfigEngine) ValidateConfig(_ context.Context, _, _ string) (*ConfigValidationResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if m.Result != nil {
		return m.Result, nil
	}
	return &ConfigValidationResult{Valid: true}, nil
}
