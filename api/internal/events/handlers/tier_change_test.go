package handlers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/engine"
	"github.com/merlon-aml/merlon/api/internal/events"
	"github.com/merlon-aml/merlon/api/internal/metrics"
	"github.com/merlon-aml/merlon/api/internal/store"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func riskTier(t domain.RiskTier) *domain.RiskTier { return &t }

func newTierChangeEvent(t *testing.T, tc TierChangeEvent, hopCount int) events.Event {
	t.Helper()
	payload, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return events.Event{
		ID:            "evt1",
		Topic:         "cdd.tier_changed",
		Payload:       payload,
		ChainID:       "chain1",
		ChainHopCount: hopCount,
		CreatedAt:     time.Now(),
	}
}

func seedTransaction(t *testing.T, transactions domain.TransactionRepository, customerID, id string, createdAt time.Time) {
	t.Helper()
	tx := &domain.Transaction{
		ID:         id,
		CustomerID: customerID,
		ExternalID: "EXT_" + id,
		Amount:     500000,
		Currency:   "JPY",
		Direction:  domain.DirectionInbound,
		CreatedAt:  createdAt,
		ExecutedAt: createdAt,
	}
	if err := transactions.Create(context.Background(), tx); err != nil {
		t.Fatalf("seed transaction %s: %v", id, err)
	}
}

// TestTierChangeHandler_UpgradeTriggersRetroactiveReevaluation verifies
// transaction-monitoring.md's "in-flight tier consistency" rule: a
// MEDIUM/LOW -> HIGH upgrade re-evaluates transactions evaluated within the
// last 24 hours under the new tier's thresholds.
func TestTierChangeHandler_UpgradeTriggersRetroactiveReevaluation(t *testing.T) {
	transactions := store.NewMemoryTransactionRepo()
	alerts := store.NewMemoryAlertRepo()

	now := time.Now()
	seedTransaction(t, transactions, "cust1", "tx-recent", now.Add(-2*time.Hour))
	seedTransaction(t, transactions, "cust1", "tx-old", now.Add(-48*time.Hour))

	var evaluatedTxnIDs []string
	monitoring := &engine.MockMonitoringEngine{}
	monitoring.EvaluateFunc = func(_ context.Context, _ string, tier domain.RiskTier, txns []domain.Transaction, _ []string) ([]domain.Alert, error) {
		if tier != domain.RiskTierHigh {
			t.Errorf("tier = %s, want %s", tier, domain.RiskTierHigh)
		}
		for _, tx := range txns {
			evaluatedTxnIDs = append(evaluatedTxnIDs, tx.ID)
		}
		return nil, nil
	}

	handler := NewTierChangeHandler(transactions, monitoring, alerts)
	handler(newTierChangeEvent(t, TierChangeEvent{
		CustomerID: "cust1",
		OldTier:    riskTier(domain.RiskTierMedium),
		NewTier:    domain.RiskTierHigh,
		ChainID:    "chain1",
		ScoredAt:   now,
	}, 0))

	if len(evaluatedTxnIDs) != 1 || evaluatedTxnIDs[0] != "tx-recent" {
		t.Errorf("evaluated = %v, want only [tx-recent] (24h window)", evaluatedTxnIDs)
	}
}

