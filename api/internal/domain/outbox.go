package domain

import (
	"context"
	"encoding/json"
	"time"
)

// DurableEvent is the small, source-of-truth record placed in the
// transactional outbox. The transport may be unavailable after the business
// transaction commits; the row remains available for retry and replay.
type DurableEvent struct {
	ID            string          `json:"id"`
	Topic         string          `json:"topic"`
	Payload       json.RawMessage `json:"payload"`
	SequenceNum   int64           `json:"sequence_num"`
	ChainID       string          `json:"chain_id"`
	ChainHopCount int             `json:"chain_hop_count"`
	Attempts      int             `json:"attempts"`
	LastError     string          `json:"last_error,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	NextAttemptAt *time.Time      `json:"next_attempt_at,omitempty"`
	PublishedAt   *time.Time      `json:"published_at,omitempty"`
}

// EventOutboxRepository is deliberately separate from events.Bus. Enqueue is
// part of the business transaction; publication is an asynchronous delivery
// concern and must never make a committed business row look successful while
// losing the event.
type EventOutboxRepository interface {
	Enqueue(ctx context.Context, event *DurableEvent) error
	ListPending(ctx context.Context, limit int) ([]DurableEvent, error)
	ListAfter(ctx context.Context, topic string, afterSequence int64, limit int) ([]DurableEvent, error)
	MarkPublished(ctx context.Context, id string, at time.Time) error
	RecordFailure(ctx context.Context, id string, err error, nextAttemptAt time.Time) error
}
