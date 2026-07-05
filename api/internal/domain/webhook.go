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

	// WebhookEventScreeningTruePositive notifies the core system that a
	// screening_results hit was confirmed a true positive so it can decide
	// on an immediate transaction freeze (screening.md "TRUE_POSITIVE：制裁
	// 対象者と同一人物と判定。自動的にケース管理にケースを生成し（severity = CRITICAL）、
	// 該当顧客の取引を即時凍結の判断を基幹に通知する（Webhook screening_true_positive
	// イベント）"). Deliberately a distinct event from WebhookEventScreeningMatch,
	// which fires on the raw single-shot screen call before investigation.
	WebhookEventScreeningTruePositive WebhookEventType = "screening_true_positive"
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
