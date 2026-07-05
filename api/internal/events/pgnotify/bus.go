// Package pgnotify implements events.Bus on top of PostgreSQL's
// LISTEN/NOTIFY, the default event transport (03_implementation-plan.md
// design decision: pg_notify default, NATS required only past the
// horizontal-scale / 10k-events-per-day threshold).
package pgnotify

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/merlon-aml/merlon/api/internal/events"
)

func init() {
	events.Register("pg_notify", func(cfg events.Config) (events.Bus, error) {
		if cfg.InstanceCount > 1 {
			slog.Warn("pg_notify event bus does not fan out across multiple API instances; set EVENT_BUS=nats for horizontal scaling", "instance_count", cfg.InstanceCount)
		}
		return New(cfg.Pool), nil
	})
}

// defaultGapWait is how long Subscribe waits for a missing sequence number
// to arrive before falling back to a source-of-truth requery (overview.md
// §4.4 event delivery guarantees: sequence number gap default 5s).
const defaultGapWait = 5 * time.Second

// RequeryFunc re-queries the source-of-truth table (e.g.
// customer_score_history) for events on topic after afterSeq. It backs both
// the reconnect-catchup path and the sequence-gap fallback, since NOTIFY
// payloads carry only enough information to identify an event, not its
// content (PostgreSQL NOTIFY payloads are size-limited).
type RequeryFunc func(ctx context.Context, topic string, afterSeq int64) ([]events.Event, error)

// notifyPayload is the minimal information sent over NOTIFY; consumers
// resolve full event content via RequeryFunc.
type notifyPayload struct {
	EventID     string `json:"event_id"`
	SequenceNum int64  `json:"sequence_num"`
}

// Bus implements events.Bus using PostgreSQL LISTEN/NOTIFY.
type Bus struct {
	pool *pgxpool.Pool

	// Requery is exported so callers (and tests) can inject
	// source-of-truth catchup logic without changing the New(pool)
	// constructor signature that events.NewBus (Task 7) relies on.
	Requery RequeryFunc
	// GapWait overrides the default 5s sequence-gap wait; primarily for
	// tests.
	GapWait time.Duration

	sleep func(time.Duration)

	mu      sync.Mutex
	lastSeq map[string]int64
}

var _ events.Bus = (*Bus)(nil)

// New builds a Bus around pool. pool may be nil for unit tests that only
// exercise the gap-detection/catchup logic directly (Publish/Subscribe
// require a real pool).
func New(pool *pgxpool.Pool) *Bus {
	return &Bus{
		pool:    pool,
		GapWait: defaultGapWait,
		sleep:   time.Sleep,
		lastSeq: make(map[string]int64),
	}
}

// Publish sends a minimal notification (event_id, sequence_num) on e.Topic.
// Full event content is not included in the NOTIFY payload since PostgreSQL
// limits it to roughly 8000 bytes; subscribers resolve details via Requery
// or their own lookup of e.ID.
func (b *Bus) Publish(ctx context.Context, e events.Event) error {
	data, err := json.Marshal(notifyPayload{EventID: e.ID, SequenceNum: e.SequenceNum})
	if err != nil {
		return err
	}
	_, err = b.pool.Exec(ctx, "SELECT pg_notify($1, $2)", e.Topic, string(data))
	return err
}

// Subscribe LISTENs on topic and invokes h for each notification, applying
// sequence-gap detection and reconnect catchup via Requery. It blocks until
// ctx is canceled or the underlying connection is lost.
func (b *Bus) Subscribe(ctx context.Context, topic string, h func(events.Event)) error {
	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{topic}.Sanitize()); err != nil {
		return err
	}

	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Connection dropped: catch up on whatever NOTIFYs were missed
			// while disconnected, then surface the error so the caller can
			// reconnect and re-subscribe.
			b.catchUp(ctx, topic, h)
			return err
		}

		var p notifyPayload
		if jsonErr := json.Unmarshal([]byte(notification.Payload), &p); jsonErr != nil {
			continue
		}
		b.handleNotification(ctx, topic, p, h)
	}
}

// handleNotification delivers a single notification to h, first checking
// for a sequence-number gap against the last sequence seen for topic. A gap
// waits GapWait and then falls back to Requery instead of delivering the
// (possibly out-of-order) notification directly.
func (b *Bus) handleNotification(ctx context.Context, topic string, p notifyPayload, h func(events.Event)) {
	b.mu.Lock()
	last, seen := b.lastSeq[topic]
	b.mu.Unlock()

	if seen && p.SequenceNum > last+1 {
		b.sleep(b.GapWait)
		b.catchUp(ctx, topic, h)
		return
	}

	h(events.Event{ID: p.EventID, Topic: topic, SequenceNum: p.SequenceNum})
	b.mu.Lock()
	b.lastSeq[topic] = p.SequenceNum
	b.mu.Unlock()
}

// catchUp calls Requery for events on topic after the last sequence number
// seen, delivering each to h and advancing lastSeq. It is a no-op if no
// Requery function has been configured.
func (b *Bus) catchUp(ctx context.Context, topic string, h func(events.Event)) {
	if b.Requery == nil {
		return
	}

	b.mu.Lock()
	last := b.lastSeq[topic]
	b.mu.Unlock()

	evs, err := b.Requery(ctx, topic, last)
	if err != nil {
		return
	}
	for _, e := range evs {
		h(e)
		b.mu.Lock()
		b.lastSeq[topic] = e.SequenceNum
		b.mu.Unlock()
	}
}
