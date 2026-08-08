package policy

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

const travelRuleSchemaVersion = "travel_rule_v1"

// TravelRuleAssertionAuthority decides whose applicability verdict wins when
// the client's assertion and the policy disagree.
type TravelRuleAssertionAuthority string

const (
	// TravelRuleAssertionClient keeps the client's assertion as the stored
	// value and records the policy verdict alongside it with a conflict flag.
	// This preserves the published contract while making the disagreement
	// auditable.
	TravelRuleAssertionClient TravelRuleAssertionAuthority = "client"
	// TravelRuleAssertionServer rejects a conflicting assertion outright.
	TravelRuleAssertionServer TravelRuleAssertionAuthority = "server"
)

// TravelRuleIncompleteRouting decides where an applicable-but-incomplete
// transfer goes for follow-up.
type TravelRuleIncompleteRouting string

const (
	// TravelRuleRoutePendingReview enqueues the transfer on the operator
	// review queue.
	TravelRuleRoutePendingReview TravelRuleIncompleteRouting = "pending_review"
	// TravelRuleRouteNone records the gap without routing it.
	TravelRuleRouteNone TravelRuleIncompleteRouting = "none"
)

// Not-applicable reason codes shipped by default. An institution may replace
// the list wholesale; the point is that the reason is a closed enumeration
// rather than free text nobody can aggregate.
const (
	TravelRuleReasonBelowThreshold     = "below_threshold"
	TravelRuleReasonNonCoveredChannel  = "non_covered_channel"
	TravelRuleReasonDomesticInternal   = "domestic_internal_transfer"
	TravelRuleReasonFiatOnly           = "fiat_only"
	TravelRuleReasonExemptCounterparty = "exempt_counterparty"
	// TravelRuleReasonOther absorbs a legacy free-text exemption so an
	// existing client is never rejected for supplying prose.
	TravelRuleReasonOther = "other"
)

// TravelRulePolicy expresses when the Travel Rule applies and what evidence a
// complete transfer must carry (DR-16). The shipped threshold is the
// 100,000 JPY equivalent used for crypto-asset transfers; every figure here
// is an institutional setting, not a constant.
type TravelRulePolicy struct {
	SchemaVersion               string                               `yaml:"schema_version" json:"schema_version"`
	PolicyVersion               string                               `yaml:"policy_version" json:"policy_version"`
	ThresholdAmount             float64                              `yaml:"threshold_amount" json:"threshold_amount"`
	ThresholdCurrency           string                               `yaml:"threshold_currency" json:"threshold_currency"`
	CoveredChannels             []string                             `yaml:"covered_channels" json:"covered_channels"`
	CoveredDirections           []string                             `yaml:"covered_directions" json:"covered_directions"`
	ApplicableCounterpartyTypes []domain.CounterpartyType            `yaml:"applicable_counterparty_types" json:"applicable_counterparty_types"`
	RequiredEvidenceFields      map[domain.CounterpartyType][]string `yaml:"required_evidence_fields" json:"required_evidence_fields"`
	NotApplicableReasons        []string                             `yaml:"not_applicable_reasons" json:"not_applicable_reasons"`
	AssertionAuthority          TravelRuleAssertionAuthority         `yaml:"assertion_authority" json:"assertion_authority"`
	IncompleteRouting           TravelRuleIncompleteRouting          `yaml:"incomplete_routing" json:"incomplete_routing"`
}

// DefaultTravelRule ships the 100,000 JPY crypto-asset threshold with client
// assertion authority, so an existing integration keeps working while the
// server verdict starts being recorded.
func DefaultTravelRule() *TravelRulePolicy {
	return &TravelRulePolicy{
		SchemaVersion:     travelRuleSchemaVersion,
		PolicyVersion:     "2026-08-06-default",
		ThresholdAmount:   100000,
		ThresholdCurrency: "JPY",
		CoveredChannels:   []string{"crypto", "virtual_asset", "exchange_transfer"},
		CoveredDirections: []string{"inbound", "outbound"},
		ApplicableCounterpartyTypes: []domain.CounterpartyType{
			domain.CounterpartyTypeVASP,
			domain.CounterpartyTypeUnhostedWallet,
			domain.CounterpartyTypeUnknown,
		},
		RequiredEvidenceFields: map[domain.CounterpartyType][]string{
			domain.CounterpartyTypeVASP: {
				"originator_name", "originator_account", "originator_vasp_name",
				"beneficiary_name", "beneficiary_account", "beneficiary_vasp_name",
			},
			domain.CounterpartyTypeUnhostedWallet: {
				"originator_name", "originator_account", "beneficiary_account",
				"wallet_attribution_method",
			},
			domain.CounterpartyTypeUnknown: {
				"originator_name", "beneficiary_account",
			},
		},
		NotApplicableReasons: []string{
			TravelRuleReasonBelowThreshold,
			TravelRuleReasonNonCoveredChannel,
			TravelRuleReasonDomesticInternal,
			TravelRuleReasonFiatOnly,
			TravelRuleReasonExemptCounterparty,
			TravelRuleReasonOther,
		},
		AssertionAuthority: TravelRuleAssertionClient,
		IncompleteRouting:  TravelRuleRoutePendingReview,
	}
}

