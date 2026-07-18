package native

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
)

func TestRiskTierKeysAreNormalized(t *testing.T) {
	dir := t.TempDir()
	cdd := writeFixture(t, dir, "cdd.yaml", `schema_version: cdd_weight_v1
preset_id: cdd_test
risk_factors: {customer_type: {weight: 1, values: {individual: 1}}}
tier_thresholds: {low: {max: 2}, medium: {min: 2, max: 3}, high: {min: 3}}
`)
	tmDir := filepath.Join(dir, "tm")
	if err := os.Mkdir(tmDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, tmDir, "v1.yaml", `schema_version: "1.0"
scenario_id: tm_structuring_basic
parameters: {window_hours: 24, threshold_amount: 9999, min_transactions: 3, individual_below: 1000}
risk_tier_adjustments: {low: {threshold_amount: 100}}
evaluation_mode: realtime
`)
	writeFixture(t, tmDir, "v2.yaml", `schema_version: "2.0"
scenario_id: test_structuring_v2
conditions:
  threshold: {by_customer_type: {individual: {by_risk_tier: {low: 100}}}}
  additional: {min_transactions: 3, individual_below: 1000}
evaluation_mode: realtime
`)

	e, err := New(cdd, tmDir, filepath.Join(dir, "missing-lists"), "")
	if err != nil {
		t.Fatal(err)
	}
	score, err := e.ScoreCustomer(context.Background(), &domain.Customer{ID: "c1", CustomerType: domain.CustomerTypeIndividual}, "")
	if err != nil {
		t.Fatal(err)
	}
	if score.Tier != domain.RiskTierLow {
		t.Fatalf("tier = %q, want LOW", score.Tier)
	}
	base := time.Unix(1_000_000, 0)
	txns := []domain.Transaction{
		{ID: "t1", CustomerID: "c1", Amount: 40, ExecutedAt: base},
		{ID: "t2", CustomerID: "c1", Amount: 40, ExecutedAt: base.Add(time.Minute)},
		{ID: "t3", CustomerID: "c1", Amount: 40, ExecutedAt: base.Add(2 * time.Minute)},
	}
	alerts, err := e.Evaluate(context.Background(), engine.MonitoringRequest{
		CustomerID: "c1", CustomerType: domain.CustomerTypeIndividual, RiskTier: domain.RiskTierLow,
		Transactions: txns, Mode: engine.EvaluationModeRealtime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 2 {
		t.Fatalf("alerts = %+v, want both lowercase-key v1 and v2 scenarios", alerts)
	}
}

func TestRiskTierKeysRejectUnknownAndCaseCollisions(t *testing.T) {
	e := &Engine{}
	tests := []struct {
		name    string
		typ     string
		content string
	}{
		{
			name: "CDD unknown", typ: "cdd_weights",
			content: `risk_factors: {x: {weight: 1, values: {v: 1}}}
tier_thresholds: {LOW: {max: 2}, CRITICAL: {min: 2}}
`,
		},
		{
			name: "CDD collision", typ: "cdd_weights",
			content: `risk_factors: {x: {weight: 1, values: {v: 1}}}
tier_thresholds: {LOW: {max: 2}, low: {max: 3}}
`,
		},
		{
			name: "TM v1 unknown", typ: "tm_scenarios",
			content: `scenario_id: tm_structuring_basic
parameters: {threshold_amount: 1}
risk_tier_adjustments: {critical: {threshold_amount: 2}}
`,
		},
		{
			name: "TM v1 collision", typ: "tm_scenarios",
			content: `scenario_id: tm_structuring_basic
parameters: {threshold_amount: 1}
risk_tier_adjustments: {LOW: {threshold_amount: 2}, low: {threshold_amount: 3}}
`,
		},
		{
			name: "TM v2 unknown", typ: "tm_scenarios",
			content: `schema_version: "2.0"
scenario_id: tm_structuring_basic
conditions: {threshold: {by_customer_type: {individual: {by_risk_tier: {critical: 1}}}}}
`,
		},
		{
			name: "TM v2 collision", typ: "tm_scenarios",
			content: `schema_version: "2.0"
scenario_id: tm_structuring_basic
conditions: {threshold: {by_customer_type: {individual: {by_risk_tier: {LOW: 1, low: 2}}}}}
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ValidateConfig(context.Background(), tt.typ, tt.content)
			if err != nil {
				t.Fatal(err)
			}
			if result.Valid || len(result.Errors) == 0 {
				t.Fatalf("result = %+v, want validation error", result)
			}
		})
	}
}

func TestNewRejectsInvalidRiskTierKeys(t *testing.T) {
	tests := []struct {
		name string
		cdd  string
		tm   string
	}{
		{
			name: "CDD collision",
			cdd: `risk_factors: {x: {weight: 1, values: {v: 1}}}
tier_thresholds: {LOW: {max: 2}, low: {max: 3}}
`,
			tm: "scenario_id: tm_structuring_basic\nparameters: {threshold_amount: 1}\n",
		},
		{
			name: "TM unknown",
			cdd: `risk_factors: {x: {weight: 1, values: {v: 1}}}
tier_thresholds: {LOW: {max: 2}}
`,
			tm: `scenario_id: tm_structuring_basic
parameters: {threshold_amount: 1}
risk_tier_adjustments: {unexpected: {threshold_amount: 2}}
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cdd := writeFixture(t, dir, "cdd.yaml", tt.cdd)
			tm := writeFixture(t, dir, "tm.yaml", tt.tm)
			if _, err := New(cdd, tm, filepath.Join(dir, "missing-lists"), ""); err == nil {
				t.Fatal("New returned nil error")
			}
		})
	}
}

func TestCountryRiskScoresMustBeIntegersFromOneThroughFive(t *testing.T) {
	tests := []struct {
		name  string
		table countryRisk
		valid bool
	}{
		{name: "minimum", table: countryRisk{DefaultScore: 1, Countries: countryRows("JP", 1)}, valid: true},
		{name: "maximum", table: countryRisk{DefaultScore: 5, Countries: countryRows("KP", 5)}, valid: true},
		{name: "fractional default", table: countryRisk{DefaultScore: 1.5}, valid: false},
		{name: "fractional country", table: countryRisk{DefaultScore: 3, Countries: countryRows("JP", 2.5)}, valid: false},
		{name: "country below range", table: countryRisk{DefaultScore: 3, Countries: countryRows("JP", 0)}, valid: false},
		{name: "above range", table: countryRisk{DefaultScore: 6}, valid: false},
		{name: "NaN", table: countryRisk{DefaultScore: math.NaN()}, valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCountryRisk(tt.table)
			if tt.valid && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func countryRows(code string, score float64) map[string]struct {
	Score float64 `yaml:"score"`
} {
	return map[string]struct {
		Score float64 `yaml:"score"`
	}{code: {Score: score}}
}

func TestNativeEntryPointsHonorCanceledContext(t *testing.T) {
	e := &Engine{
		cdd: cddConfig{
			PresetID:       "test",
			RiskFactors:    map[string]riskFactor{"customer_type": {Weight: 1, Values: map[string]float64{"individual": 1}}},
			TierThresholds: map[string]threshold{"LOW": {Max: 2}},
		},
		scenarios: []scenario{{ID: "tm_high_risk_country_transfer", Parameters: map[string]any{}}},
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	customer := &domain.Customer{ID: "c1", CustomerType: domain.CustomerTypeIndividual}
	txn := domain.Transaction{ID: "t1", CustomerID: "c1", ExecutedAt: time.Now()}

	tests := []struct {
		name string
		call func() error
	}{
		{name: "score", call: func() error { _, err := e.ScoreCustomer(canceled, customer, ""); return err }},
		{name: "evaluate legacy", call: func() error {
			_, err := e.EvaluateTransactions(canceled, "c1", domain.RiskTierLow, []domain.Transaction{txn}, nil)
			return err
		}},
		{name: "evaluate v2", call: func() error {
			_, err := e.Evaluate(canceled, engine.MonitoringRequest{CustomerID: "c1", Transactions: []domain.Transaction{txn}})
			return err
		}},
		{name: "evaluate batch", call: func() error {
			_, err := e.EvaluateTransactionsBatch(canceled, "c1", domain.RiskTierLow, []domain.Transaction{txn}, nil)
			return err
		}},
		{name: "screen", call: func() error { _, err := e.ScreenCustomer(canceled, customer, nil); return err }},
		{name: "backtest", call: func() error {
			_, err := e.RunBacktest(canceled, []domain.Customer{*customer}, []domain.Transaction{txn}, nil, "")
			return err
		}},
		{name: "candidate backtest", call: func() error {
			_, err := e.RunBacktestWithRuleSet(canceled, nil, nil, nil, "", "rule", []byte("scenario_id: tm_structuring_basic"))
			return err
		}},
		{name: "validate", call: func() error { _, err := e.ValidateConfig(canceled, "unknown", ""); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
		})
	}
}

func TestRealtimeHistoryWindowUsesLongestRealtimeScenario(t *testing.T) {
	e := &Engine{scenarios: []scenario{
		{ID: "tm_structuring_basic", Mode: "both", Parameters: map[string]any{"window_hours": map[string]any{"LOW": 12, "MEDIUM": 36, "HIGH": 18}}},
		{ID: "tm_rapid_movement", Mode: "realtime", Parameters: map[string]any{"window_hours": 48}},
		{ID: "tm_dormant_account_reactivation", Mode: "batch", Parameters: map[string]any{"dormant_days": 365}},
	}}
	got, bounded := e.RealtimeHistoryWindow()
	if !bounded || got != 48*time.Hour {
		t.Fatalf("RealtimeHistoryWindow = %s, want 48h", got)
	}
}

func TestRealtimeHistoryWindowIsUnboundedForRealtimeDormancy(t *testing.T) {
	e := &Engine{scenarios: []scenario{{ID: "tm_dormant_account_reactivation", Mode: "realtime", Parameters: map[string]any{"dormant_days": 180}}}}
	if _, bounded := e.RealtimeHistoryWindow(); bounded {
		t.Fatal("realtime dormancy requires the preceding transaction and must not advertise a finite history window")
	}
}

func TestTierValidationErrorsIdentifyTheInvalidKey(t *testing.T) {
	_, err := parseScenario([]byte(`scenario_id: tm_structuring_basic
parameters: {threshold_amount: 1}
risk_tier_adjustments: {unexpected: {threshold_amount: 2}}
`))
	if err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("error = %v, want invalid key in message", err)
	}
}
