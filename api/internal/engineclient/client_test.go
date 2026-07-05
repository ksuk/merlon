package engineclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

// stubMonitoringEngine lets tests control how many times EvaluateTransactions
// fails before succeeding, to exercise Client's retry-through-the-breaker
// behavior without a real gRPC connection.
type stubMonitoringEngine struct {
	failuresBeforeSuccess int
	calls                 int
	alerts                []domain.Alert
}

func (s *stubMonitoringEngine) EvaluateTransactions(
	_ context.Context,
	_ string,
	_ domain.RiskTier,
	_ []domain.Transaction,
	_ []string,
) ([]domain.Alert, error) {
	s.calls++
	if s.calls <= s.failuresBeforeSuccess {
		return nil, errors.New("engine unavailable")
	}
	return s.alerts, nil
}

func TestClientRetriesWithExponentialBackoff(t *testing.T) {
	stub := &stubMonitoringEngine{failuresBeforeSuccess: 1, alerts: []domain.Alert{{ID: "a1"}}}
	c := newClientForTest(stub, circuitBreakerConfig{
		timeout:      20 * time.Millisecond,
		maxRetries:   2,
		baseBackoff:  1 * time.Millisecond,
		openDuration: 100 * time.Millisecond,
		now:          time.Now,
	})

	alerts, err := c.EvaluateTransactions(context.Background(), "cust1", domain.RiskTierLow, nil, nil)
	if err != nil {
		t.Fatalf("EvaluateTransactions: %v", err)
	}
	if len(alerts) != 1 || alerts[0].ID != "a1" {
		t.Errorf("alerts = %+v, want [{ID: a1}]", alerts)
	}
	if stub.calls != 2 {
		t.Errorf("calls = %d, want 2 (1 failure + 1 success)", stub.calls)
	}
	if c.cb.State() != StateClosed {
		t.Errorf("breaker state = %s, want closed after eventual success", c.cb.State())
	}
}

func TestClient_BreakerOpensAfterRepeatedFailure(t *testing.T) {
	stub := &stubMonitoringEngine{failuresBeforeSuccess: 1000}
	c := newClientForTest(stub, circuitBreakerConfig{
		timeout:      5 * time.Millisecond,
		maxRetries:   1,
		baseBackoff:  1 * time.Millisecond,
		openDuration: 100 * time.Millisecond,
		now:          time.Now,
	})

	_, err := c.EvaluateTransactions(context.Background(), "cust1", domain.RiskTierLow, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if c.cb.State() != StateOpen {
		t.Fatalf("breaker state = %s, want open", c.cb.State())
	}

	calls := stub.calls
	_, err = c.EvaluateTransactions(context.Background(), "cust1", domain.RiskTierLow, nil, nil)
	if err == nil {
		t.Fatal("expected error while breaker is open")
	}
	if stub.calls != calls {
		t.Errorf("downstream called %d more times while breaker open, want 0", stub.calls-calls)
	}
}
