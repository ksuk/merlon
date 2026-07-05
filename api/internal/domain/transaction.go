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
	// (data-model.md §1.1.3, WS-11 Task 4). Nil preserves the pre-existing
	// single-customer-account model.
	AccountID  *string   `json:"account_id,omitempty"`
	ExecutedAt time.Time `json:"executed_at"`
	CreatedAt  time.Time `json:"created_at"`
}
