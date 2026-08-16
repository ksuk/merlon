package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"gopkg.in/yaml.v3"
)

const cddReviewSchemaVersion = "cdd_review_policy_v1"

type CDDReviewAnchor string

const (
	AnchorLastCompletedReview CDDReviewAnchor = "last_completed_review"
	AnchorLastScoredAt        CDDReviewAnchor = "last_scored_at"
	AnchorCustomerCreatedAt   CDDReviewAnchor = "customer_created_at"
)

type CDDReviewCompletion struct {
	RequiresRationale bool     `yaml:"requires_rationale" json:"requires_rationale"`
	Roles             []string `yaml:"roles" json:"roles"`
}

// CDDReviewPolicy is the versioned schedule for periodic CDD reviews.  The
// policy owns timing and completion authority; P08 owns the durable queue and
// completion records that consume this calculation.
type CDDReviewPolicy struct {
	SchemaVersion     string                  `yaml:"schema_version" json:"schema_version"`
	PolicyVersion     string                  `yaml:"policy_version" json:"policy_version"`
	Intervals         map[domain.RiskTier]int `yaml:"intervals" json:"intervals"`
	AnchorPrecedence  []CDDReviewAnchor       `yaml:"anchor_precedence" json:"anchor_precedence"`
	TierIncreaseEarly bool                    `yaml:"tier_increase_early" json:"tier_increase_early"`
	Completion        CDDReviewCompletion     `yaml:"completion" json:"completion"`
	GraceDays         int                     `yaml:"grace_days" json:"grace_days"`
	ColdStartSpread   map[domain.RiskTier]int `yaml:"cold_start_spread" json:"cold_start_spread"`
}

type CDDReviewInput struct {
	CustomerID          string
	Tier                domain.RiskTier
	PreviousTier        domain.RiskTier
	LastCompletedReview *time.Time
	LastScoredAt        *time.Time
	CustomerCreatedAt   time.Time
	AsOf                time.Time
}

type CDDReviewSchedule struct {
	CustomerID      string          `json:"customer_id"`
	Tier            domain.RiskTier `json:"tier"`
	Anchor          CDDReviewAnchor `json:"anchor"`
	AnchorAt        time.Time       `json:"anchor_at"`
	NextReviewAt    time.Time       `json:"next_review_at"`
	GraceUntil      time.Time       `json:"grace_until"`
	ColdStartOffset int             `json:"cold_start_offset_days"`
	PolicyVersion   string          `json:"policy_version"`
	PolicyDigest    string          `json:"policy_digest"`
	TierIncreased   bool            `json:"tier_increased"`
}

func DefaultCDDReviewPolicy() *CDDReviewPolicy {
	return &CDDReviewPolicy{
		SchemaVersion: cddReviewSchemaVersion,
		PolicyVersion: "2026-08-15-default",
		Intervals: map[domain.RiskTier]int{
			domain.RiskTierHigh: 365, domain.RiskTierMedium: 730, domain.RiskTierLow: 1095,
		},
		AnchorPrecedence:  []CDDReviewAnchor{AnchorLastCompletedReview, AnchorLastScoredAt, AnchorCustomerCreatedAt},
		TierIncreaseEarly: true,
		Completion:        CDDReviewCompletion{RequiresRationale: true, Roles: []string{"analyst", "admin"}},
		GraceDays:         30,
		ColdStartSpread: map[domain.RiskTier]int{
			domain.RiskTierHigh: 30, domain.RiskTierMedium: 90, domain.RiskTierLow: 180,
		},
	}
}

