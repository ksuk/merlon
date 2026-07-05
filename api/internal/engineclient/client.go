package engineclient

import (
	"context"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/engine"
)

// Client wraps api/internal/engine.Client, routing every call through a
// CircuitBreaker so a stalled Rust engine trips the breaker (overview.md
// §4.4) instead of leaving every caller blocked on a hung gRPC call. It
// implements engine.ScoringEngine / engine.MonitoringEngine /
// engine.ScreeningEngine with identical signatures, so it drops directly
// into server.Deps.Scoring/Monitoring/Screening without changing any
// existing handler code.
type Client struct {
	scoring    engine.ScoringEngine
	monitoring engine.MonitoringEngine
	screening  engine.ScreeningEngine
	cb         *CircuitBreaker
}

var (
	_ engine.ScoringEngine    = (*Client)(nil)
	_ engine.MonitoringEngine = (*Client)(nil)
	_ engine.ScreeningEngine  = (*Client)(nil)
)

// Wrap builds a Client around inner (the real gRPC engine client), sharing a
// single CircuitBreaker across all three engine calls: a stalled engine
// affects scoring, monitoring, and screening identically, so they trip and
// recover together rather than independently.
func Wrap(inner *engine.Client) *Client {
	return &Client{
		scoring:    inner,
		monitoring: inner,
		screening:  inner,
		cb:         NewCircuitBreaker(),
	}
}

// newClientForTest builds a Client around narrower stub interfaces with an
// injectable circuit breaker config, so unit tests can exercise retry/breaker
// behavior without a real gRPC connection or real-time sleeps.
func newClientForTest(inner any, cfg circuitBreakerConfig) *Client {
	c := &Client{cb: newCircuitBreaker(cfg)}
	if s, ok := inner.(engine.ScoringEngine); ok {
		c.scoring = s
	}
	if m, ok := inner.(engine.MonitoringEngine); ok {
		c.monitoring = m
	}
	if s, ok := inner.(engine.ScreeningEngine); ok {
		c.screening = s
	}
	return c
}

func (c *Client) ScoreCustomer(ctx context.Context, customer *domain.Customer, ruleSetID string) (*domain.ScoreRecord, error) {
	var result *domain.ScoreRecord
	err := c.cb.Call(ctx, func(callCtx context.Context) error {
		var err error
		result, err = c.scoring.ScoreCustomer(callCtx, customer, ruleSetID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) EvaluateTransactions(
	ctx context.Context,
	customerID string,
	riskTier domain.RiskTier,
	transactions []domain.Transaction,
	scenarioIDs []string,
) ([]domain.Alert, error) {
	var result []domain.Alert
	err := c.cb.Call(ctx, func(callCtx context.Context) error {
		var err error
		result, err = c.monitoring.EvaluateTransactions(callCtx, customerID, riskTier, transactions, scenarioIDs)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) EvaluateTransactionsBatch(
	ctx context.Context,
	customerID string,
	riskTier domain.RiskTier,
	transactions []domain.Transaction,
	scenarioIDs []string,
) ([]domain.Alert, error) {
	var result []domain.Alert
	err := c.cb.Call(ctx, func(callCtx context.Context) error {
		var err error
		result, err = c.monitoring.EvaluateTransactionsBatch(callCtx, customerID, riskTier, transactions, scenarioIDs)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ScreenCustomer(ctx context.Context, customer *domain.Customer, listIDs []string) (*domain.ScreenResult, error) {
	var result *domain.ScreenResult
	err := c.cb.Call(ctx, func(callCtx context.Context) error {
		var err error
		result, err = c.screening.ScreenCustomer(callCtx, customer, listIDs)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// State reports the underlying circuit breaker's current state, so callers
// (e.g. the PENDING_REVIEW fallback in Task 4) can decide whether to queue
// without invoking the engine at all.
func (c *Client) State() CircuitState {
	return c.cb.State()
}
