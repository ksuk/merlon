package domain

import "time"

type TransactionDirection string

const (
	DirectionInbound  TransactionDirection = "inbound"
	DirectionOutbound TransactionDirection = "outbound"
	DirectionInternal TransactionDirection = "internal"
)

type Transaction struct {
	ID                  string               `json:"id"`
	CustomerID          string               `json:"customer_id"`
	ExternalID          string               `json:"external_id"`
	Amount              float64              `json:"amount"`
	Currency            string               `json:"currency"`
	Direction           TransactionDirection `json:"direction"`
	CounterpartyID      string               `json:"counterparty_id,omitempty"`
	CounterpartyCountry string               `json:"counterparty_country,omitempty"`
	Channel             string               `json:"channel,omitempty"`
	// AccountID optionally links this transaction to a joint account
	// (the data model §1.1.3, WS-11 Task 4). Nil preserves the pre-existing
	// single-customer-account model.
	AccountID *string `json:"account_id,omitempty"`
	// Counterparty holds travel-rule (originator/beneficiary) data for
	// virtual-asset transfers (the data model §1.3.1, WS-11 Task 5). Nil for
	// transactions with no travel-rule counterparty (e.g. domestic fiat).
	Counterparty *Counterparty `json:"counterparty,omitempty"`
	// Metadata carries optional out-of-band enrichment such as
	// chain_analysis_result from an external vendor (the data model §1.3.1 —
	// wallet sanctions screening itself is out of this system's scope; this
	// field is only a receptacle for the vendor's result).
	Metadata map[string]any `json:"metadata,omitempty"`
	// IdempotencyKey mirrors the client's Idempotency-Key header (the HTTP API contract
	// §4.1). Nil when the client omitted the header (optional, not
	// required). A resend using an already-used key must be rejected with
	// 409 rather than creating a second transaction, even if external_id
	// differs.
	IdempotencyKey                *string        `json:"idempotency_key,omitempty"`
	ExecutedAt                    time.Time      `json:"executed_at"`
	CreatedAt                     time.Time      `json:"created_at"`
	TravelRuleApplicable          *bool          `json:"travel_rule_applicable,omitempty"`
	TravelRuleEvidence            map[string]any `json:"travel_rule_evidence,omitempty"`
	TravelRuleNotApplicableReason string         `json:"travel_rule_not_applicable_reason,omitempty"`
	// TravelRuleNotApplicableReasonCode is the closed-enum companion to the
	// free text above. The free text keeps being accepted; a code is what
	// makes "why was this exempt" answerable across a whole book.
	TravelRuleNotApplicableReasonCode string `json:"travel_rule_not_applicable_reason_code,omitempty"`
	// TravelRuleStatus and TravelRuleAssessment are the server's own verdict,
	// recorded for every transaction. The client's assertion above is kept
	// unchanged; where the two disagree, the assessment says so.
	TravelRuleStatus     string         `json:"travel_rule_status,omitempty"`
	TravelRuleAssessment map[string]any `json:"travel_rule_assessment,omitempty"`
}

// CounterpartyType classifies the counterparty side of a virtual-asset
// transfer (the data model §1.3.1).
type CounterpartyType string

const (
	CounterpartyTypeVASP           CounterpartyType = "vasp"
	CounterpartyTypeUnhostedWallet CounterpartyType = "unhosted_wallet"
	CounterpartyTypeUnknown        CounterpartyType = "unknown"
)

// TravelRuleStatus records whether travel-rule originator/beneficiary
// information is complete for a transfer (the data model §1.3.1).
// Incomplete does not block TM evaluation (Fail-Alert: prefer evaluating
// with partial data over silently dropping the transaction).
type TravelRuleStatus string

const (
	TravelRuleComplete      TravelRuleStatus = "complete"
	TravelRuleIncomplete    TravelRuleStatus = "incomplete"
	TravelRuleNotApplicable TravelRuleStatus = "not_applicable"
)

type CounterpartyParty struct {
	Name          string `json:"name"`
	AccountNumber string `json:"account_number"`
	VASPName      string `json:"vasp_name,omitempty"`
}

type Counterparty struct {
	CounterpartyType CounterpartyType  `json:"counterparty_type"`
	Originator       CounterpartyParty `json:"originator"`
	Beneficiary      CounterpartyParty `json:"beneficiary"`
	TravelRuleStatus TravelRuleStatus  `json:"travel_rule_status"`
}