func LoadCDDReviewPolicy(path string) (*CDDReviewPolicy, error) {
	if strings.TrimSpace(path) == "" {
		return DefaultCDDReviewPolicy(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read CDD review policy %q: %w", path, err)
	}
	var loaded CDDReviewPolicy
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&loaded); err != nil {
		return nil, fmt.Errorf("parse CDD review policy %q: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, fmt.Errorf("parse CDD review policy %q: multiple YAML documents are not allowed", path)
	} else if err != io.EOF {
		return nil, fmt.Errorf("parse CDD review policy %q: invalid trailing YAML: %w", path, err)
	}
	if err := loaded.Validate(); err != nil {
		return nil, fmt.Errorf("validate CDD review policy %q: %w", path, err)
	}
	return &loaded, nil
}

// LoadCDDReview is a short compatibility alias for callers that use the
// policy type name rather than the longer policy-specific constructor.
func LoadCDDReview(path string) (*CDDReviewPolicy, error) { return LoadCDDReviewPolicy(path) }

func (p *CDDReviewPolicy) Validate() error {
	if p == nil {
		return fmt.Errorf("CDD review policy is nil")
	}
	if err := requireVersion("CDD review", p.SchemaVersion, cddReviewSchemaVersion, p.PolicyVersion); err != nil {
		return err
	}
	for _, tier := range []domain.RiskTier{domain.RiskTierHigh, domain.RiskTierMedium, domain.RiskTierLow} {
		days, ok := p.Intervals[tier]
		if !ok || days <= 0 {
			return fmt.Errorf("intervals.%s must be a positive number of days", tier)
		}
		spread, ok := p.ColdStartSpread[tier]
		if !ok || spread <= 0 {
			return fmt.Errorf("cold_start_spread.%s must be a positive number of days", tier)
		}
	}
	for tier := range p.Intervals {
		if tier != domain.RiskTierHigh && tier != domain.RiskTierMedium && tier != domain.RiskTierLow {
			return fmt.Errorf("intervals contains unknown tier %q", tier)
		}
	}
	for tier := range p.ColdStartSpread {
		if tier != domain.RiskTierHigh && tier != domain.RiskTierMedium && tier != domain.RiskTierLow {
			return fmt.Errorf("cold_start_spread contains unknown tier %q", tier)
		}
	}
	wantAnchors := []CDDReviewAnchor{AnchorLastCompletedReview, AnchorLastScoredAt, AnchorCustomerCreatedAt}
	if len(p.AnchorPrecedence) != len(wantAnchors) {
		return fmt.Errorf("anchor_precedence must list %d anchors", len(wantAnchors))
	}
	for i, anchor := range p.AnchorPrecedence {
		if anchor != wantAnchors[i] {
			return fmt.Errorf("anchor_precedence[%d] must be %q", i, wantAnchors[i])
		}
	}
	if p.GraceDays < 0 {
		return fmt.Errorf("grace_days must not be negative")
	}
	if !p.Completion.RequiresRationale {
		return fmt.Errorf("completion.requires_rationale must be true")
	}
	if !slices.Equal(p.Completion.Roles, []string{"analyst", "admin"}) {
		return fmt.Errorf("completion.roles must contain analyst and admin")
	}
	return nil
}

func (p *CDDReviewPolicy) resolved() *CDDReviewPolicy {
	if p == nil {
		return DefaultCDDReviewPolicy()
	}
	return p
}

func (p *CDDReviewPolicy) Version() string {
	if strings.TrimSpace(p.resolved().PolicyVersion) == "" {
		return "unknown"
	}
	return p.resolved().PolicyVersion
}

func (p *CDDReviewPolicy) Interval(tier domain.RiskTier) int {
	tier = resolveReviewTier(tier)
	return p.resolved().Intervals[tier]
}

func (p *CDDReviewPolicy) GracePeriod() time.Duration {
	return time.Duration(p.resolved().GraceDays) * 24 * time.Hour
}

func (p *CDDReviewPolicy) RoleCanComplete(role string) bool {
	return slices.Contains(p.resolved().Completion.Roles, strings.ToLower(strings.TrimSpace(role)))
}

func (p *CDDReviewPolicy) ValidateCompletion(role, rationale string) error {
	if !p.RoleCanComplete(role) {
		return fmt.Errorf("role %q cannot complete a CDD review", role)
	}
	if p.resolved().Completion.RequiresRationale && strings.TrimSpace(rationale) == "" {
		return fmt.Errorf("CDD review rationale is required")
	}
	return nil
}

func (p *CDDReviewPolicy) Schedule(input CDDReviewInput) CDDReviewSchedule {
	resolved := p.resolved()
	tier := resolveReviewTier(input.Tier)
	anchor, anchorAt := chooseReviewAnchor(input)
	if anchorAt.IsZero() {
		anchorAt = input.AsOf.UTC()
		if anchorAt.IsZero() {
			// A missing anchor is invalid customer data, but returning a stable
			// epoch-based schedule keeps the pure calculation deterministic and
			// lets the caller surface the validation issue without a moving date.
			anchorAt = time.Unix(0, 0).UTC()
		}
	}
	spread := resolved.ColdStartSpread[tier]
	offset := 0
	if anchor == AnchorCustomerCreatedAt && input.LastCompletedReview == nil && input.LastScoredAt == nil {
		offset = coldStartOffset(input.CustomerID, spread)
	}
	next := anchorAt.AddDate(0, 0, resolved.Interval(tier)+offset)
	increased := riskRank(tier) > riskRank(input.PreviousTier) && input.PreviousTier != ""
	if increased && resolved.TierIncreaseEarly {
		early := input.AsOf.UTC()
		if early.IsZero() {
			early = anchorAt
		}
		if early.Before(next) {
			next = early
		}
	}
	return CDDReviewSchedule{CustomerID: input.CustomerID, Tier: tier, Anchor: anchor, AnchorAt: anchorAt,
		NextReviewAt: next, GraceUntil: next.Add(resolved.GracePeriod()), ColdStartOffset: offset,
		PolicyVersion: resolved.Version(), PolicyDigest: digest(resolved), TierIncreased: increased}
}

func chooseReviewAnchor(input CDDReviewInput) (CDDReviewAnchor, time.Time) {
	for _, candidate := range []struct {
		name CDDReviewAnchor
		at   *time.Time
	}{
		{AnchorLastCompletedReview, input.LastCompletedReview},
		{AnchorLastScoredAt, input.LastScoredAt},
	} {
		if candidate.at != nil && !candidate.at.IsZero() {
			return candidate.name, candidate.at.UTC()
		}
	}
	return AnchorCustomerCreatedAt, input.CustomerCreatedAt.UTC()
}

func resolveReviewTier(tier domain.RiskTier) domain.RiskTier {
	switch tier {
	case domain.RiskTierLow, domain.RiskTierMedium, domain.RiskTierHigh:
		return tier
	default:
		// An unscored customer is treated as High so the control fails alert.
		return domain.RiskTierHigh
	}
}

func riskRank(tier domain.RiskTier) int {
	switch resolveReviewTier(tier) {
	case domain.RiskTierHigh:
		return 3
	case domain.RiskTierMedium:
		return 2
	default:
		return 1
	}
}

func coldStartOffset(customerID string, spread int) int {
	if spread <= 0 || strings.TrimSpace(customerID) == "" {
		return 0
	}
	sum := sha256.Sum256([]byte(customerID))
	return int(binary.BigEndian.Uint64(sum[:8]) % uint64(spread))
}

func (p *CDDReviewPolicy) versionInfo() (string, string) {
	resolved := p.resolved()
	return resolved.SchemaVersion, resolved.PolicyVersion
}
