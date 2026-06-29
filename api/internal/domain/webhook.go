package domain

import (
	"context"
	"time"
)

type WebhookEventType string

const (
	WebhookEventAlertCreated    WebhookEventType = "alert.created"
	WebhookEventAlertResolved   WebhookEventType = "alert.resolved"
	WebhookEventCaseCreated     WebhookEventType = "case.created"
	WebhookEventCaseUpdated     WebhookEventType = "case.updated"
	WebhookEventCaseClosed      WebhookEventType = "case.closed"
	WebhookEventSTRCreated      WebhookEventType = "str.created"
	WebhookEventScoreChanged    WebhookEventType = "score.changed"
	WebhookEventScreeningMatch  WebhookEventType = "screening.match"
)

type Webhook struct {
	ID         string             `json:"id"`
	URL        string             `json:"url"`
	Events     []WebhookEventType `json:"events"`
	Secret     string             `json:"-"`
	Active     bool               `json:"active"`
	CreatedAt  time.Time          `json:"created_at"`
	UpdatedAt  time.Time          `json:"updated_at"`
}

type WebhookDelivery struct {
	ID         string           `json:"id"`
	WebhookID  string           `json:"webhook_id"`
	Event      WebhookEventType `json:"event"`
	Payload    string           `json:"payload"`
	StatusCode int              `json:"status_code"`
	Success    bool             `json:"success"`
	Error      string           `json:"error,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
}

type WebhookRepository interface {
	Get(ctx context.Context, id string) (*Webhook, error)
	List(ctx context.Context) ([]Webhook, error)
	ListByEvent(ctx context.Context, event WebhookEventType) ([]Webhook, error)
	Create(ctx context.Context, w *Webhook) error
	Update(ctx context.Context, w *Webhook) error
	Delete(ctx context.Context, id string) error
	CreateDelivery(ctx context.Context, d *WebhookDelivery) error
	ListDeliveries(ctx context.Context, webhookID string, limit int) ([]WebhookDelivery, error)
}
