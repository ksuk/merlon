package native

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
)

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNativeV2ThresholdsUseCustomerTypeAndAbsoluteSafetyValve(t *testing.T) {
	dir := t.TempDir()
	cdd := writeFixture(t, dir, "cdd.yaml", `schema_version: cdd_weight_v1
preset_id: cdd_test
risk_factors: {x: {weight: 1, values: {v: 1}}}
tier_thresholds: {LOW: {max: 2}, MEDIUM: {min: 2, max: 3}, HIGH: {min: 3}}
`)
	tmDir := filepath.Join(dir, "tm")
	if err := os.Mkdir(tmDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, tmDir, "structuring.yaml", `schema_version: "2.0"
scenario_id: tm_structuring_basic
name: test
type: aggregation
conditions:
  threshold:
    by_customer_type:
      individual: {by_risk_tier: {LOW: 1000}}
      corporate_domestic: {by_risk_tier: {LOW: 2000}}
  absolute_threshold: 2500
  additional: {min_transactions: 3, individual_below: 1000}
evaluation_mode: realtime
severity: HIGH
`)
	e, err := New(cdd, tmDir, filepath.Join(dir, "missing-lists"), "")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Unix(1_000_000, 0)
	txns := []domain.Transaction{
		{ID: "t1", CustomerID: "c1", Amount: 900, ExecutedAt: base},
		{ID: "t2", CustomerID: "c1", Amount: 900, ExecutedAt: base.Add(time.Minute)},
		{ID: "t3", CustomerID: "c1", Amount: 900, ExecutedAt: base.Add(2 * time.Minute)},
	}
	alerts, err := e.Evaluate(context.Background(), engine.MonitoringRequest{
		CustomerID: "c1", CustomerType: domain.CustomerTypeCorporateDomestic,
		RiskTier: domain.RiskTierLow, Transactions: txns, Mode: engine.EvaluationModeRealtime,
	})
	if err != nil || len(alerts) != 1 {
		t.Fatalf("alerts=%+v err=%v", alerts, err)
	}
	if alerts[0].Severity != domain.AlertSeverityHigh || !strings.Contains(alerts[0].Description, "absolute_threshold safety valve") {
		t.Fatalf("unexpected alert: %+v", alerts[0])
	}
}