// LoadTravelRule reads the policy from path, or returns the default when path
// is blank.
func LoadTravelRule(path string) (*TravelRulePolicy, error) {
	var loaded TravelRulePolicy
	present, err := readPolicy("travel rule", path, &loaded)
	if err != nil {
		return nil, err
	}
	if !present {
		return DefaultTravelRule(), nil
	}
	if err := loaded.Validate(); err != nil {
		return nil, fmt.Errorf("validate travel rule policy %q: %w", path, err)
	}
	return &loaded, nil
}

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// Validate refuses a policy the assessor cannot apply.
func (p *TravelRulePolicy) Validate() error {
	if p == nil {
		return fmt.Errorf("policy is nil")
	}
	if err := requireVersion("travel rule", p.SchemaVersion, travelRuleSchemaVersion, p.PolicyVersion); err != nil {
		return err
	}
	if p.ThresholdAmount <= 0 {
		return fmt.Errorf("threshold_amount must be greater than zero")
	}
	if !currencyPattern.MatchString(p.ThresholdCurrency) {
		return fmt.Errorf("threshold_currency must be a three-letter uppercase code")
	}
	if len(p.CoveredChannels) == 0 {
		return fmt.Errorf("covered_channels must list at least one channel")
	}
	if value, dup := duplicates(p.CoveredChannels); dup {
		return fmt.Errorf("covered_channels repeats %q", value)
	}
	if len(p.CoveredDirections) == 0 {
		return fmt.Errorf("covered_directions must list at least one direction")
	}
	for _, direction := range p.CoveredDirections {
		switch direction {
		case "inbound", "outbound", "internal":
		default:
			return fmt.Errorf("covered_directions contains an unknown direction %q", direction)
		}
	}
	for _, counterpartyType := range p.ApplicableCounterpartyTypes {
		if !validCounterpartyType(counterpartyType) {
			return fmt.Errorf("applicable_counterparty_types contains an unknown type %q", counterpartyType)
		}
	}
	for _, key := range sortedCounterpartyTypes(p.RequiredEvidenceFields) {
		if !validCounterpartyType(domain.CounterpartyType(key)) {
			return fmt.Errorf("required_evidence_fields.%s is not a known counterparty type", key)
		}
		if value, dup := duplicates(p.RequiredEvidenceFields[domain.CounterpartyType(key)]); dup {
			return fmt.Errorf("required_evidence_fields.%s repeats %q", key, value)
		}
	}
	if len(p.NotApplicableReasons) == 0 {
		return fmt.Errorf("not_applicable_reasons must list at least one reason")
	}
	if value, dup := duplicates(p.NotApplicableReasons); dup {
		return fmt.Errorf("not_applicable_reasons repeats %q", value)
	}
	switch p.AssertionAuthority {
	case TravelRuleAssertionClient, TravelRuleAssertionServer:
	default:
		return fmt.Errorf("assertion_authority must be client or server")
	}
	switch p.IncompleteRouting {
	case TravelRuleRoutePendingReview, TravelRuleRouteNone:
	default:
		return fmt.Errorf("incomplete_routing must be pending_review or none")
	}
	return nil
}

func validCounterpartyType(counterpartyType domain.CounterpartyType) bool {
	switch counterpartyType {
	case domain.CounterpartyTypeVASP, domain.CounterpartyTypeUnhostedWallet, domain.CounterpartyTypeUnknown:
		return true
	default:
		return false
	}
}

func sortedCounterpartyTypes(in map[domain.CounterpartyType][]string) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, string(key))
	}
	sort.Strings(out)
	return out
}

func (p *TravelRulePolicy) resolved() *TravelRulePolicy {
	if p == nil {
		return DefaultTravelRule()
	}
	return p
}

// Assessment is the recorded verdict for one transaction. It is persisted
// verbatim so a later reviewer can see which policy version decided what, on
// what threshold, and what evidence was missing at the time.
type Assessment struct {
	PolicyVersion string    `json:"policy_version"`
	Applicable    bool      `json:"applicable"`
	ReasonCode    string    `json:"reason_code,omitempty"`
	MissingFields []string  `json:"missing_fields,omitempty"`
	Threshold     float64   `json:"threshold"`
	Currency      string    `json:"currency"`
	Conflict      bool      `json:"conflict,omitempty"`
	EvaluatedAt   time.Time `json:"evaluated_at"`
}

