package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ksuk/merlon/api/internal/webhook"
)

// PgInboundWebhookRepo stores the authenticated envelope before any record is
// handed to the common ingestion service.  PayloadCiphertext is already
// encrypted by webhook.Service; this repository never receives plaintext.
type PgInboundWebhookRepo struct{ pool DBTX }

func NewPgInboundWebhookRepo(pool DBTX) *PgInboundWebhookRepo {
	return &PgInboundWebhookRepo{pool: pool}
}

const inboundEventColumns = `id, kind, payload_digest, payload_ciphertext, record_count, status, attempt_count, next_attempt_at, first_received_at, last_attempt_at, completed_at, last_error, created_at, updated_at`

func scanInboundEvent(row interface{ Scan(...any) error }) (*webhook.Event, error) {
	var event webhook.Event
	if err := row.Scan(&event.ID, &event.Kind, &event.PayloadDigest, &event.PayloadCiphertext,
		&event.RecordCount, &event.Status, &event.AttemptCount, &event.NextAttemptAt,
		&event.FirstReceivedAt, &event.LastAttemptAt, &event.CompletedAt, &event.LastError,
		&event.CreatedAt, &event.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, webhook.ErrNotFound
		}
		return nil, err
	}
	return &event, nil
}

func (r *PgInboundWebhookRepo) CreateEvent(ctx context.Context, event *webhook.Event) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO inbound_webhook_events
		(id, kind, payload_digest, payload_ciphertext, record_count, status, attempt_count, next_attempt_at, first_received_at, last_attempt_at, completed_at, last_error, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		event.ID, event.Kind, event.PayloadDigest, event.PayloadCiphertext, event.RecordCount, event.Status,
		event.AttemptCount, event.NextAttemptAt, event.FirstReceivedAt, event.LastAttemptAt, event.CompletedAt,
		event.LastError, event.CreatedAt, event.UpdatedAt)
	if err != nil {
		// PostgreSQL's unique violation is deliberately collapsed to the same
		// error the memory repository returns; Service.Accept then re-reads the
		// event to distinguish an idempotent retry from a race.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return webhook.ErrConflict
		}
		return fmt.Errorf("create inbound webhook event: %w", err)
	}
	return nil
}

func (r *PgInboundWebhookRepo) GetEvent(ctx context.Context, id string) (*webhook.Event, error) {
	return scanInboundEvent(r.pool.QueryRow(ctx, `SELECT `+inboundEventColumns+` FROM inbound_webhook_events WHERE id=$1`, id))
}

func (r *PgInboundWebhookRepo) UpdateEvent(ctx context.Context, event *webhook.Event) error {
	tag, err := r.pool.Exec(ctx, `UPDATE inbound_webhook_events SET kind=$2, payload_digest=$3, payload_ciphertext=$4, record_count=$5, status=$6, attempt_count=$7, next_attempt_at=$8, first_received_at=$9, last_attempt_at=$10, completed_at=$11, last_error=$12, updated_at=$13 WHERE id=$1`,
		event.ID, event.Kind, event.PayloadDigest, event.PayloadCiphertext, event.RecordCount, event.Status,
		event.AttemptCount, event.NextAttemptAt, event.FirstReceivedAt, event.LastAttemptAt, event.CompletedAt,
		event.LastError, event.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return webhook.ErrNotFound
	}
	return nil
}

func (r *PgInboundWebhookRepo) ListDueEvents(ctx context.Context, now time.Time, limit int) ([]webhook.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `SELECT `+inboundEventColumns+` FROM inbound_webhook_events
		WHERE status IN ('accepted','failed','running') AND next_attempt_at <= $1
		ORDER BY next_attempt_at, id LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []webhook.Event
	for rows.Next() {
		event, err := scanInboundEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, *event)
	}
	return events, rows.Err()
}

func (r *PgInboundWebhookRepo) ListOutcomes(ctx context.Context, eventID string) ([]webhook.RecordOutcome, error) {
	rows, err := r.pool.Query(ctx, `SELECT record_index, entity_type, external_id, entity_id, status, reason, created_at FROM inbound_webhook_record_outcomes WHERE event_id=$1 ORDER BY record_index`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var outcomes []webhook.RecordOutcome
	for rows.Next() {
		var outcome webhook.RecordOutcome
		if err := rows.Scan(&outcome.Index, &outcome.EntityType, &outcome.ExternalID, &outcome.EntityID, &outcome.Status, &outcome.Reason, &outcome.CreatedAt); err != nil {
			return nil, err
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, rows.Err()
}

func (r *PgInboundWebhookRepo) SaveOutcomes(ctx context.Context, eventID string, outcomes []webhook.RecordOutcome) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM inbound_webhook_record_outcomes WHERE event_id=$1`, eventID); err != nil {
		return err
	}
	for _, outcome := range outcomes {
		if _, err := tx.Exec(ctx, `INSERT INTO inbound_webhook_record_outcomes(event_id, record_index, entity_type, external_id, entity_id, status, reason, created_at) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8)`,
			eventID, outcome.Index, outcome.EntityType, outcome.ExternalID, outcome.EntityID, outcome.Status, outcome.Reason, outcome.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

var _ webhook.EventRepository = (*PgInboundWebhookRepo)(nil)

// MemoryInboundWebhookRepo is the database-free implementation used by the
// API's local mode.  The alias keeps callers that already obtain repositories
// from store consistent with the other memory repositories.
type MemoryInboundWebhookRepo = webhook.MemoryEventRepository

func NewMemoryInboundWebhookRepo() *MemoryInboundWebhookRepo {
	return webhook.NewMemoryEventRepository()
}
