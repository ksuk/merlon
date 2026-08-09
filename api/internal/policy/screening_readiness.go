package policy

import (
	"fmt"
	"strings"
	"time"
)

const screeningReadinessSchemaVersion = "screening_readiness_v1"

// ScreeningSource is one configured watchlist source and how fresh it must be
// to count as ready.
type ScreeningSource struct {
	ListID           string `yaml:"list_id" json:"list_id"`
	Required         bool   `yaml:"required" json:"required"`
	FreshnessSeconds int    `yaml:"freshness_seconds,omitempty" json:"freshness_seconds,omitempty"`
}

// ScreeningReadinessPolicy names the watchlist sources a deployment expects
// and how stale each may become. Readiness is deliberately not a gate by
// default: blocking screening during a list-provider outage trades a missed
// detection for a halted operation. Instead an unready required source marks
// the run and its results degraded, so a later reviewer can tell a genuine
// no-hit result from one produced against a stale list.
type ScreeningReadinessPolicy struct {
	SchemaVersion           string            `yaml:"schema_version" json:"schema_version"`
	PolicyVersion           string            `yaml:"policy_version" json:"policy_version"`
	DefaultFreshnessSeconds int               `yaml:"default_freshness_seconds" json:"default_freshness_seconds"`
	MarkRunsDegraded        bool              `yaml:"mark_runs_degraded" json:"mark_runs_degraded"`
	GateScreeningRuns       bool              `yaml:"gate_screening_runs" json:"gate_screening_runs"`
	Sources                 []ScreeningSource `yaml:"sources" json:"sources"`
}

// DefaultScreeningReadiness preserves the 72-hour threshold the code carried
// before the policy existed, over the same source list.
func DefaultScreeningReadiness() *ScreeningReadinessPolicy {
	return &ScreeningReadinessPolicy{
		SchemaVersion:           screeningReadinessSchemaVersion,
		PolicyVersion:           "2026-08-06-default",
		DefaultFreshnessSeconds: int((72 * time.Hour).Seconds()),
		MarkRunsDegraded:        true,
		GateScreeningRuns:       false,
		Sources: []ScreeningSource{
			{ListID: "ofac_sdn", Required: true},
			{ListID: "eu_sanctions", Required: true},
			{ListID: "un_sc", Required: true},
			{ListID: "mof_japan", Required: true},
			{ListID: "pep_provider", Required: false},
		},
	}
}

// LoadScreeningReadiness reads the policy from path, or returns the default
// when path is blank.
func LoadScreeningReadiness(path string) (*ScreeningReadinessPolicy, error) {
	var loaded ScreeningReadinessPolicy
	present, err := readPolicy("screening readiness", path, &loaded)
	if err != nil {
		return nil, err
	}
	if !present {
		return DefaultScreeningReadiness(), nil
	}
	if err := loaded.Validate(); err != nil {
		return nil, fmt.Errorf("validate screening readiness policy %q: %w", path, err)
	}
	return &loaded, nil
}

// Validate refuses a policy that would leave the deployment with nothing it
// must screen against.
func (p *ScreeningReadinessPolicy) Validate() error {
	if p == nil {
		return fmt.Errorf("policy is nil")
	}
	if err := requireVersion("screening readiness", p.SchemaVersion, screeningReadinessSchemaVersion, p.PolicyVersion); err != nil {
		return err
	}
	if p.DefaultFreshnessSeconds <= 0 {
		return fmt.Errorf("default_freshness_seconds must be greater than zero")
	}
	if len(p.Sources) == 0 {
		return fmt.Errorf("sources must list at least one source")
	}
	ids := make([]string, 0, len(p.Sources))
	required := 0
	for i, source := range p.Sources {
		if strings.TrimSpace(source.ListID) == "" {
			return fmt.Errorf("sources[%d].list_id is required", i)
		}
		if source.FreshnessSeconds < 0 {
			return fmt.Errorf("sources[%d].freshness_seconds must not be negative", i)
		}
		ids = append(ids, source.ListID)
		if source.Required {
			required++
		}
	}
	if value, dup := duplicates(ids); dup {
		return fmt.Errorf("sources repeat list_id %q", value)
	}
	if required == 0 {
		return fmt.Errorf("at least one source must be required")
	}
	return nil
}

func (p *ScreeningReadinessPolicy) resolved() *ScreeningReadinessPolicy {
	if p == nil {
		return DefaultScreeningReadiness()
	}
	return p
}

// SourceIDs lists every configured source in policy order. Reporting the
// configured cardinality rather than the imported one is what stops a
// never-imported list from disappearing from the readiness view.
func (p *ScreeningReadinessPolicy) SourceIDs() []string {
	policy := p.resolved()
	out := make([]string, 0, len(policy.Sources))
	for _, source := range policy.Sources {
		out = append(out, source.ListID)
	}
	return out
}

// ThresholdFor returns the freshness window for a source, falling back to the
// policy default.
func (p *ScreeningReadinessPolicy) ThresholdFor(listID string) time.Duration {
	policy := p.resolved()
	for _, source := range policy.Sources {
		if source.ListID == listID && source.FreshnessSeconds > 0 {
			return time.Duration(source.FreshnessSeconds) * time.Second
		}
	}
	return time.Duration(policy.DefaultFreshnessSeconds) * time.Second
}

// Required reports whether the deployment treats this source as mandatory.
// An unknown source is not required: it was not configured, so nothing
// depends on it.
func (p *ScreeningReadinessPolicy) Required(listID string) bool {
	for _, source := range p.resolved().Sources {
		if source.ListID == listID {
			return source.Required
		}
	}
	return false
}

// MarksDegraded reports whether runs made against an unready required source
// are recorded as degraded.
func (p *ScreeningReadinessPolicy) MarksDegraded() bool {
	return p.resolved().MarkRunsDegraded
}

// GatesRuns reports whether an unready required source blocks screening
// outright.
func (p *ScreeningReadinessPolicy) GatesRuns() bool {
	return p.resolved().GateScreeningRuns
}

// Version reports the policy version for audit records.
func (p *ScreeningReadinessPolicy) Version() string {
	if p == nil || strings.TrimSpace(p.PolicyVersion) == "" {
		return "unknown"
	}
	return p.PolicyVersion
}

func (p *ScreeningReadinessPolicy) versionInfo() (string, string) {
	if p == nil {
		return screeningReadinessSchemaVersion, "unknown"
	}
	return p.SchemaVersion, p.Version()
}
