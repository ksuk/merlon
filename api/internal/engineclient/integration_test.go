package engineclient

import (
	"context"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

// toggleMonitoringEngine lets a test flip between failing and succeeding
// responses to drive the breaker through every state transition.
type toggleMonitoringEngine struct {
	fail  bool
	calls int
}

func (s *toggleMonitoringEngine) EvaluateTransactions(
	_ context.Context,
	_ string,
	_ domain.RiskTier,
	_ []domain.Transaction,
	_ []string,
) ([]domain.Alert, error) {
	s.calls++
	if s.fail {
		return nil, errBoom
	}
	return nil, nil
}

func (s *toggleMonitoringEngine) EvaluateTransactionsBatch(
	ctx context.Context,
	customerID string,
	riskTier domain.RiskTier,
	transactions []domain.Transaction,
	scenarioIDs []string,
) ([]domain.Alert, error) {
	return s.EvaluateTransactions(ctx, customerID, riskTier, transactions, scenarioIDs)
}

// TestCircuitBreakerFullLifecycle_ClosedOpenHalfOpenClosed is the explicit
// acceptance-criteria test for "circuit breaker state transition
// (Closed→Open→Half-Open→Closed)", exercised end-to-end through
// engineclient.Client rather than the bare CircuitBreaker unit tests in
// circuitbreaker_test.go.
func TestCircuitBreakerFullLifecycle_ClosedOpenHalfOpenClosed(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	stub := &toggleMonitoringEngine{fail: true}
	c := newClientForTest(stub, circuitBreakerConfig{
		timeout:      5 * time.Millisecond,
		maxRetries:   1,
		baseBackoff:  1 * time.Millisecond,
		openDuration: 100 * time.Millisecond,
		now:          clock.Now,
	})

	if c.State() != StateClosed {
		t.Fatalf("initial state = %s, want %s", c.State(), StateClosed)
	}

	// Closed -> Open: the call exhausts its retries against a failing engine.
	if _, err := c.EvaluateTransactions(context.Background(), "cust1", domain.RiskTierLow, nil, nil); err == nil {
		t.Fatal("expected error from failing engine")
	}
	if c.State() != StateOpen {
		t.Fatalf("state after repeated failure = %s, want %s", c.State(), StateOpen)
	}

	// Open: further calls are rejected without reaching the engine.
	callsWhileOpen := stub.calls
	if _, err := c.EvaluateTransactions(context.Background(), "cust1", domain.RiskTierLow, nil, nil); err == nil {
		t.Fatal("expected ErrCircuitOpen")
	}
	if stub.calls != callsWhileOpen {
		t.Errorf("engine called while open: calls = %d, want %d (skipped)", stub.calls, callsWhileOpen)
	}

	// Open -> Half-Open once the 30s (here: openDuration) window elapses.
	clock.Advance(100 * time.Millisecond)
	if c.State() != StateHalfOpen {
		t.Fatalf("state after openDuration elapsed = %s, want %s", c.State(), StateHalfOpen)
	}

	// Half-Open -> Closed: the single probe request succeeds.
	stub.fail = false
	if _, err := c.EvaluateTransactions(context.Background(), "cust1", domain.RiskTierLow, nil, nil); err != nil {
		t.Fatalf("half-open probe: %v", err)
	}
	if c.State() != StateClosed {
		t.Fatalf("state after successful probe = %s, want %s", c.State(), StateClosed)
	}
}
