package domain

import (
	"context"
	"time"
)

type CaseStatus string

const (
	CaseStatusOpen         CaseStatus = "open"
	CaseStatusInvestigating CaseStatus = "investigating"
	CaseStatusEscalated    CaseStatus = "escalated"
	CaseStatusClosed       CaseStatus = "closed"
)

type CasePriority string

const (
	CasePriorityLow    CasePriority = "low"
	CasePriorityMedium CasePriority = "medium"
	CasePriorityHigh   CasePriority = "high"
)

type Case struct {
	ID          string       `json:"id"`
	CustomerID  string       `json:"customer_id"`
	AlertIDs    []string     `json:"alert_ids"`
	Status      CaseStatus   `json:"status"`
	Priority    CasePriority `json:"priority"`
	AssignedTo  string       `json:"assigned_to,omitempty"`
	Summary     string       `json:"summary"`
	Notes       []CaseNote   `json:"notes,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	ClosedAt    *time.Time   `json:"closed_at,omitempty"`
}

type CaseNote struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type CaseRepository interface {
	Get(ctx context.Context, id string) (*Case, error)
	ListByCustomer(ctx context.Context, customerID string) ([]Case, error)
	ListOpen(ctx context.Context, limit, offset int) ([]Case, error)
	ListOpenByCursor(ctx context.Context, limit int, after *Cursor) ([]Case, error)
	Create(ctx context.Context, c *Case) error
	Update(ctx context.Context, c *Case) error
	AddNote(ctx context.Context, caseID string, note *CaseNote) error
}
