package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/crypto"
	"github.com/ksuk/merlon/api/internal/domain"
)

// PgWebhookRepo is the durable production implementation of webhook
// configuration and delivery retry state. The delivery rows are the source of
// truth for restart recovery; no retry depends on a process-local timer.
type PgWebhookRepo struct {
	pool      DBTX
	encryptor *crypto.Encryptor
}

func NewPgWebhookRepo(pool DBTX, encryptor *crypto.Encryptor) *PgWebhookRepo {
	return &PgWebhookRepo{pool: pool, encryptor: encryptor}
}

const webhookColumns = "id, url, events, secret_ciphertext, active, created_at, updated_at"

func (r *PgWebhookRepo) Get(ctx context.Context, id string) (*domain.Webhook, error) {
	return r.scanWebhook(r.pool.QueryRow(ctx, `SELECT `+webhookColumns+` FROM webhooks WHERE id=$1`, id), id)
}

func (r *PgWebhookRepo) List(ctx context.Context) ([]domain.Webhook, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+webhookColumns+` FROM webhooks ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Webhook, 0)
	for rows.Next() {
		webhook, err := r.scanWebhook(rows, "")
		if err != nil {
			return nil, err
		}
		out = append(out, *webhook)
	}
	return out, rows.Err()
}

func (r *PgWebhookRepo) ListByEvent(ctx context.Context, event domain.WebhookEventType) ([]domain.Webhook, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+webhookColumns+` FROM webhooks WHERE active AND $1 = ANY(events) ORDER BY created_at DESC, id DESC`, string(event))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Webhook, 0)
	for rows.Next() {
		webhook, err := r.scanWebhook(rows, "")
		if err != nil {
			return nil, err
		}
		out = append(out, *webhook)
	}
	return out, rows.Err()
}

func (r *PgWebhookRepo) Create(ctx context.Context, webhook *domain.Webhook) error {
	secret, err := r.encryptSecret(webhook.Secret)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO webhooks (id,url,events,secret_ciphertext,active,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, webhook.ID, webhook.URL, eventStrings(webhook.Events), nullableString(secret), webhook.Active, webhook.CreatedAt, webhook.UpdatedAt)
	return err
}

func (r *PgWebhookRepo) Update(ctx context.Context, webhook *domain.Webhook) error {
	secret, err := r.encryptSecret(webhook.Secret)
	if err != nil {
		return err
	}
	webhook.UpdatedAt = time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `UPDATE webhooks SET url=$2, events=$3, secret_ciphertext=$4, active=$5, updated_at=$6 WHERE id=$1`,
		webhook.ID, webhook.URL, eventStrings(webhook.Events), nullableString(secret), webhook.Active, webhook.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "webhook", ID: webhook.ID}
	}
	return nil
}

func (r *PgWebhookRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM webhooks WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "webhook", ID: id}
	}
	return nil
}

const webhookDeliveryColumns = "id, webhook_id, event, payload, status_code, success, error, created_at, event_id, attempt_count, next_attempt_at"

func (r *PgWebhookRepo) CreateDelivery(ctx context.Context, delivery *domain.WebhookDelivery) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO webhook_deliveries
		(id,webhook_id,event,payload,status_code,success,error,created_at,event_id,attempt_count,next_attempt_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, delivery.ID, delivery.WebhookID, string(delivery.Event), delivery.Payload,
		delivery.StatusCode, delivery.Success, nullableString(delivery.Error), delivery.CreatedAt, delivery.EventID, delivery.AttemptCount, delivery.NextAttemptAt)
	return err
}

func (r *PgWebhookRepo) UpdateDelivery(ctx context.Context, delivery *domain.WebhookDelivery) error {
	tag, err := r.pool.Exec(ctx, `UPDATE webhook_deliveries SET event=$2,payload=$3,status_code=$4,success=$5,error=$6,created_at=$7,event_id=$8,attempt_count=$9,next_attempt_at=$10 WHERE id=$1`,
		delivery.ID, string(delivery.Event), delivery.Payload, delivery.StatusCode, delivery.Success, nullableString(delivery.Error), delivery.CreatedAt,
		delivery.EventID, delivery.AttemptCount, delivery.NextAttemptAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "webhook_delivery", ID: delivery.ID}
	}
	return nil
}

func (r *PgWebhookRepo) ListDeliveries(ctx context.Context, webhookID string, limit int) ([]domain.WebhookDelivery, error) {
	query := `SELECT ` + webhookDeliveryColumns + ` FROM webhook_deliveries WHERE webhook_id=$1 ORDER BY created_at DESC, id DESC`
	args := []any{webhookID}
	if limit > 0 {
		query += " LIMIT $2"
		args = append(args, limit)
	}
	return r.listDeliveries(ctx, query, args...)
}

func (r *PgWebhookRepo) ListPendingRetries(ctx context.Context, before time.Time) ([]domain.WebhookDelivery, error) {
	return r.listDeliveries(ctx, `SELECT `+webhookDeliveryColumns+` FROM webhook_deliveries
		WHERE success=FALSE AND next_attempt_at IS NOT NULL AND next_attempt_at <= $1
		ORDER BY next_attempt_at, created_at, id`, before)
}

func (r *PgWebhookRepo) listDeliveries(ctx context.Context, query string, args ...any) ([]domain.WebhookDelivery, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.WebhookDelivery, 0)
	for rows.Next() {
		delivery, err := scanWebhookDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, delivery)
	}
	return out, rows.Err()
}

func (r *PgWebhookRepo) CreateDLQEntry(ctx context.Context, entry *domain.DLQEntry) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO webhook_dlq
		(id,webhook_id,event_id,event,payload,attempt_count,last_error,failed_at,reprocessed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, entry.ID, entry.WebhookID, entry.EventID, string(entry.Event), entry.Payload,
		entry.AttemptCount, nullableString(entry.LastError), entry.FailedAt, entry.ReprocessedAt)
	return err
}

func (r *PgWebhookRepo) GetDLQEntry(ctx context.Context, id string) (*domain.DLQEntry, error) {
	row := r.pool.QueryRow(ctx, `SELECT id,webhook_id,event_id,event,payload,attempt_count,last_error,failed_at,reprocessed_at FROM webhook_dlq WHERE id=$1`, id)
	entry, err := scanDLQEntry(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "webhook_dlq_entry", ID: id}
		}
		return nil, err
	}
	return &entry, nil
}

func (r *PgWebhookRepo) ListDLQEntries(ctx context.Context) ([]domain.DLQEntry, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,webhook_id,event_id,event,payload,attempt_count,last_error,failed_at,reprocessed_at FROM webhook_dlq ORDER BY failed_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.DLQEntry, 0)
	for rows.Next() {
		entry, err := scanDLQEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (r *PgWebhookRepo) CountDLQEntries(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM webhook_dlq WHERE reprocessed_at IS NULL`).Scan(&count)
	return count, err
}