// Assess decides whether the Travel Rule applies to a transaction and what
// evidence is still missing. baseAmount is the transaction value already
// converted to the policy's threshold currency by the caller; the policy does
// not carry an FX concept of its own.
func (p *TravelRulePolicy) Assess(transaction *domain.Transaction, baseAmount float64, now time.Time) Assessment {
	policy := p.resolved()
	assessment := Assessment{
		PolicyVersion: policy.Version(),
		Threshold:     policy.ThresholdAmount,
		Currency:      policy.ThresholdCurrency,
		EvaluatedAt:   now.UTC(),
	}
	if transaction == nil {
		assessment.ReasonCode = TravelRuleReasonNonCoveredChannel
		return assessment
	}
	if !slices.Contains(policy.CoveredChannels, transaction.Channel) {
		assessment.ReasonCode = TravelRuleReasonFiatOnly
		return assessment
	}
	if len(policy.CoveredDirections) > 0 && !slices.Contains(policy.CoveredDirections, string(transaction.Direction)) {
		assessment.ReasonCode = TravelRuleReasonDomesticInternal
		return assessment
	}
	counterpartyType := domain.CounterpartyTypeUnknown
	if transaction.Counterparty != nil && transaction.Counterparty.CounterpartyType != "" {
		counterpartyType = transaction.Counterparty.CounterpartyType
	}
	if len(policy.ApplicableCounterpartyTypes) > 0 && !slices.Contains(policy.ApplicableCounterpartyTypes, counterpartyType) {
		assessment.ReasonCode = TravelRuleReasonExemptCounterparty
		return assessment
	}
	if baseAmount < policy.ThresholdAmount {
		assessment.ReasonCode = TravelRuleReasonBelowThreshold
		return assessment
	}
	assessment.Applicable = true
	assessment.MissingFields = policy.MissingEvidence(counterpartyType, transaction)
	return assessment
}

// MissingEvidence lists the required evidence fields the transaction does not
// yet carry, resolving each field name against both the structured
// counterparty and the free-form evidence map.
func (p *TravelRulePolicy) MissingEvidence(counterpartyType domain.CounterpartyType, transaction *domain.Transaction) []string {
	policy := p.resolved()
	required := policy.RequiredEvidenceFields[counterpartyType]
	var missing []string
	for _, field := range required {
		if !travelRuleEvidencePresent(field, transaction) {
			missing = append(missing, field)
		}
	}
	return missing
}

func travelRuleEvidencePresent(field string, transaction *domain.Transaction) bool {
	if transaction == nil {
		return false
	}
	if value, ok := transaction.TravelRuleEvidence[field]; ok {
		if text, isText := value.(string); isText {
			if strings.TrimSpace(text) != "" {
				return true
			}
		} else if value != nil {
			return true
		}
	}
	counterparty := transaction.Counterparty
	if counterparty == nil {
		return false
	}
	switch field {
	case "originator_name":
		return strings.TrimSpace(counterparty.Originator.Name) != ""
	case "originator_account":
		return strings.TrimSpace(counterparty.Originator.AccountNumber) != ""
	case "originator_vasp_name":
		return strings.TrimSpace(counterparty.Originator.VASPName) != ""
	case "beneficiary_name":
		return strings.TrimSpace(counterparty.Beneficiary.Name) != ""
	case "beneficiary_account":
		return strings.TrimSpace(counterparty.Beneficiary.AccountNumber) != ""
	case "beneficiary_vasp_name":
		return strings.TrimSpace(counterparty.Beneficiary.VASPName) != ""
	default:
		return false
	}
}

// BaseCurrency reports the currency the threshold is denominated in. Named
// distinctly from the ThresholdCurrency field so callers holding a nil policy
// still get the default.
func (p *TravelRulePolicy) BaseCurrency() string {
	return p.resolved().ThresholdCurrency
}

// ValidReasonCode reports whether code is one of the policy's permitted
// not-applicable reasons.
func (p *TravelRulePolicy) ValidReasonCode(code string) bool {
	return slices.Contains(p.resolved().NotApplicableReasons, code)
}

// ReasonCodes lists the permitted not-applicable reasons.
func (p *TravelRulePolicy) ReasonCodes() []string {
	return append([]string(nil), p.resolved().NotApplicableReasons...)
}

// ServerDecides reports whether a conflicting client assertion is rejected
// rather than recorded.
func (p *TravelRulePolicy) ServerDecides() bool {
	return p.resolved().AssertionAuthority == TravelRuleAssertionServer
}

// RoutesIncompleteToReview reports whether an applicable-but-incomplete
// transfer is enqueued for operator follow-up.
func (p *TravelRulePolicy) RoutesIncompleteToReview() bool {
	return p.resolved().IncompleteRouting == TravelRuleRoutePendingReview
}

// Version reports the policy version for audit records.
func (p *TravelRulePolicy) Version() string {
	if p == nil || strings.TrimSpace(p.PolicyVersion) == "" {
		return "unknown"
	}
	return p.PolicyVersion
}

func (p *TravelRulePolicy) versionInfo() (string, string) {
	if p == nil {
		return travelRuleSchemaVersion, "unknown"
	}
	return p.SchemaVersion, p.Version()
}
