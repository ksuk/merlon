package native

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestParseScenarioV21RequiresDeclaredDetector(t *testing.T) {
	_, err := parseScenario([]byte(`schema_version: "2.1"
scenario_id: tm_structuring_basic
name: Structuring
type: aggregation
conditions: {}
severity: HIGH
`))
	if err == nil || !strings.Contains(err.Error(), "detector") {
		t.Fatalf("parseScenario error = %v, want missing detector error", err)
	}
}

func TestParseScenarioV21RejectsUnknownCondition(t *testing.T) {
	_, err := parseScenario([]byte(`schema_version: "2.1"
scenario_id: tm_structuring_basic
name: Structuring
type: aggregation
detector: structuring
conditions:
  unsupported: true
severity: HIGH
`))
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("parseScenario error = %v, want unsupported condition error", err)
	}
}

func TestParseScenarioRejectsUnknownNestedAggregationKey(t *testing.T) {
	_, err := parseScenario([]byte(`schema_version: "2.1"
scenario_id: tm_structuring_basic
name: Structuring
type: aggregation
detector: structuring
conditions:
  aggregation:
    field: amount
    function: sum
    period: 24h
    group_by: customer_id
    typo: ignored
severity: HIGH
`))
	if err == nil || !strings.Contains(err.Error(), "typo") {
		t.Fatalf("parseScenario error = %v, want unknown nested key error", err)
	}
}

func TestParseScenarioLegacyAliasesAreMarkedForCompatibility(t *testing.T) {
	s, err := parseScenario([]byte(`schema_version: "2.0"
scenario_id: tm_structuring_basic
name: Structuring
type: aggregation
conditions:
  additional:
    window_hours: 24
severity: HIGH
`))
	if err != nil {
		t.Fatal(err)
	}
	if !s.LegacyRouting || !s.LegacyWindowAlias {
		t.Fatalf("scenario = %+v, want legacy detector and window alias markers", s)
	}
}

func TestEvaluateScenarioUsesDeclaredDetectorInsteadOfScenarioID(t *testing.T) {
	s := scenario{
		ID:       "institution_specific_pattern",
		Detector: "structuring",
		Mode:     "both",
		Parameters: map[string]any{
			"min_transaction_count": int64(3),
			"individual_below":      float64(1000),
			"absolute_threshold":    float64(10000),
		},
		Thresholds: map[string]map[string]float64{
			"individual": {"LOW": 100},
		},
	}
	base := time.Unix(1_000_000, 0).UTC()
	txns := []domain.Transaction{
		{ID: "t1", Amount: 40, Direction: domain.DirectionInbound, ExecutedAt: base},
		{ID: "t2", Amount: 40, Direction: domain.DirectionInbound, ExecutedAt: base.Add(time.Minute)},
		{ID: "t3", Amount: 40, Direction: domain.DirectionInbound, ExecutedAt: base.Add(2 * time.Minute)},
	}
	alerts, err := evaluateScenario(context.Background(), s, "individual", "LOW", txns)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("alerts = %+v, want one alert from declared detector", alerts)
	}
}

func TestEvaluateScenarioRejectsUnboundDetector(t *testing.T) {
	_, err := evaluateScenario(context.Background(), scenario{ID: "not_a_detector"}, "individual", "LOW", nil)
	if err == nil || !strings.Contains(err.Error(), "detector") {
		t.Fatalf("error = %v, want explicit detector error", err)
	}
}

