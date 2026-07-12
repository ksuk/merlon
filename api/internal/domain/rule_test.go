package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRuleDefinition_JSONRoundTrip(t *testing.T) {
	def := json.RawMessage(`{"schema_version":"1.0","default_score":3,"countries":{"JP":{"score":1}}}`)
	now := time.Now().UTC().Truncate(time.Second)

	original := RuleDefinition{
		ID:          "rule-1",
		Type:        RuleTypeCountryRisk,
		Name:        "country_risk_sample",
		Description: "sample country risk table",
		Definition:  def,
		Version:     1,
		IsActive:    true,
		CreatedBy:   "admin@example.com",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var round RuleDefinition
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !jsonEqual(t, round.Definition, original.Definition) {
		t.Errorf("Definition mismatch after round trip: got %s, want %s", round.Definition, original.Definition)
	}
	if round.ID != original.ID || round.Type != original.Type || round.Name != original.Name {
		t.Errorf("scalar fields mismatch: got %+v, want %+v", round, original)
	}
	if round.Version != original.Version || round.IsActive != original.IsActive {
		t.Errorf("version/is_active mismatch: got %+v, want %+v", round, original)
	}
}

func TestRuleType_Constants(t *testing.T) {
	cases := map[RuleType]string{
		RuleTypeTMScenario:      "TM_SCENARIO",
		RuleTypeCDDWeight:       "CDD_WEIGHT",
		RuleTypeScreeningConfig: "SCREENING_CONFIG",
		RuleTypeCountryRisk:     "COUNTRY_RISK",
	}
	for rt, want := range cases {
		if string(rt) != want {
			t.Errorf("RuleType %v = %q, want %q", rt, string(rt), want)
		}
	}
}

func jsonEqual(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("unmarshal b: %v", err)
	}
	ab, _ := json.Marshal(av)
	bb, _ := json.Marshal(bv)
	return string(ab) == string(bb)
}
