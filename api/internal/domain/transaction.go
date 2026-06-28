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
	ExecutedAt          time.Time            `json:"executed_at"`
	CreatedAt           time.Time            `json:"created_at"`
}
