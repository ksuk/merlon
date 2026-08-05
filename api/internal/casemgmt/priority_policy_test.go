package casemgmt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestPriorityPolicyDerivesOnlyFromCDDState(t *testing.T) {
	policy := DefaultPriorityPolicy()
	high := domain.RiskTierHigh
	customer := &domain.Customer{ID: "cust-1", RiskTier: &high}
	if got := policy.PriorityFor(customer); got != domain.CasePriorityHigh {
		t.Fatalf("high CDD tier priority = %q, want high", got)
	}
	mediumScore := 3.1
	customer = &domain.Customer{ID: "cust-2", RiskScore: &mediumScore}
	if got := policy.PriorityFor(customer); got != domain.CasePriorityMedium {
		t.Fatalf("medium CDD score priority = %q, want medium", got)
	}
	if got := policy.PriorityFor(&domain.Customer{ID: "cust-3"}); got != domain.CasePriorityMedium {
		t.Fatalf("unscored priority = %q, want configured medium fallback", got)
	}
	lowScore := 1.5
	customer = &domain.Customer{ID: "cust-4", RiskScore: &lowScore, RiskTier: &high}
	if got := policy.PriorityFor(customer); got != domain.CasePriorityLow {
		t.Fatalf("score-derived priority = %q, want low even when legacy tier disagrees", got)
	}
}

func TestLoadPriorityPolicyValidatesVersionedConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "priority.yaml")
	if err := os.WriteFile(path, []byte(`schema_version: case_priority_v1
policy_version: test-v1
unscored_priority: medium
tier_priorities: {low: low, medium: medium, high: high}
score_bands:
  - {min: 0, max: 10, priority: low}
  - {min: 10, priority: high}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadPriorityPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Version() != "test-v1" {
		t.Fatalf("policy version = %q", policy.Version())
	}
}