func TestParseScenarioReadsTransactionTypesAndAggregationPeriod(t *testing.T) {
	s, err := parseScenario([]byte(`schema_version: "2.1"
scenario_id: institution_rule
name: Institution rule
type: aggregation
detector: structuring
conditions:
  transaction_type: [deposit, transfer_in]
  aggregation:
    field: amount
    function: sum
    period: 48h
    group_by: customer_id
severity: HIGH
`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Detector != detectorStructuring || len(s.TransactionTypes) != 2 || s.Aggregation.Period != 48*time.Hour {
		t.Fatalf("parsed scenario = %+v, want explicit detector/types/period", s)
	}
	if got := s.windowSeconds("LOW", time.Hour); got != int64(48*time.Hour/time.Second) {
		t.Fatalf("window seconds = %d, want 172800", got)
	}
}

func TestParseScenarioValidatesDetectorAggregationFunction(t *testing.T) {
	_, err := parseScenario([]byte(`schema_version: "2.1"
scenario_id: institution_hfsa
name: High frequency
type: aggregation
detector: high_frequency_small_amount
conditions:
  aggregation:
    field: amount
    function: sum
    period: 1h
    group_by: customer_id
severity: HIGH
`))
	if err == nil || !strings.Contains(err.Error(), "requires aggregation function count") {
		t.Fatalf("parseScenario error = %v, want detector-specific function error", err)
	}
}

func TestScenarioTransactionTypeFilterUsesLegacyDirectionFallback(t *testing.T) {
	s := scenario{Detector: detectorStructuring, TransactionTypes: []string{"transfer_in"}}
	txns := []domain.Transaction{
		{ID: "in", Direction: domain.DirectionInbound},
		{ID: "out", Direction: domain.DirectionOutbound},
		{ID: "explicit", Direction: domain.DirectionInbound, TransactionType: "cash"},
	}
	got := s.filterTransactions(txns)
	if len(got) != 1 || got[0].ID != "in" {
		t.Fatalf("filtered transactions = %+v, want only inbound legacy fallback", got)
	}
}

func TestAbsoluteThresholdSafetyValveAppliesToEveryDetector(t *testing.T) {
	base := time.Unix(1_000_000, 0).UTC()
	tests := []struct {
		name     string
		detector string
		params   map[string]any
		txns     []domain.Transaction
	}{
		{
			name:     "rapid movement",
			detector: detectorRapid,
			params:   map[string]any{"inbound_threshold": float64(1000), "outbound_threshold": float64(1000), "outbound_ratio_min": float64(.8), "absolute_threshold": float64(100)},
			txns:     []domain.Transaction{{ID: "in", Amount: 100, Direction: domain.DirectionInbound, ExecutedAt: base}, {ID: "out", Amount: 100, Direction: domain.DirectionOutbound, ExecutedAt: base.Add(time.Minute)}},
		},
		{
			name:     "high frequency",
			detector: detectorHFSA,
			params:   map[string]any{"count_threshold": float64(100), "max_amount_per_txn": float64(1000), "absolute_threshold": float64(3)},
			txns:     []domain.Transaction{{ID: "a", Amount: 10, ExecutedAt: base}, {ID: "b", Amount: 10, ExecutedAt: base.Add(time.Minute)}, {ID: "c", Amount: 10, ExecutedAt: base.Add(2 * time.Minute)}},
		},
		{
			name:     "dormant",
			detector: detectorDormant,
			params:   map[string]any{"dormant_days": int64(30), "reactivation_threshold": float64(1000), "absolute_threshold": float64(100)},
			txns:     []domain.Transaction{{ID: "old", Amount: 10, ExecutedAt: base}, {ID: "new", Amount: 100, ExecutedAt: base.Add(31 * 24 * time.Hour)}},
		},
		{
			name:     "high risk country",
			detector: detectorHighRisk,
			params:   map[string]any{"threshold_amount": float64(1000), "absolute_threshold": float64(100), "high_risk_countries": []any{"KP"}},
			txns:     []domain.Transaction{{ID: "risk", Amount: 100, Direction: domain.DirectionOutbound, CounterpartyCountry: "KP", ExecutedAt: base}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alerts, err := evaluateScenario(context.Background(), scenario{ID: "custom", Detector: tt.detector, Parameters: tt.params}, "individual", "HIGH", tt.txns)
			if err != nil {
				t.Fatal(err)
			}
			if len(alerts) == 0 {
				t.Fatalf("alerts = %+v, want absolute threshold safety valve", alerts)
			}
		})
	}
}
