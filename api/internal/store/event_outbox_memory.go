package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// MemoryEventOutboxRepo is the database-free implementation of the same
// durable-event contract used by PostgreSQL. The failure hook is intentionally
// narrow: tests can prove that a required enqueue failure rolls back the
// business mutation without making production code aware of test behavior.
type MemoryEventOutboxRepo struct {
	mu             sync.RWMutex
	events         []domain.DurableEvent
	nextSequence   int64
	enqueueFailure error
}

func NewMemoryEventOutboxRepo() *MemoryEventOutboxRepo {
	return &MemoryEventOutboxRepo{nextSequence: 1}
}

func (r *MemoryEventOutboxRepo) SetEnqueueFailure(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enqueueFailure = err
}

func (r *MemoryEventOutboxRepo) Enqueue(_ context.Context, event *domain.DurableEvent) error {
	if event == nil || strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.Topic) == "" {
		return errors.New("durable event id and topic are required")
	}
	if len(event.Payload) == 0 {
		return errors.New("durable event payload is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.enqueueFailure != nil {
		return r.enqueueFailure
	}
	for _, existing := range r.events {
		if existing.ID == event.ID {
			return &domain.ErrConflict{Entity: "domain_event_outbox", ID: event.ID, Reason: "event already exists"}
		}
	}
	copy := cloneDurableEvent(*event)
	copy.SequenceNum = r.nextSequence
	r.nextSequence++
	if copy.CreatedAt.IsZero() {
		copy.CreatedAt = time.Now().UTC()
	}
	r.events = append(r.events, copy)
	*event = cloneDurableEvent(copy)
	return nil
}

func (r *MemoryEventOutboxRepo) ListPending(_ context.Context, limit int) ([]domain.DurableEvent, error) {
	now := time.Now()
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.DurableEvent, 0)
	for _, event := range r.events {
		if event.PublishedAt != nil || (event.NextAttemptAt != nil && event.NextAttemptAt.After(now)) {
			continue
		}
		out = append(out, cloneDurableEvent(event))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *MemoryEventOutboxRepo) ListAfter(_ context.Context, topic string, afterSequence int64, limit int) ([]domain.DurableEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.DurableEvent, 0)
	for _, event := range r.events {
		if event.Topic != topic || event.SequenceNum <= afterSequence {
			continue
		}
		out = append(out, cloneDurableEvent(event))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SequenceNum < out[j].SequenceNum })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *MemoryEventOutboxRepo) MarkPublished(_ context.Context, id string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.events {
		if r.events[i].ID != id {
			continue
		}
		r.events[i].PublishedAt = timePtr(at)
		r.events[i].NextAttemptAt = nil
		r.events[i].LastError = ""
		return nil
	}
	return &domain.ErrNotFound{Entity: "domain_event_outbox", ID: id}
}

func (r *MemoryEventOutboxRepo) RecordFailure(_ context.Context, id string, eventErr error, nextAttemptAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.events {
		if r.events[i].ID != id || r.events[i].PublishedAt != nil {
			continue
		}
		r.events[i].Attempts++
		r.events[i].LastError = "event publication failed"
		if eventErr != nil {
			r.events[i].LastError = eventErr.Error()
		}
		r.events[i].NextAttemptAt = timePtr(nextAttemptAt)
		return nil
	}
	return &domain.ErrNotFound{Entity: "domain_event_outbox", ID: id}
}

func cloneDurableEvent(event domain.DurableEvent) domain.DurableEvent {
	event.Payload = append([]byte(nil), event.Payload...)
	if event.NextAttemptAt != nil {
		at := *event.NextAttemptAt
		event.NextAttemptAt = &at
	}
	if event.PublishedAt != nil {
		at := *event.PublishedAt
		event.PublishedAt = &at
	}
	return event
}

func timePtr(value time.Time) *time.Time { return &value }

var _ domain.EventOutboxRepository = (*MemoryEventOutboxRepo)(nil)
