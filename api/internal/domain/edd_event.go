package domain

import (
	"context"
	"time"
)

// EDDEventType names what happened to an EDD window. The set is closed
// because these events are the audit answer to "was enhanced due diligence
// ever completed for this customer, by whom, and on what grounds" long after
// the customer's tier has moved on.
type EDDEventType string

const (
	EDDEventRequested         EDDEventType = "requested"
	EDDEventStageEscalated    EDDEventType = "stage_escalated"
	EDDEventCompleted         EDDEventType = "completed"
	EDDEventReopened          EDDEventType = "reopened"
	EDDEventClosedOnDowngrade EDDEventType = "closed_on_downgrade"
)

// CustomerEDDEvent is one append-only entry in a customer's EDD history.
type CustomerEDDEvent struct {
	ID            string       `json:"id"`
	CustomerID    string       `json:"customer_id"`
	EventType     EDDEventType `json:"event_type"`
	Stage         string       `json:"stage,omitempty"`
	Rationale     string       `json:"rationale,omitempty"`
	CaseID        string       `json:"case_id,omitempty"`
	Actor         string       `json:"actor"`
	PolicyVersion string       `json:"policy_version,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
}

// CustomerEDDEventRepository is additive: an adapter that does not implement
// it keeps the pre-Wave-3 EDD behaviour, minus the history.
type CustomerEDDEventRepository interface {
	AppendCustomerEDDEvent(ctx context.Context, event *CustomerEDDEvent) error
	ListCustomerEDDEvents(ctx context.Context, customerID string, limit int) ([]CustomerEDDEvent, error)
}