func (r *PgWebhookRepo) MarkDLQEntryReprocessed(ctx context.Context, id string, at time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE webhook_dlq SET reprocessed_at=$2 WHERE id=$1`, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "webhook_dlq_entry", ID: id}
	}
	return nil
}

func (r *PgWebhookRepo) scanWebhook(row interface{ Scan(dest ...any) error }, id string) (*domain.Webhook, error) {
	var webhook domain.Webhook
	var events []string
	var secret *string
	if err := row.Scan(&webhook.ID, &webhook.URL, &events, &secret, &webhook.Active, &webhook.CreatedAt, &webhook.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "webhook", ID: id}
		}
		return nil, err
	}
	webhook.Events = make([]domain.WebhookEventType, len(events))
	for i, event := range events {
		webhook.Events[i] = domain.WebhookEventType(event)
	}
	if secret != nil {
		decrypted, err := r.decryptSecret(*secret)
		if err != nil {
			return nil, fmt.Errorf("decrypt webhook secret %s: %w", webhook.ID, err)
		}
		webhook.Secret = decrypted
	}
	return &webhook, nil
}

func scanWebhookDelivery(row interface{ Scan(dest ...any) error }) (domain.WebhookDelivery, error) {
	var delivery domain.WebhookDelivery
	var event, deliveryError *string
	if err := row.Scan(&delivery.ID, &delivery.WebhookID, &event, &delivery.Payload, &delivery.StatusCode, &delivery.Success,
		&deliveryError, &delivery.CreatedAt, &delivery.EventID, &delivery.AttemptCount, &delivery.NextAttemptAt); err != nil {
		return delivery, err
	}
	if event != nil {
		delivery.Event = domain.WebhookEventType(*event)
	}
	if deliveryError != nil {
		delivery.Error = *deliveryError
	}
	return delivery, nil
}

func scanDLQEntry(row interface{ Scan(dest ...any) error }) (domain.DLQEntry, error) {
	var entry domain.DLQEntry
	var event, lastError *string
	if err := row.Scan(&entry.ID, &entry.WebhookID, &entry.EventID, &event, &entry.Payload, &entry.AttemptCount, &lastError, &entry.FailedAt, &entry.ReprocessedAt); err != nil {
		return entry, err
	}
	if event != nil {
		entry.Event = domain.WebhookEventType(*event)
	}
	if lastError != nil {
		entry.LastError = *lastError
	}
	return entry, nil
}

func eventStrings(events []domain.WebhookEventType) []string {
	out := make([]string, len(events))
	for i, event := range events {
		out[i] = string(event)
	}
	return out
}

func (r *PgWebhookRepo) encryptSecret(secret string) (string, error) {
	if secret == "" {
		return "", nil
	}
	if r.encryptor == nil {
		return secret, nil
	}
	return r.encryptor.Encrypt(secret)
}

func (r *PgWebhookRepo) decryptSecret(ciphertext string) (string, error) {
	if r.encryptor == nil {
		return ciphertext, nil
	}
	return r.encryptor.Decrypt(ciphertext)
}

var _ domain.WebhookRepository = (*PgWebhookRepo)(nil)
