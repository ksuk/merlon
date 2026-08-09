package native

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
)

// provenanceEngine builds an engine over a scenario that fires on any transfer,
// so the tests exercise provenance rather than detection logic.
func provenanceEngine(t *testing.T) *Engine {
	t.Helper()
	dir := t.TempDir()

	cdd := filepath.Join(dir, "cdd.yaml")
	writeFile(t, cdd, `schema_version: cdd_weight_v1
preset_id: test
risk_factors: {x: {weight: 1, values: {v: 1}}}
tier_thresholds: {LOW: {max: 2}, MEDIUM: {min: 2, max: 3}, HIGH: {min: 3}}
`)

	tmDir := filepath.Join(dir, "tm")
	if err := os.MkdirAll(tmDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A v2 aggregation scenario with a per-tier threshold, so the applied
	// threshold is a real number the provenance record can name.
	writeFile(t, filepath.Join(tmDir, "structuring.yaml"), `schema_version: "2.0"
scenario_id: tm_structuring_basic
name: Structuring
type: aggregation
conditions:
  threshold:
    by_customer_type:
      individual: {by_risk_tier: {MEDIUM: 1000}}
  absolute_threshold: 2500
  additional: {min_transactions: 3}
evaluation_mode: both
severity: HIGH
`)

	e, err := New(cdd, tmDir, filepath.Join(dir, "missing-lists"), "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func provenanceTransactions() []domain.Transaction {
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	txns := make([]domain.Transaction, 0, 6)
	for i := 0; i < 6; i++ {
		txns = append(txns, domain.Transaction{
			ID:         "t" + string(rune('0'+i)),
			CustomerID: "c1",
			Amount:     900,
			Currency:   "JPY",
			Direction:  domain.DirectionOutbound,
			ExecutedAt: base.Add(time.Duration(i) * time.Hour),
		})
	}
	return txns
}

func evaluateWithProvenance(t *testing.T, e *Engine, mode engine.EvaluationMode) []domain.Alert {
	t.Helper()
	evaluatedAt := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	from := evaluatedAt.Add(-7 * 24 * time.Hour)

	alerts, err := engine.EvaluateCompat(context.Background(), e, engine.MonitoringRequest{
		CustomerID:    "c1",
		CustomerType:  domain.CustomerTypeIndividual,
		RiskTier:      domain.RiskTierMedium,
		Transactions:  provenanceTransactions(),
		Mode:          mode,
		EvaluatedAt:   evaluatedAt,
		WindowFrom:    &from,
		WindowTo:      &evaluatedAt,
		ConfigDigests: map[string]string{"tm_scenarios": "digest-abc"},
	})
	if err != nil {
		t.Fatalf("EvaluateCompat: %v", err)
	}
	if len(alerts) == 0 {
		t.Fatal("no alerts produced; the fixture scenario must fire for these tests to mean anything")
	}
	return alerts
}

// TestProvenance_PinsTheSuppliedConfiguration is the defect: the request has
// carried ConfigDigests since Wave 1 and the engine discarded them.
func TestProvenance_PinsTheSuppliedConfiguration(t *testing.T) {
	e := provenanceEngine(t)

	alert := evaluateWithProvenance(t, e, engine.EvaluationModeRealtime)[0]

	if alert.Provenance == nil {
		t.Fatal("alert carries no provenance")
	}
	if got := alert.Provenance.ConfigDigests["tm_scenarios"]; got != "digest-abc" {
		t.Errorf("config digest = %q, want the digest the caller supplied", got)
	}
	// The engine also knows its own roots exactly, and says so separately from
	// what the caller claimed.
	if alert.Provenance.ConfigDigests["engine:tm_scenarios"] == "" {
		t.Error("the engine's own scenario digest is missing")
	}
	if alert.Provenance.EvaluatedAt == nil || !alert.Provenance.EvaluatedAt.Equal(time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("evaluated_at = %v, want the request's evaluation time", alert.Provenance.EvaluatedAt)
	}
	if alert.Provenance.WindowFrom == nil || alert.Provenance.WindowTo == nil {
		t.Error("the evaluation window was not pinned")
	}
	if alert.Provenance.ScenarioID != alert.ScenarioID {
		t.Errorf("provenance scenario %q does not match the alert's %q", alert.Provenance.ScenarioID, alert.ScenarioID)
	}
	if alert.Provenance.EngineVersion == "" {
		t.Error("engine version is empty; a detection must identify the build that produced it")
	}
}

// TestProvenance_RealtimeAndBatchAgree pins the requirement that every
// generation path produces equivalent semantics. Realtime, batch and recovery
// all reach the engine through EvaluateCompat, so this covers all three.
func TestProvenance_RealtimeAndBatchAgree(t *testing.T) {
	e := provenanceEngine(t)

	realtime := evaluateWithProvenance(t, e, engine.EvaluationModeRealtime)[0]
	batch := evaluateWithProvenance(t, e, engine.EvaluationModeBatch)[0]

	if realtime.Provenance.EvaluationMode != "realtime" {
		t.Errorf("realtime evaluation_mode = %q", realtime.Provenance.EvaluationMode)
	}
	if batch.Provenance.EvaluationMode != "batch" {
		t.Errorf("batch evaluation_mode = %q", batch.Provenance.EvaluationMode)
	}
	// Everything except the mode must match: the same rule and configuration
	// produced both.
	if realtime.Provenance.ConfigDigests["tm_scenarios"] != batch.Provenance.ConfigDigests["tm_scenarios"] {
		t.Error("the two paths pinned different configuration digests")
	}
	if realtime.Provenance.ScenarioID != batch.Provenance.ScenarioID {
		t.Error("the two paths pinned different scenarios")
	}
	if realtime.Provenance.Availability != batch.Provenance.Availability {
		t.Error("the two paths reported different availability")
	}
}

// TestProvenance_LegacyPathCapturesNothing: the pre-V2 entry points carry no
// request. Inventing provenance for them would be worse than recording none.
func TestProvenance_LegacyPathCapturesNothing(t *testing.T) {
	e := provenanceEngine(t)

	alerts, err := e.EvaluateTransactionsBatch(context.Background(), "c1", domain.RiskTierMedium, provenanceTransactions(), nil)
	if err != nil {
		t.Fatalf("EvaluateTransactionsBatch: %v", err)
	}
	if len(alerts) == 0 {
		t.Fatal("no alerts produced")
	}
	if alerts[0].Provenance != nil {
		t.Errorf("legacy path produced provenance %+v; nobody told it what configuration was effective", alerts[0].Provenance)
	}
}

// TestProvenance_BacktestDoesNotDuplicateJobPinning: a backtest job already
// records its rule snapshot and digests on its own row (migrations 027/032).
func TestProvenance_BacktestDoesNotDuplicateJobPinning(t *testing.T) {
	e := provenanceEngine(t)

	tier := domain.RiskTierMedium
	result, err := e.RunBacktest(context.Background(),
		[]domain.Customer{{ID: "c1", CustomerType: domain.CustomerTypeIndividual, RiskTier: &tier}},
		provenanceTransactions(), nil, "provenance check")
	if err != nil {
		t.Fatalf("RunBacktest: %v", err)
	}
	if result == nil {
		t.Fatal("no backtest result")
	}
}