func TestNativeEngineScenarioAndScoreParityContract(t *testing.T) {
	dir := t.TempDir()
	cdd := writeFixture(t, dir, "cdd.yaml", `schema_version: cdd_weight_v1
preset_id: cdd_test
risk_factors:
  customer_type:
    weight: 1.0
    values: {individual: 1, corporate_domestic: 2}
tier_thresholds:
  LOW: {max: 2}
  MEDIUM: {min: 2, max: 3.5}
  HIGH: {min: 3.5}
`)
	tmDir := filepath.Join(dir, "tm")
	if err := os.Mkdir(tmDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, tmDir, "structuring.yaml", `schema_version: "2.0"
scenario_id: tm_structuring_basic
name: test
description: test
conditions:
  threshold:
    by_customer_type:
      individual: {by_risk_tier: {LOW: 100}}
  additional: {min_transaction_count: 3, individual_below: 100}
evaluation_mode: realtime
severity: HIGH
`)
	e, err := New(cdd, tmDir, filepath.Join(dir, "missing-lists"), "")
	if err != nil {
		t.Fatal(err)
	}
	c := &domain.Customer{ID: "c1", CustomerType: domain.CustomerTypeIndividual, Attributes: map[string]any{}}
	score, err := e.ScoreCustomer(context.Background(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	if score.Tier != domain.RiskTierLow || score.RuleSetSHA256 == "" {
		t.Fatalf("unexpected score: %+v", score)
	}
	base := time.Unix(1_000_000, 0)
	txns := []domain.Transaction{{ID: "t1", CustomerID: "c1", Amount: 40, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: base}, {ID: "t2", CustomerID: "c1", Amount: 40, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: base.Add(time.Minute)}, {ID: "t3", CustomerID: "c1", Amount: 40, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: base.Add(2 * time.Minute)}}
	alerts, err := e.EvaluateTransactions(context.Background(), "c1", domain.RiskTierLow, txns, nil)
	if err != nil || len(alerts) != 1 || alerts[0].ScenarioID != "tm_structuring_basic" {
		t.Fatalf("alerts=%+v err=%v", alerts, err)
	}
}

func TestNativeBacktestCandidateUsesIsolatedRuleDefinition(t *testing.T) {
	dir := t.TempDir()
	cdd := writeFixture(t, dir, "cdd.yaml", `schema_version: cdd_weight_v1
preset_id: cdd_test
risk_factors: {x: {weight: 1, values: {v: 1}}}
tier_thresholds: {LOW: {max: 2}, MEDIUM: {min: 2, max: 3}, HIGH: {min: 3}}
`)
	tmDir := filepath.Join(dir, "tm")
	if err := os.Mkdir(tmDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, tmDir, "structuring.yaml", `scenario_id: tm_structuring_basic
conditions: {threshold: {by_customer_type: {individual: {by_risk_tier: {LOW: 100}}}}, additional: {min_transaction_count: 3, individual_below: 1000}}
evaluation_mode: both
`)
	e, err := New(cdd, tmDir, filepath.Join(dir, "missing-lists"), "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_000_000, 0)
	low := domain.RiskTierLow
	customers := []domain.Customer{{ID: "c1", CustomerType: domain.CustomerTypeIndividual, RiskTier: &low}}
	txns := []domain.Transaction{{ID: "t1", CustomerID: "c1", Amount: 20, ExecutedAt: now}, {ID: "t2", CustomerID: "c1", Amount: 20, ExecutedAt: now.Add(time.Minute)}, {ID: "t3", CustomerID: "c1", Amount: 20, ExecutedAt: now.Add(2 * time.Minute)}}
	base, err := e.RunBacktest(context.Background(), customers, txns, nil, "base")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := e.RunBacktestWithRuleSet(context.Background(), customers, txns, nil, "candidate", "rule-v2", []byte(`scenario_id: tm_structuring_basic
conditions: {threshold: {by_customer_type: {individual: {by_risk_tier: {LOW: 50}}}}, additional: {min_transaction_count: 3, individual_below: 1000}}
evaluation_mode: both
`))
	if err != nil {
		t.Fatal(err)
	}
	if base.TotalAlerts != 0 || candidate.TotalAlerts == 0 {
		t.Fatalf("base=%+v candidate=%+v", base, candidate)
	}
	// The live engine remains on the baseline definition after candidate replay.
	again, err := e.RunBacktest(context.Background(), customers, txns, nil, "base-again")
	if err != nil || again.TotalAlerts != 0 {
		t.Fatalf("baseline mutated: result=%+v err=%v", again, err)
	}
}

func TestNativeScreeningUsesDeterministicLevenshtein(t *testing.T) {
	dir := t.TempDir()
	cdd := writeFixture(t, dir, "cdd.yaml", `schema_version: cdd_weight_v1
preset_id: cdd_test
risk_factors: {x: {weight: 1, values: {v: 1}}}
tier_thresholds: {LOW: {max: 2}, MEDIUM: {min: 2, max: 3}, HIGH: {min: 3}}
`)
	tmDir := filepath.Join(dir, "tm")
	if err := os.Mkdir(tmDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, tmDir, "x.yaml", `scenario_id: tm_high_risk_country_transfer
conditions: {additional: {threshold_amount: 1, high_risk_countries: [KP]}}
`)
	lists := filepath.Join(dir, "lists")
	if err := os.Mkdir(lists, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, lists, "list.yaml", `list_id: l1
list_type: sanctions
source: test
entries: [{entry_id: e1, names: [Kim Jong Un]}]
`)
	e, err := New(cdd, tmDir, lists, "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.ScreenCustomer(context.Background(), &domain.Customer{ID: "c1", ExternalID: "kim jong un", Attributes: map[string]any{}}, nil)
	if err != nil || !r.Hit || len(r.Matches) != 1 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestNativeValidateConfigMatchesTypedValidation(t *testing.T) {
	e := &Engine{}
	valid, err := e.ValidateConfig(context.Background(), "cdd_weights", `schema_version: cdd_weight_v1
preset_id: test
risk_factors: {x: {weight: 1, values: {v: 1}}}
tier_thresholds: {LOW: {max: 2}, MEDIUM: {min: 2, max: 3}, HIGH: {min: 3}}
`)
	if err != nil || !valid.Valid {
		t.Fatalf("valid=%+v err=%v", valid, err)
	}
	unknown, err := e.ValidateConfig(context.Background(), "unknown", "x: 1")
	if err != nil || unknown.Valid || len(unknown.Errors) == 0 || unknown.Errors[0].Field != "config_type" {
		t.Fatalf("unknown=%+v err=%v", unknown, err)
	}
}
