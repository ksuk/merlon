package policy

import (
	"path/filepath"
	"testing"
)

func contentPath(name string) string {
	return filepath.Join("..", "..", "..", "content", name)
}

// The YAML shipped in content/ must load and validate. A sample an operator
// copies as a starting point that the loader would reject is worse than no
// sample at all.
func TestShippedContentPoliciesLoad(t *testing.T) {
	if _, err := LoadKYCRequiredFields(contentPath("kyc_required_fields_v1.yaml")); err != nil {
		t.Fatalf("kyc_required_fields_v1.yaml: %v", err)
	}
	if _, err := LoadEDD(contentPath("edd_policy_v1.yaml")); err != nil {
		t.Fatalf("edd_policy_v1.yaml: %v", err)
	}
	if _, err := LoadCDDRuleSelection(contentPath("cdd_rule_selection_v1.yaml")); err != nil {
		t.Fatalf("cdd_rule_selection_v1.yaml: %v", err)
	}
	if _, err := LoadTravelRule(contentPath("travel_rule_v1.yaml")); err != nil {
		t.Fatalf("travel_rule_v1.yaml: %v", err)
	}
	if _, err := LoadScreeningReadiness(contentPath("screening_readiness_v1.yaml")); err != nil {
		t.Fatalf("screening_readiness_v1.yaml: %v", err)
	}
}

// The shipped YAML and the in-code defaults must be the same policy. If they
// drift, a deployment that mounts content/ scores differently from one that
// does not, and neither operator has any way to notice.
func TestShippedContentMatchesInCodeDefaults(t *testing.T) {
	tests := []struct {
		name     string
		fromFile func() (any, error)
		fallback any
	}{
		{
			name:     "kyc_required_fields_v1.yaml",
			fromFile: func() (any, error) { return LoadKYCRequiredFields(contentPath("kyc_required_fields_v1.yaml")) },
			fallback: DefaultKYCRequiredFields(),
		},
		{
			name:     "edd_policy_v1.yaml",
			fromFile: func() (any, error) { return LoadEDD(contentPath("edd_policy_v1.yaml")) },
			fallback: DefaultEDD(),
		},
		{
			name:     "cdd_rule_selection_v1.yaml",
			fromFile: func() (any, error) { return LoadCDDRuleSelection(contentPath("cdd_rule_selection_v1.yaml")) },
			fallback: DefaultCDDRuleSelection(),
		},
		{
			name:     "travel_rule_v1.yaml",
			fromFile: func() (any, error) { return LoadTravelRule(contentPath("travel_rule_v1.yaml")) },
			fallback: DefaultTravelRule(),
		},
		{
			name:     "screening_readiness_v1.yaml",
			fromFile: func() (any, error) { return LoadScreeningReadiness(contentPath("screening_readiness_v1.yaml")) },
			fallback: DefaultScreeningReadiness(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loaded, err := test.fromFile()
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got, want := digest(loaded), digest(test.fallback); got != want {
				t.Fatalf("shipped %s does not match the in-code default\n file digest: %s\n code digest: %s", test.name, got, want)
			}
		})
	}
}
