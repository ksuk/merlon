package domain

import "time"

type CustomerType string

const (
	CustomerTypeIndividual       CustomerType = "individual"
	CustomerTypeCorporateDomestic CustomerType = "corporate_domestic"
	CustomerTypeCorporateForeign  CustomerType = "corporate_foreign"
)

type RiskTier string

const (
	RiskTierLow    RiskTier = "low"
	RiskTierMedium RiskTier = "medium"
	RiskTierHigh   RiskTier = "high"
)

type Customer struct {
	ID           string            `json:"id"`
	ExternalID   string            `json:"external_id"`
	CustomerType CustomerType      `json:"customer_type"`
	CountryCode  string            `json:"country_code"`
	ProductTypes []string          `json:"product_types"`
	Attributes   map[string]string `json:"attributes"`
	RiskScore    *float64          `json:"risk_score,omitempty"`
	RiskTier     *RiskTier         `json:"risk_tier,omitempty"`
	LastScoredAt *time.Time        `json:"last_scored_at,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
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
