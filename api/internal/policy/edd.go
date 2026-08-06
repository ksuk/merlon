package policy

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

const eddSchemaVersion = "edd_policy_v1"

// EDDTierDowngrade decides what happens to an open EDD window when the
// customer's risk tier falls below the trigger.
type EDDTierDowngrade string

const (
	// EDDRetainEvidence keeps the stage timestamps and closes the window with
	// a recorded reason, so the escalation history remains auditable.
	EDDRetainEvidence EDDTierDowngrade = "retain_evidence"
	// EDDCloseWindow clears the window, which is the pre-policy behaviour.
	EDDCloseWindow EDDTierDowngrade = "close_window"
)

// EDDStage is one escalation step measured from the moment EDD was requested.
type EDDStage struct {
	Name         string              `yaml:"name" json:"name"`
	AfterDays    int                 `yaml:"after_days" json:"after_days"`
	Action       string              `yaml:"action" json:"action"`
	CasePriority domain.CasePriority `yaml:"case_priority,omitempty" json:"case_priority,omitempty"`
}

// EDDCompletion describes what an operator must supply to close an EDD
// window.
type EDDCompletion struct {
	RequiresRationale bool `yaml:"requires_rationale" json:"requires_rationale"`
	RequiresCaseLink  bool `yaml:"requires_case_link" json:"requires_case_link"`
}

// EDDPolicy is the single source of the EDD stage schedule (DR-14). Before
// this policy the 30/60/90 day figures were duplicated across the
// investigation read model, the escalation job, and the process
// configuration, so the three could disagree.
type EDDPolicy struct {
	SchemaVersion string            `yaml:"schema_version" json:"schema_version"`
	PolicyVersion string            `yaml:"policy_version" json:"policy_version"`
	TriggerTiers  []domain.RiskTier `yaml:"trigger_tiers" json:"trigger_tiers"`
	Stages        []EDDStage        `yaml:"stages" json:"stages"`
	DueDays       int               `yaml:"due_days" json:"due_days"`
	Completion    EDDCompletion     `yaml:"completion" json:"completion"`
	TierDowngrade EDDTierDowngrade  `yaml:"tier_downgrade" json:"tier_downgrade"`
}

// DefaultEDD reproduces the schedule the code carried before the policy
// existed, so adopting the policy changes no behaviour by itself.
func DefaultEDD() *EDDPolicy {
	return &EDDPolicy{
		SchemaVersion: eddSchemaVersion,
		PolicyVersion: "2026-08-06-default",
		TriggerTiers:  []domain.RiskTier{domain.RiskTierHigh},
		Stages: []EDDStage{
			{Name: "stage1", AfterDays: 30, Action: "reminder"},
			{Name: "stage2", AfterDays: 60, Action: "transaction_restriction_recommended", CasePriority: domain.CasePriorityHigh},
			{Name: "stage3", AfterDays: 90, Action: "relationship_decline_recommended", CasePriority: domain.CasePriorityCritical},
		},
		DueDays:       90,
		Completion:    EDDCompletion{RequiresRationale: true},
		TierDowngrade: EDDRetainEvidence,
	}
}

// LoadEDD reads the policy from path, or returns the default when path is
// blank.
func LoadEDD(path string) (*EDDPolicy, error) {
	var loaded EDDPolicy
	present, err := readPolicy("edd", path, &loaded)
	if err != nil {
		return nil, err
	}
	if !present {
		return DefaultEDD(), nil
	}
	if err := loaded.Validate(); err != nil {
		return nil, fmt.Errorf("validate edd policy %q: %w", path, err)
	}
	return &loaded, nil
}

