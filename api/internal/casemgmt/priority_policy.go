package casemgmt

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/ksuk/merlon/api/internal/domain"
	"gopkg.in/yaml.v3"
)

// PriorityBand maps a CDD score interval to a case priority. The lower bound
// is inclusive and the upper bound is exclusive; an omitted Max means there
// is no upper bound.
type PriorityBand struct {
	Min      float64             `yaml:"min"`
	Max      *float64            `yaml:"max,omitempty"`
	Priority domain.CasePriority `yaml:"priority"`
}

// PriorityPolicy is the versioned, operator-editable mapping required by
// ADR-0004. Alert severity is intentionally absent: a case priority is a
// consequence of CDD state, not a second name for TM severity.
type PriorityPolicy struct {
	SchemaVersion  string                                  `yaml:"schema_version"`
	PolicyVersion  string                                  `yaml:"policy_version"`
	Unscored       domain.CasePriority                     `yaml:"unscored_priority"`
	TierPriorities map[domain.RiskTier]domain.CasePriority `yaml:"tier_priorities"`
	ScoreBands     []PriorityBand                          `yaml:"score_bands"`
}

func DefaultPriorityPolicy() *PriorityPolicy {
	return &PriorityPolicy{
		SchemaVersion: "case_priority_v1",
		PolicyVersion: "default-2026-08-04",
		Unscored:      domain.CasePriorityMedium,
		TierPriorities: map[domain.RiskTier]domain.CasePriority{
			domain.RiskTierLow:    domain.CasePriorityLow,
			domain.RiskTierMedium: domain.CasePriorityMedium,
			domain.RiskTierHigh:   domain.CasePriorityHigh,
		},
		ScoreBands: []PriorityBand{
			{Min: 0, Max: float64Ptr(2), Priority: domain.CasePriorityLow},
			{Min: 2, Max: float64Ptr(4), Priority: domain.CasePriorityMedium},
			{Min: 4, Priority: domain.CasePriorityHigh},
		},
	}
}

func float64Ptr(value float64) *float64 { return &value }

func LoadPriorityPolicy(path string) (*PriorityPolicy, error) {
	if strings.TrimSpace(path) == "" {
		return DefaultPriorityPolicy(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read case priority policy %q: %w", path, err)
	}
	var policy PriorityPolicy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("parse case priority policy %q: %w", path, err)
	}
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("validate case priority policy %q: %w", path, err)
	}
	return &policy, nil
}

func (p *PriorityPolicy) Validate() error {
	if p == nil {
		return fmt.Errorf("policy is nil")
	}
	if p.SchemaVersion != "case_priority_v1" {
		return fmt.Errorf("schema_version must be case_priority_v1")
	}
	if strings.TrimSpace(p.PolicyVersion) == "" {
		return fmt.Errorf("policy_version is required")
	}
	if !validPriority(p.Unscored) {
		return fmt.Errorf("unscored_priority is invalid")
	}
	for _, tier := range []domain.RiskTier{domain.RiskTierLow, domain.RiskTierMedium, domain.RiskTierHigh} {
		if !validPriority(p.TierPriorities[tier]) {
			return fmt.Errorf("tier_priorities.%s is required and invalid", tier)
		}
	}
	previousMax := math.Inf(-1)
	for i, band := range p.ScoreBands {
		if math.IsNaN(band.Min) || band.Min < previousMax || !validPriority(band.Priority) {
			return fmt.Errorf("score_bands[%d] is invalid or overlaps a previous band", i)
		}
		if band.Max != nil && (*band.Max <= band.Min || math.IsNaN(*band.Max)) {
			return fmt.Errorf("score_bands[%d] max must be greater than min", i)
		}
		if band.Max != nil {
			previousMax = *band.Max
		} else {
			previousMax = math.Inf(1)
		}
	}
	return nil
}

func validPriority(priority domain.CasePriority) bool {
	switch priority {
	case domain.CasePriorityLow, domain.CasePriorityMedium, domain.CasePriorityHigh, domain.CasePriorityCritical:
		return true
	default:
		return false
	}
}

// PriorityFor derives a priority from the persisted CDD score first. The tier
// is a compatibility fallback for legacy rows that have no score; it must not
// silently override the score-to-priority policy. Completely unscored
// customers use the explicit policy fallback.
func (p *PriorityPolicy) PriorityFor(customer *domain.Customer) domain.CasePriority {
	if p == nil || customer == nil {
		return domain.CasePriorityMedium
	}
	if customer.RiskScore != nil {
		for _, band := range p.ScoreBands {
			if *customer.RiskScore >= band.Min && (band.Max == nil || *customer.RiskScore < *band.Max) {
				return band.Priority
			}
		}
	}
	if customer.RiskTier != nil {
		if priority, ok := p.TierPriorities[*customer.RiskTier]; ok {
			return priority
		}
	}
	return p.Unscored
}

func (p *PriorityPolicy) Version() string {
	if p == nil || strings.TrimSpace(p.PolicyVersion) == "" {
		return "unknown"
	}
	return p.PolicyVersion
}
