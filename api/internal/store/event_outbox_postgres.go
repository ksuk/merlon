package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// PgEventOutboxRepo stores event intent in the same DB transaction as the
// business mutation. Publication is deliberately performed by a worker after
// commit, so a transport outage cannot erase a required event.
type PgEventOutboxRepo struct {
	pool DBTX
}

func NewPgEventOutboxRepo(pool DBTX) *PgEventOutboxRepo {
	return &PgEventOutboxRepo{pool: pool}
}

const eventOutboxColumns = "sequence_num, id, topic, payload, chain_id, chain_hop_count, attempts, last_error, created_at, next_attempt_at, published_at"

func scanDurableEvent(row interface{ Scan(dest ...any) error }) (domain.DurableEvent, error) {
	var event domain.DurableEvent
	var lastError *string
	if err := row.Scan(&event.SequenceNum, &event.ID, &event.Topic, &event.Payload, &event.ChainID,
		&event.ChainHopCount, &event.Attempts, &lastError, &event.CreatedAt, &event.NextAttemptAt, &event.PublishedAt); err != nil {
		return event, err
	}
	if lastError != nil {
		event.LastError = *lastError
	}
	return event, nil
}

func (r *PgEventOutboxRepo) Enqueue(ctx context.Context, event *domain.DurableEvent) error {
	if event == nil || strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.Topic) == "" {
		return errors.New("durable event id and topic are required")
	}
	if len(event.Payload) == 0 {
		return errors.New("durable event payload is required")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	err := r.pool.QueryRow(ctx, `INSERT INTO domain_event_outbox
		(id, topic, payload, chain_id, chain_hop_count, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING sequence_num`, event.ID, event.Topic, event.Payload, event.ChainID,
		event.ChainHopCount, event.CreatedAt).Scan(&event.SequenceNum)
	if err != nil && isUniqueViolation(err) {
		return &domain.ErrConflict{Entity: "domain_event_outbox", ID: event.ID, Reason: "event already exists"}
	}
	return err
}

func (r *PgEventOutboxRepo) ListPending(ctx context.Context, limit int) ([]domain.DurableEvent, error) {
	query := `SELECT ` + eventOutboxColumns + ` FROM domain_event_outbox
		WHERE published_at IS NULL AND (next_attempt_at IS NULL OR next_attempt_at <= NOW())
		ORDER BY sequence_num`
	if limit > 0 {
		query += ` LIMIT $1`
		return r.list(ctx, query, limit)
	}
	return r.list(ctx, query)
}

func (r *PgEventOutboxRepo) ListAfter(ctx context.Context, topic string, afterSequence int64, limit int) ([]domain.DurableEvent, error) {
	query := `SELECT ` + eventOutboxColumns + ` FROM domain_event_outbox
		WHERE topic=$1 AND sequence_num>$2 ORDER BY sequence_num`
	args := []any{topic, afterSequence}
	if limit > 0 {
		query += ` LIMIT $3`
		args = append(args, limit)
	}
	return r.list(ctx, query, args...)
}

func (r *PgEventOutboxRepo) list(ctx context.Context, query string, args ...any) ([]domain.DurableEvent, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.DurableEvent, 0)
	for rows.Next() {
		event, err := scanDurableEvent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func (r *PgEventOutboxRepo) MarkPublished(ctx context.Context, id string, at time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE domain_event_outbox SET published_at=$2, next_attempt_at=NULL, last_error=NULL WHERE id=$1 AND published_at IS NULL`, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM domain_event_outbox WHERE id=$1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return &domain.ErrNotFound{Entity: "domain_event_outbox", ID: id}
		}
	}
	return nil
}

func (r *PgEventOutboxRepo) RecordFailure(ctx context.Context, id string, eventErr error, nextAttemptAt time.Time) error {
	message := "event publication failed"
	if eventErr != nil {
		message = eventErr.Error()
	}
	tag, err := r.pool.Exec(ctx, `UPDATE domain_event_outbox
		SET attempts=attempts+1, last_error=$2, next_attempt_at=$3
		WHERE id=$1 AND published_at IS NULL`, id, message, nextAttemptAt)
	if err != nil {
		return fmt.Errorf("record outbox failure: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "domain_event_outbox", ID: id}
	}
	return nil
}

var _ domain.EventOutboxRepository = (*PgEventOutboxRepo)(nil)