// TestTierChangeHandler_DowngradeDoesNotRetroactivelyReevaluate verifies
// that a tier downgrade never triggers retroactive re-evaluation
// (Fail-Alert principle: prefer false positives over missed detections, so
// existing alerts are never retroactively invalidated).
func TestTierChangeHandler_DowngradeDoesNotRetroactivelyReevaluate(t *testing.T) {
	transactions := store.NewMemoryTransactionRepo()
	alerts := store.NewMemoryAlertRepo()
	seedTransaction(t, transactions, "cust1", "tx-recent", time.Now().Add(-1*time.Hour))

	called := false
	monitoring := &engine.MockMonitoringEngine{}
	monitoring.EvaluateFunc = func(context.Context, string, domain.RiskTier, []domain.Transaction, []string) ([]domain.Alert, error) {
		called = true
		return nil, nil
	}

	handler := NewTierChangeHandler(transactions, monitoring, alerts)
	handler(newTierChangeEvent(t, TierChangeEvent{
		CustomerID: "cust1",
		OldTier:    riskTier(domain.RiskTierHigh),
		NewTier:    domain.RiskTierMedium,
		ChainID:    "chain1",
		ScoredAt:   time.Now(),
	}, 0))

	if called {
		t.Error("monitoring.EvaluateTransactions should not be called on downgrade")
	}
}

// TestTierChangeHandler_NewAlertsGeneratedOnReevaluation verifies that
// alerts returned by re-evaluation are persisted.
func TestTierChangeHandler_NewAlertsGeneratedOnReevaluation(t *testing.T) {
	transactions := store.NewMemoryTransactionRepo()
	alerts := store.NewMemoryAlertRepo()
	seedTransaction(t, transactions, "cust1", "tx-recent", time.Now().Add(-1*time.Hour))

	monitoring := &engine.MockMonitoringEngine{}
	monitoring.EvaluateFunc = func(context.Context, string, domain.RiskTier, []domain.Transaction, []string) ([]domain.Alert, error) {
		return []domain.Alert{{CustomerID: "cust1", ScenarioID: "S1", Severity: domain.AlertSeverityHigh, Description: "retroactive hit"}}, nil
	}

	handler := NewTierChangeHandler(transactions, monitoring, alerts)
	handler(newTierChangeEvent(t, TierChangeEvent{
		CustomerID: "cust1",
		OldTier:    riskTier(domain.RiskTierLow),
		NewTier:    domain.RiskTierHigh,
		ChainID:    "chain1",
		ScoredAt:   time.Now(),
	}, 0))

	created, err := alerts.ListByCustomer(context.Background(), "cust1", 10, 0)
	if err != nil {
		t.Fatalf("ListByCustomer: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("alerts count = %d, want 1", len(created))
	}
	if created[0].Description != "retroactive hit" {
		t.Errorf("Description = %q, want %q", created[0].Description, "retroactive hit")
	}
}

// TestTierChangeHandler_EventChainTruncatedAfterThreeHops verifies
// cdd-scoring.md safety valve 4: once an event chain has hopped past the
// default limit (3), the handler stops re-evaluating and instead increments
// merlon_cdd_event_chain_truncated_total.
func TestTierChangeHandler_EventChainTruncatedAfterThreeHops(t *testing.T) {
	transactions := store.NewMemoryTransactionRepo()
	alerts := store.NewMemoryAlertRepo()
	seedTransaction(t, transactions, "cust1", "tx-recent", time.Now().Add(-1*time.Hour))

	called := false
	monitoring := &engine.MockMonitoringEngine{}
	monitoring.EvaluateFunc = func(context.Context, string, domain.RiskTier, []domain.Transaction, []string) ([]domain.Alert, error) {
		called = true
		return nil, nil
	}

	before := testutil.ToFloat64(metrics.CDDEventChainTruncatedTotal)

	handler := NewTierChangeHandler(transactions, monitoring, alerts)
	handler(newTierChangeEvent(t, TierChangeEvent{
		CustomerID: "cust1",
		OldTier:    riskTier(domain.RiskTierMedium),
		NewTier:    domain.RiskTierHigh,
		ChainID:    "chain1",
		ScoredAt:   time.Now(),
	}, maxChainHops))

	if called {
		t.Error("monitoring.EvaluateTransactions should not be called once the chain hop limit is exceeded")
	}
	after := testutil.ToFloat64(metrics.CDDEventChainTruncatedTotal)
	if after != before+1 {
		t.Errorf("CDDEventChainTruncatedTotal = %v, want %v", after, before+1)
	}
}
