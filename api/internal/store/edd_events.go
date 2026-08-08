package store

import (
	"context"
	"sort"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// AppendCustomerEDDEvent records one entry in a customer's EDD history. The
// table is append-only in PostgreSQL, so this is the only way an event is ever
// written and there is no update or delete counterpart.
func (r *PgCustomerRepo) AppendCustomerEDDEvent(ctx context.Context, event *domain.CustomerEDDEvent) error {
	if event == nil || event.CustomerID == "" {
		return &domain.ErrConflict{Entity: "customer_edd_event", Reason: "customer_id is required"}
	}
	if event.ID == "" {
		event.ID = domain.CanonicalUUID(newEventID())
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO customer_edd_events (id, customer_id, event_type, stage, rationale, case_id, actor, policy_version, created_at)
		 VALUES ($1, $2, $3, NULLIF($4,''), $5, NULLIF($6,'')::uuid, $7, $8, $9)`,
		event.ID, domain.CanonicalUUID(event.CustomerID), string(event.EventType), event.Stage,
		event.Rationale, event.CaseID, event.Actor, event.PolicyVersion, event.CreatedAt)
	return err
}

func (r *PgCustomerRepo) ListCustomerEDDEvents(ctx context.Context, customerID string, limit int) ([]domain.CustomerEDDEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, customer_id::text, event_type, COALESCE(stage,''), rationale, COALESCE(case_id::text,''), actor, policy_version, created_at
		 FROM customer_edd_events WHERE customer_id = $1 AND purge_marked_at IS NULL
		 ORDER BY created_at DESC, id DESC LIMIT $2`,
		domain.CanonicalUUID(customerID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.CustomerEDDEvent{}
	for rows.Next() {
		var event domain.CustomerEDDEvent
		if err := rows.Scan(&event.ID, &event.CustomerID, &event.EventType, &event.Stage,
			&event.Rationale, &event.CaseID, &event.Actor, &event.PolicyVersion, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.ID = domain.CanonicalUUID(event.ID)
		event.CustomerID = domain.CanonicalUUID(event.CustomerID)
		out = append(out, event)
	}
	return out, rows.Err()
}

func newEventID() string { return wave3ID() }

// AppendCustomerEDDEvent is the in-memory equivalent, kept append-only by
// construction: nothing in this file mutates a stored event.
func (r *MemoryCustomerRepo) AppendCustomerEDDEvent(_ context.Context, event *domain.CustomerEDDEvent) error {
	if event == nil || event.CustomerID == "" {
		return &domain.ErrConflict{Entity: "customer_edd_event", Reason: "customer_id is required"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.eddEvents == nil {
		r.eddEvents = map[string][]domain.CustomerEDDEvent{}
	}
	stored := *event
	if stored.ID == "" {
		stored.ID = wave3ID()
		event.ID = stored.ID
	}
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now().UTC()
		event.CreatedAt = stored.CreatedAt
	}
	key := domain.CanonicalIdentifier(event.CustomerID)
	r.eddEvents[key] = append(r.eddEvents[key], stored)
	return nil
}

func (r *MemoryCustomerRepo) ListCustomerEDDEvents(_ context.Context, customerID string, limit int) ([]domain.CustomerEDDEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	events := append([]domain.CustomerEDDEvent(nil), r.eddEvents[domain.CanonicalIdentifier(customerID)]...)
	sort.Slice(events, func(i, j int) bool {
		if !events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].CreatedAt.After(events[j].CreatedAt)
		}
		return events[i].ID > events[j].ID
	})
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}
