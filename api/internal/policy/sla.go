package policy

import (
	"fmt"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

const slaSchemaVersion = "sla_policy_v1"

// SLAState is what a deadline is doing. not_configured is first because it is
// the default: a deployment that has written no rules has no deadlines, and
// saying so is not the same as saying nothing is overdue (ADR-0024, DR-07).
type SLAState string

const (
	SLANotConfigured SLAState = "not_configured"
	SLARunning       SLAState = "running"
	SLABreached      SLAState = "breached"
	SLAMet           SLAState = "met"
)

// SLARule is one deadline: how long work of a given severity or priority may
// remain open before it is breached.
//
// Severity applies to alerts, Priority to cases. A rule naming neither applies
// to everything of its kind, which is how a deployment states a single blanket
// deadline without enumerating every level.
type SLARule struct {
	Kind        string `yaml:"kind" json:"kind"`
	Severity    string `yaml:"severity,omitempty" json:"severity,omitempty"`
	Priority    string `yaml:"priority,omitempty" json:"priority,omitempty"`
	WithinHours int    `yaml:"within_hours" json:"within_hours"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// SLAPolicy declares the deadlines this institution holds itself to.
//
// It ships with no rules at all. An internal service target is an institutional
// decision, and inventing one would report work as overdue against a deadline
// nobody set -- the same class of error as showing an unconfigured dependency as
// healthy. An unconfigured deployment therefore reports not_configured, which
// is neither zero nor healthy.
//
// The first version measures plain elapsed time. There is no business calendar
// and no pause: those need calendar maintenance and a state machine of their
// own, and precision in a default nobody configured buys nothing. An
// institution that needs them writes a policy document.
type SLAPolicy struct {
	SchemaVersion string    `yaml:"schema_version" json:"schema_version"`
	PolicyVersion string    `yaml:"policy_version" json:"policy_version"`
	Rules         []SLARule `yaml:"rules" json:"rules"`
}

const (
	SLAKindAlert = "alert"
	SLAKindCase  = "case"
)

// DefaultSLAPolicy is deliberately empty. See the type's documentation.
func DefaultSLAPolicy() *SLAPolicy {
	return &SLAPolicy{
		SchemaVersion: slaSchemaVersion,
		PolicyVersion: "2026-08-08-unset",
		Rules:         []SLARule{},
	}
}

// LoadSLA reads the policy from path, or returns the default when path is blank.
func LoadSLA(path string) (*SLAPolicy, error) {
	var loaded SLAPolicy
	present, err := readPolicy("SLA", path, &loaded)
	if err != nil {
		return nil, err
	}
	if !present {
		return DefaultSLAPolicy(), nil
	}
	if err := loaded.Validate(); err != nil {
		return nil, fmt.Errorf("validate SLA policy %q: %w", path, err)
	}
	return &loaded, nil
}

// Validate refuses a document the server cannot compute a deadline from.
func (p *SLAPolicy) Validate() error {
	if p == nil {
		return fmt.Errorf("SLA policy is nil")
	}
	if err := requireVersion("SLA", p.SchemaVersion, slaSchemaVersion, p.PolicyVersion); err != nil {
		return err
	}
	for i, rule := range p.Rules {
		switch rule.Kind {
		case SLAKindAlert, SLAKindCase:
		default:
			return fmt.Errorf("rules[%d].kind must be %q or %q", i, SLAKindAlert, SLAKindCase)
		}
		if rule.WithinHours <= 0 {
			return fmt.Errorf("rules[%d].within_hours must be positive", i)
		}
		if rule.Kind == SLAKindAlert && rule.Priority != "" {
			return fmt.Errorf("rules[%d] is an alert rule and cannot name a case priority", i)
		}
		if rule.Kind == SLAKindCase && rule.Severity != "" {
			return fmt.Errorf("rules[%d] is a case rule and cannot name an alert severity", i)
		}
	}
	return nil
}

func (p *SLAPolicy) resolved() *SLAPolicy {
	if p == nil {
		return DefaultSLAPolicy()
	}
	return p
}

// Configured reports whether this deployment declared any deadline at all.
func (p *SLAPolicy) Configured() bool {
	return len(p.resolved().Rules) > 0
}

// Version identifies the policy that produced a deadline. An alert or case
// records the version applied to it and is never re-stamped with a later one.
func (p *SLAPolicy) Version() string {
	resolved := p.resolved()
	if strings.TrimSpace(resolved.PolicyVersion) == "" {
		return "unknown"
	}
	return resolved.PolicyVersion
}

// AlertDue computes the deadline for an alert of this severity, measured from
// basis. The second return is false when no rule covers it, which is different
// from a deadline of zero.
func (p *SLAPolicy) AlertDue(severity domain.AlertSeverity, basis time.Time) (time.Time, bool) {
	return p.resolved().due(SLAKindAlert, string(severity), basis)
}

// CaseDue computes the deadline for a case of this priority.
func (p *SLAPolicy) CaseDue(priority string, basis time.Time) (time.Time, bool) {
	return p.resolved().due(SLAKindCase, priority, basis)
}

// due finds the most specific rule that applies. A rule naming the exact level
// wins over a blanket rule for the kind, so a deployment can state one general
// deadline and override it where it matters.
func (p *SLAPolicy) due(kind, level string, basis time.Time) (time.Time, bool) {
	if basis.IsZero() {
		return time.Time{}, false
	}
	var blanket *SLARule
	for i := range p.Rules {
		rule := &p.Rules[i]
		if rule.Kind != kind {
			continue
		}
		named := rule.Severity
		if kind == SLAKindCase {
			named = rule.Priority
		}
		if named == "" {
			if blanket == nil {
				blanket = rule
			}
			continue
		}
		if strings.EqualFold(named, level) {
			return basis.Add(time.Duration(rule.WithinHours) * time.Hour), true
		}
	}
	if blanket != nil {
		return basis.Add(time.Duration(blanket.WithinHours) * time.Hour), true
	}
	return time.Time{}, false
}

// State reports what a deadline is doing at now.
//
// resolvedAt being non-nil means the work finished: whether it finished in time
// is a fact about the past and does not change as the clock moves.
func (p *SLAPolicy) State(due *time.Time, resolvedAt *time.Time, now time.Time) SLAState {
	if !p.Configured() || due == nil {
		return SLANotConfigured
	}
	if resolvedAt != nil {
		if resolvedAt.After(*due) {
			return SLABreached
		}
		return SLAMet
	}
	if now.After(*due) {
		return SLABreached
	}
	return SLARunning
}

func (p *SLAPolicy) versionInfo() (string, string) {
	resolved := p.resolved()
	return resolved.SchemaVersion, resolved.PolicyVersion
}
