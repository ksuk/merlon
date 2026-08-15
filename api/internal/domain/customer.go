package domain

import "time"

type CustomerType string

const (
	CustomerTypeIndividual        CustomerType = "individual"
	CustomerTypeCorporateDomestic CustomerType = "corporate_domestic"
	CustomerTypeCorporateForeign  CustomerType = "corporate_foreign"
	// CustomerTypeTrust, CustomerTypePartnership, CustomerTypeNPO,
	// CustomerTypeGovernment, and CustomerTypeForeignLegalArrangement extend
	// customer_type for non-natural-person entities beyond ordinary
	// corporations (the data model §1.1.1). Beneficial-owner confirmation is
	// required for all of these except CustomerTypeGovernment (犯収法上の取引
	// 時確認義務は原則免除、ただし制裁リスト照合は実施) — that distinction is
	// screening-scheduler policy (WS-7), not encoded in this type.
	CustomerTypeTrust                   CustomerType = "trust"
	CustomerTypePartnership             CustomerType = "partnership"
	CustomerTypeNPO                     CustomerType = "npo"
	CustomerTypeGovernment              CustomerType = "government"
	CustomerTypeForeignLegalArrangement CustomerType = "foreign_legal_arrangement"
)

// AllCustomerTypes lists every accepted customer type in a stable order. It
// is the single source for validation and for the operator-facing error
// message, so the two can no longer drift apart.
func AllCustomerTypes() []CustomerType {
	return []CustomerType{
		CustomerTypeIndividual,
		CustomerTypeCorporateDomestic,
		CustomerTypeCorporateForeign,
		CustomerTypeTrust,
		CustomerTypePartnership,
		CustomerTypeNPO,
		CustomerTypeGovernment,
		CustomerTypeForeignLegalArrangement,
	}
}

// IsValidCustomerType reports whether ct is an accepted customer type.
func IsValidCustomerType(ct CustomerType) bool {
	for _, candidate := range AllCustomerTypes() {
		if candidate == ct {
			return true
		}
	}
	return false
}

type RiskTier string

const (
	RiskTierLow    RiskTier = "low"
	RiskTierMedium RiskTier = "medium"
	RiskTierHigh   RiskTier = "high"
)

// CustomerStatus is the customer lifecycle state (the data model §1.1.2). The
// core banking/exchange system owns state-transition validity; this system
// only records whatever status it is notified of (Adapter Isolation).
type CustomerStatus string

const (
	CustomerStatusActive  CustomerStatus = "active"
	CustomerStatusDormant CustomerStatus = "dormant"
	CustomerStatusFrozen  CustomerStatus = "frozen"
	CustomerStatusClosed  CustomerStatus = "closed"
)

type Customer struct {
	ID           string         `json:"id"`
	ExternalID   string         `json:"external_id"`
	CustomerType CustomerType   `json:"customer_type"`
	CountryCode  string         `json:"country_code"`
	ProductTypes []string       `json:"product_types"`
	Status       CustomerStatus `json:"status"`
	// Attributes holds business-scalar fields (occupation, industry, etc.) as
	// strings, plus structured fields that are arrays/objects of their own —
	// notably attributes.trust_parties (the data model §1.1.1: JSONB array of
	// settlor/trustee/beneficiary entries for trust/partnership/
	// foreign_legal_arrangement customers) and the direct-PII fields WS-11
	// Task 7 encrypts in place. any (rather than string) is required to round
	// -trip that structure through JSONB without a lossy flatten-to-string
	// step.
	Attributes   map[string]any `json:"attributes"`
	RiskScore    *float64       `json:"risk_score,omitempty"`
	RiskTier     *RiskTier      `json:"risk_tier,omitempty"`
	LastScoredAt *time.Time     `json:"last_scored_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	// SourceUpdatedAt is the source system's monotonic update timestamp. It
	// lets inbound webhook upserts ignore an older delivery without confusing
	// transport arrival time with the core system's version.
	SourceUpdatedAt *time.Time `json:"source_updated_at,omitempty"`

	// EDD escalation tracking (the case-management workflow §EDD未実施継続時の段階的
	// 措置). EddRequestedAt marks when the customer entered the current
	// High-tier EDD requirement window (nil when not in that state).
	// StageNotifiedAt fields make RunEDDEscalationJob idempotent: stage 2/3
	// fire at most once (never re-sent), stage 1 re-fires at most once per
	// calendar day.
	EddRequestedAt      *time.Time `json:"edd_requested_at,omitempty"`
	EddStage1LastSentAt *time.Time `json:"edd_stage1_last_sent_at,omitempty"`
	EddStage2NotifiedAt *time.Time `json:"edd_stage2_notified_at,omitempty"`
	EddStage3NotifiedAt *time.Time `json:"edd_stage3_notified_at,omitempty"`
	// EddCompletedAt/EddClosedAt/EddCloseReason end a window explicitly. A
	// window could previously only be opened and escalated, so an operator who
	// had finished the enhanced due diligence had no way to say so, and the
	// record stayed outstanding forever.
	EddCompletedAt *time.Time `json:"edd_completed_at,omitempty"`
	EddClosedAt    *time.Time `json:"edd_closed_at,omitempty"`
	EddCloseReason string     `json:"edd_close_reason,omitempty"`
	// EddCaseID is the case the escalation job opened, recorded rather than
	// rediscovered by matching a marker string inside a case summary.
	EddCaseID string `json:"edd_case_id,omitempty"`

	// AnonymizedAt marks that this customer's direct-PII Attributes fields
	// have been replaced in response to an APPI deletion request made after
	// the statutory retention period elapsed (RET-004, the data model §3.7).
	// nil means not anonymized.
	AnonymizedAt *time.Time `json:"anonymized_at,omitempty"`
}

// EffectiveStatus returns c.Status, treating the zero value as
// CustomerStatusActive so callers created before status lifecycle tracking
// existed (seed data, fixtures, in-flight requests with no status field)
// behave as active rather than as an unrecognized status.
func (c *Customer) EffectiveStatus() CustomerStatus {
	if c.Status == "" {
		return CustomerStatusActive
	}
	return c.Status
}

type ScoreRecord struct {
	ID               string         `json:"id"`
	CustomerID       string         `json:"customer_id"`
	Score            float64        `json:"score"`
	Tier             RiskTier       `json:"tier"`
	Factors          []Factor       `json:"factors"`
	RuleSetID        string         `json:"rule_set_id"`
	RuleSetSHA256    string         `json:"rule_set_sha256,omitempty"`
	RuleSetVersion   int            `json:"rule_set_version"`
	ScoredAt         time.Time      `json:"scored_at"`
	Rationale        string         `json:"rationale,omitempty"`
	Actor            string         `json:"actor,omitempty"`
	OverrideEvidence map[string]any `json:"override_evidence,omitempty"`
}

type Factor struct {
	Name            string  `json:"name"`
	Axis            string  `json:"axis"`
	Score           float64 `json:"score"`
	Description     string  `json:"description"`
	BusinessMeaning string  `json:"business_meaning,omitempty"`
	Weight          float64 `json:"weight,omitempty"`
	Contribution    float64 `json:"contribution,omitempty"`
	ObservedValue   string  `json:"observed_value,omitempty"`
	Rule            string  `json:"rule,omitempty"`
	Fallback        bool    `json:"fallback,omitempty"`
}
