package domain

import "time"

type CustomerType string

const (
	CustomerTypeIndividual        CustomerType = "individual"
	CustomerTypeCorporateDomestic CustomerType = "corporate_domestic"
	CustomerTypeCorporateForeign  CustomerType = "corporate_foreign"
)

type RiskTier string

const (
	RiskTierLow    RiskTier = "low"
	RiskTierMedium RiskTier = "medium"
	RiskTierHigh   RiskTier = "high"
)

// CustomerStatus is the customer lifecycle state (data-model.md §1.1.2). The
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
	ID           string            `json:"id"`
	ExternalID   string            `json:"external_id"`
	CustomerType CustomerType      `json:"customer_type"`
	CountryCode  string            `json:"country_code"`
	ProductTypes []string          `json:"product_types"`
	Status       CustomerStatus    `json:"status"`
	Attributes   map[string]string `json:"attributes"`
	RiskScore    *float64          `json:"risk_score,omitempty"`
	RiskTier     *RiskTier         `json:"risk_tier,omitempty"`
	LastScoredAt *time.Time        `json:"last_scored_at,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`

	// EDD escalation tracking (case-management.md §EDD未実施継続時の段階的
	// 措置). EddRequestedAt marks when the customer entered the current
	// High-tier EDD requirement window (nil when not in that state).
	// StageNotifiedAt fields make RunEDDEscalationJob idempotent: stage 2/3
	// fire at most once (never re-sent), stage 1 re-fires at most once per
	// calendar day.
	EddRequestedAt      *time.Time `json:"edd_requested_at,omitempty"`
	EddStage1LastSentAt *time.Time `json:"edd_stage1_last_sent_at,omitempty"`
	EddStage2NotifiedAt *time.Time `json:"edd_stage2_notified_at,omitempty"`
	EddStage3NotifiedAt *time.Time `json:"edd_stage3_notified_at,omitempty"`
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
	ID             string    `json:"id"`
	CustomerID     string    `json:"customer_id"`
	Score          float64   `json:"score"`
	Tier           RiskTier  `json:"tier"`
	Factors        []Factor  `json:"factors"`
	RuleSetID      string    `json:"rule_set_id"`
	RuleSetVersion int       `json:"rule_set_version"`
	ScoredAt       time.Time `json:"scored_at"`
}

type Factor struct {
	Name        string  `json:"name"`
	Axis        string  `json:"axis"`
	Score       float64 `json:"score"`
	Description string  `json:"description"`
}