// Validate refuses a schedule the escalation job cannot walk: stages must be
// named, strictly increasing, and the due boundary cannot precede the last
// stage.
func (p *EDDPolicy) Validate() error {
	if p == nil {
		return fmt.Errorf("policy is nil")
	}
	if err := requireVersion("edd", p.SchemaVersion, eddSchemaVersion, p.PolicyVersion); err != nil {
		return err
	}
	if len(p.TriggerTiers) == 0 {
		return fmt.Errorf("trigger_tiers must list at least one tier")
	}
	for _, tier := range p.TriggerTiers {
		switch tier {
		case domain.RiskTierLow, domain.RiskTierMedium, domain.RiskTierHigh:
		default:
			return fmt.Errorf("trigger_tiers contains an unknown tier %q", tier)
		}
	}
	if len(p.Stages) == 0 {
		return fmt.Errorf("stages must list at least one stage")
	}
	previous := 0
	names := make([]string, 0, len(p.Stages))
	for i, stage := range p.Stages {
		if strings.TrimSpace(stage.Name) == "" {
			return fmt.Errorf("stages[%d].name is required", i)
		}
		names = append(names, stage.Name)
		if stage.AfterDays <= previous {
			return fmt.Errorf("stages[%d].after_days must be greater than the previous stage", i)
		}
		previous = stage.AfterDays
		if strings.TrimSpace(stage.Action) == "" {
			return fmt.Errorf("stages[%d].action is required", i)
		}
		if stage.CasePriority != "" && !validCasePriority(stage.CasePriority) {
			return fmt.Errorf("stages[%d].case_priority is invalid", i)
		}
	}
	if name, dup := duplicates(names); dup {
		return fmt.Errorf("stages repeat the name %q", name)
	}
	if p.DueDays < previous {
		return fmt.Errorf("due_days must be at least the last stage after_days")
	}
	switch p.TierDowngrade {
	case EDDRetainEvidence, EDDCloseWindow:
	default:
		return fmt.Errorf("tier_downgrade must be retain_evidence or close_window")
	}
	return nil
}

func validCasePriority(priority domain.CasePriority) bool {
	switch priority {
	case domain.CasePriorityLow, domain.CasePriorityMedium, domain.CasePriorityHigh, domain.CasePriorityCritical:
		return true
	default:
		return false
	}
}

func (p *EDDPolicy) resolved() *EDDPolicy {
	if p == nil {
		return DefaultEDD()
	}
	return p
}

// Triggers reports whether tier opens an EDD window.
func (p *EDDPolicy) Triggers(tier domain.RiskTier) bool {
	return slices.Contains(p.resolved().TriggerTiers, tier)
}

// StageDays returns the after_days figure for a named stage, and whether the
// stage exists.
func (p *EDDPolicy) StageDays(name string) (int, bool) {
	for _, stage := range p.resolved().Stages {
		if stage.Name == name {
			return stage.AfterDays, true
		}
	}
	return 0, false
}

// Stage returns the stage a window is currently in, given the days elapsed
// since the request, plus the next stage if one remains. Before the first
// stage fires both are nil/the first stage respectively.
func (p *EDDPolicy) Stage(elapsedDays int) (current *EDDStage, next *EDDStage) {
	stages := p.resolved().Stages
	for i := range stages {
		if elapsedDays >= stages[i].AfterDays {
			current = &stages[i]
			continue
		}
		next = &stages[i]
		break
	}
	return current, next
}

// DueAt returns the moment an EDD window opened at requestedAt becomes
// overdue.
func (p *EDDPolicy) DueAt(requestedAt time.Time) time.Time {
	return requestedAt.AddDate(0, 0, p.resolved().DueDays)
}

// IsOverdue reports whether a window opened at requestedAt has passed its due
// boundary as of now.
func (p *EDDPolicy) IsOverdue(requestedAt, now time.Time) bool {
	return now.After(p.DueAt(requestedAt))
}

// OverdueDays reports how many whole days past due a window is, never
// negative. The read model needs this because clamping remaining days at zero
// made a window 200 days overdue indistinguishable from one due today.
func (p *EDDPolicy) OverdueDays(requestedAt, now time.Time) int {
	due := p.DueAt(requestedAt)
	if !now.After(due) {
		return 0
	}
	return int(now.Sub(due).Hours() / 24)
}

// RetainOnDowngrade reports whether stage evidence survives a tier downgrade.
func (p *EDDPolicy) RetainOnDowngrade() bool {
	return p.resolved().TierDowngrade != EDDCloseWindow
}

// RequiresRationale reports whether closing a window demands a rationale.
func (p *EDDPolicy) RequiresRationale() bool {
	return p.resolved().Completion.RequiresRationale
}

// RequiresCaseLink reports whether closing a window demands a linked case.
func (p *EDDPolicy) RequiresCaseLink() bool {
	return p.resolved().Completion.RequiresCaseLink
}

// Version reports the policy version for audit records.
func (p *EDDPolicy) Version() string {
	if p == nil || strings.TrimSpace(p.PolicyVersion) == "" {
		return "unknown"
	}
	return p.PolicyVersion
}

func (p *EDDPolicy) versionInfo() (string, string) {
	if p == nil {
		return eddSchemaVersion, "unknown"
	}
	return p.SchemaVersion, p.Version()
}
