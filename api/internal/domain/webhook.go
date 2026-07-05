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

	// EventID (api.md §4.2, notifications.md §2) is generated once per
	// dispatched event and kept identical across every retry attempt so the
	// receiver can deduplicate.
	EventID string `json:"event_id"`
	// AttemptCount is the number of delivery attempts made so far (1 after
	// the first send). At webhookMaxAttempts (10) the event moves to the DLQ.
	AttemptCount int `json:"attempt_count"`
	// NextAttemptAt is when the retry worker should next try this delivery.
	// Nil means no retry is scheduled (delivery succeeded, or it has been
	// moved to the DLQ after exhausting all attempts).
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
}

// DLQEntry is a webhook event that exhausted its retry budget (api.md §3.1
// "最大再送回数を超過したイベントは Dead Letter Queue（DLQ）に退避する").
// It can be reprocessed from the UI, which is recorded in the audit log.
type DLQEntry struct {
	ID            string           `json:"id"`
	WebhookID     string           `json:"webhook_id"`
	EventID       string           `json:"event_id"`
	Event         WebhookEventType `json:"event"`
	Payload       string           `json:"payload"`
	AttemptCount  int              `json:"attempt_count"`
	LastError     string           `json:"last_error,omitempty"`
	FailedAt      time.Time        `json:"failed_at"`
	ReprocessedAt *time.Time       `json:"reprocessed_at,omitempty"`
}

type WebhookRepository interface {
	Get(ctx context.Context, id string) (*Webhook, error)
	List(ctx context.Context) ([]Webhook, error)
	ListByEvent(ctx context.Context, event WebhookEventType) ([]Webhook, error)
	Create(ctx context.Context, w *Webhook) error
	Update(ctx context.Context, w *Webhook) error
	Delete(ctx context.Context, id string) error
	CreateDelivery(ctx context.Context, d *WebhookDelivery) error
	UpdateDelivery(ctx context.Context, d *WebhookDelivery) error
	ListDeliveries(ctx context.Context, webhookID string, limit int) ([]WebhookDelivery, error)
	// ListPendingRetries returns failed deliveries whose NextAttemptAt is due
	// (non-nil and <= before).
	ListPendingRetries(ctx context.Context, before time.Time) ([]WebhookDelivery, error)

	CreateDLQEntry(ctx context.Context, entry *DLQEntry) error
	GetDLQEntry(ctx context.Context, id string) (*DLQEntry, error)
	ListDLQEntries(ctx context.Context) ([]DLQEntry, error)
	CountDLQEntries(ctx context.Context) (int, error)
	MarkDLQEntryReprocessed(ctx context.Context, id string, at time.Time) error
}
