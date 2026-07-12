package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/crypto"
	"github.com/ksuk/merlon/api/internal/domain"
)

type PgCustomerRepo struct {
	pool *pgxpool.Pool
	// encryptor transparently encrypts/decrypts customers.attributes' direct
	// PII fields (the data model §3.1, WS-11 Task 7). Nil disables encryption
	// entirely (encryption not configured), leaving attributes untouched.
	encryptor *crypto.Encryptor
}

func NewPgCustomerRepo(pool *pgxpool.Pool, encryptor *crypto.Encryptor) *PgCustomerRepo {
	return &PgCustomerRepo{pool: pool, encryptor: encryptor}
}

const customerColumns = `id, external_id, customer_type, country_code, status, product_types, attributes, risk_score, risk_tier, last_scored_at, created_at, updated_at, edd_requested_at, edd_stage1_last_sent_at, edd_stage2_notified_at, edd_stage3_notified_at, anonymized_at`

func (r *PgCustomerRepo) Get(ctx context.Context, id string) (*domain.Customer, error) {
	return r.scanCustomer(ctx, `SELECT `+customerColumns+` FROM customers WHERE id = $1 AND purge_marked_at IS NULL`, id)
}

func (r *PgCustomerRepo) GetByExternalID(ctx context.Context, externalID string) (*domain.Customer, error) {
	return r.scanCustomer(ctx, `SELECT `+customerColumns+` FROM customers WHERE external_id = $1 AND purge_marked_at IS NULL`, externalID)
}

func (r *PgCustomerRepo) scanCustomer(ctx context.Context, query string, arg any) (*domain.Customer, error) {
	var c domain.Customer
	var attrs []byte
	var products []string
	var riskTier *string

	err := r.pool.QueryRow(ctx, query, arg).Scan(
		&c.ID, &c.ExternalID, &c.CustomerType, &c.CountryCode, &c.Status,
		&products, &attrs,
		&c.RiskScore, &riskTier, &c.LastScoredAt,
		&c.CreatedAt, &c.UpdatedAt,
		&c.EddRequestedAt, &c.EddStage1LastSentAt, &c.EddStage2NotifiedAt, &c.EddStage3NotifiedAt,
		&c.AnonymizedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "customer", ID: fmt.Sprintf("%v", arg)}
		}
		return nil, err
	}

	c.ProductTypes = products
	c.Attributes = make(map[string]any)
	if len(attrs) > 0 {
		json.Unmarshal(attrs, &c.Attributes)
	}
	decryptDirectPII(r.encryptor, c.Attributes)
	if riskTier != nil {
		rt := domain.RiskTier(*riskTier)
		c.RiskTier = &rt
	}
	return &c, nil
}

// scanCustomerRows scans a single customers row using customerColumns'
// column order, shared by List/ListByCursor/ListEDDPending.
func scanCustomerRows(rows pgx.Rows, encryptor *crypto.Encryptor) (domain.Customer, error) {
	var c domain.Customer
	var attrs []byte
	var products []string
	var riskTier *string

	if err := rows.Scan(
		&c.ID, &c.ExternalID, &c.CustomerType, &c.CountryCode, &c.Status,
		&products, &attrs,
		&c.RiskScore, &riskTier, &c.LastScoredAt,
		&c.CreatedAt, &c.UpdatedAt,
		&c.EddRequestedAt, &c.EddStage1LastSentAt, &c.EddStage2NotifiedAt, &c.EddStage3NotifiedAt,
		&c.AnonymizedAt,
	); err != nil {
		return c, err
	}

	c.ProductTypes = products
	c.Attributes = make(map[string]any)
	if len(attrs) > 0 {
		json.Unmarshal(attrs, &c.Attributes)
	}
	decryptDirectPII(encryptor, c.Attributes)
	if riskTier != nil {
		rt := domain.RiskTier(*riskTier)
		c.RiskTier = &rt
	}
	return c, nil
}

func (r *PgCustomerRepo) List(ctx context.Context, limit, offset int) ([]domain.Customer, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+customerColumns+` FROM customers WHERE purge_marked_at IS NULL ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var customers []domain.Customer
	for rows.Next() {
		c, err := scanCustomerRows(rows, r.encryptor)
		if err != nil {
			return nil, err
		}
		customers = append(customers, c)
	}
	return customers, rows.Err()
}

func (r *PgCustomerRepo) ListByCursor(ctx context.Context, limit int, after *domain.Cursor) ([]domain.Customer, error) {
	baseQuery := `SELECT ` + customerColumns + ` FROM customers WHERE purge_marked_at IS NULL`

	var (
		rows pgx.Rows
		err  error
	)
	if after == nil {
		rows, err = r.pool.Query(ctx, baseQuery+` ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	} else {
		rows, err = r.pool.Query(ctx, baseQuery+` WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3`,
			after.CreatedAt, after.ID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var customers []domain.Customer
	for rows.Next() {
		c, err := scanCustomerRows(rows, r.encryptor)
		if err != nil {
			return nil, err
		}
		customers = append(customers, c)
	}
	return customers, rows.Err()
}

// ListEDDPending returns High-tier customers with an open EDD requirement
// (the case-management workflow §EDD未実施継続時の段階的措置).
func (r *PgCustomerRepo) ListEDDPending(ctx context.Context) ([]domain.Customer, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+customerColumns+` FROM customers WHERE purge_marked_at IS NULL AND risk_tier = 'high' AND edd_requested_at IS NOT NULL`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var customers []domain.Customer
	for rows.Next() {
		c, err := scanCustomerRows(rows, r.encryptor)
		if err != nil {
			return nil, err
		}
		customers = append(customers, c)
	}
	return customers, rows.Err()
}

func (r *PgCustomerRepo) Create(ctx context.Context, c *domain.Customer) error {
	encryptedAttrs, err := encryptDirectPII(r.encryptor, c.Attributes)
	if err != nil {
		return err
	}
	attrs, _ := json.Marshal(encryptedAttrs)
	status := c.Status
	if status == "" {
		status = domain.CustomerStatusActive
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO customers (id, external_id, customer_type, country_code, status, product_types, attributes, risk_score, risk_tier, last_scored_at, created_at, updated_at, edd_requested_at, edd_stage1_last_sent_at, edd_stage2_notified_at, edd_stage3_notified_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		c.ID, c.ExternalID, c.CustomerType, c.CountryCode, status,
		c.ProductTypes, attrs,
		c.RiskScore, riskTierToNullable(c.RiskTier), c.LastScoredAt,
		c.CreatedAt, c.UpdatedAt,
		c.EddRequestedAt, c.EddStage1LastSentAt, c.EddStage2NotifiedAt, c.EddStage3NotifiedAt,
	)
	return err
}

func (r *PgCustomerRepo) Update(ctx context.Context, c *domain.Customer) error {
	encryptedAttrs, err := encryptDirectPII(r.encryptor, c.Attributes)
	if err != nil {
		return err
	}
	attrs, _ := json.Marshal(encryptedAttrs)
	c.UpdatedAt = time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE customers SET external_id=$2, customer_type=$3, country_code=$4, status=$5, product_types=$6, attributes=$7, risk_score=$8, risk_tier=$9, last_scored_at=$10, updated_at=$11, edd_requested_at=$12, edd_stage1_last_sent_at=$13, edd_stage2_notified_at=$14, edd_stage3_notified_at=$15, anonymized_at=$16 WHERE id=$1`,
		c.ID, c.ExternalID, c.CustomerType, c.CountryCode, c.Status,
		c.ProductTypes, attrs,
		c.RiskScore, riskTierToNullable(c.RiskTier), c.LastScoredAt,
		c.UpdatedAt,
		c.EddRequestedAt, c.EddStage1LastSentAt, c.EddStage2NotifiedAt, c.EddStage3NotifiedAt,
		c.AnonymizedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "customer", ID: c.ID}
	}
	return nil
}

// UpdateStatus reflects a customer_status_changed webhook (the data model
// §1.1.2). reason is not persisted on the row; callers attach it to the
// audit log entry.
func (r *PgCustomerRepo) UpdateStatus(ctx context.Context, id string, status domain.CustomerStatus, _ string) (*domain.Customer, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE customers SET status=$2, updated_at=now() WHERE id=$1`,
		id, status,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, &domain.ErrNotFound{Entity: "customer", ID: id}
	}
	return r.Get(ctx, id)
}

func (r *PgCustomerRepo) SaveScoreRecord(ctx context.Context, rec *domain.ScoreRecord) error {
	factors, _ := json.Marshal(rec.Factors)
	_, err := r.pool.Exec(ctx,
		`INSERT INTO customer_score_history (id, customer_id, score, tier, factors, rule_set_id, rule_set_version, rule_set_sha256, scored_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		rec.ID, rec.CustomerID, rec.Score, string(rec.Tier),
		factors, rec.RuleSetID, rec.RuleSetVersion, nullableString(rec.RuleSetSHA256), rec.ScoredAt,
	)
	return err
}

func (r *PgCustomerRepo) ListScoreHistory(ctx context.Context, customerID string, limit int) ([]domain.ScoreRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, customer_id, score, tier, factors, rule_set_id, rule_set_version, rule_set_sha256, scored_at
		FROM customer_score_history WHERE customer_id = $1 AND purge_marked_at IS NULL ORDER BY scored_at DESC LIMIT $2`,
		customerID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []domain.ScoreRecord
	for rows.Next() {
		var rec domain.ScoreRecord
		var tier string
		var factors []byte
		if err := rows.Scan(
			&rec.ID, &rec.CustomerID, &rec.Score, &tier,
			&factors, &rec.RuleSetID, &rec.RuleSetVersion, &rec.RuleSetSHA256, &rec.ScoredAt,
		); err != nil {
			return nil, err
		}
		rec.Tier = domain.RiskTier(tier)
		if len(factors) > 0 {
			json.Unmarshal(factors, &rec.Factors)
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

func riskTierToNullable(t *domain.RiskTier) *string {
	if t == nil {
		return nil
	}
	s := string(*t)
	return &s
}

// PgTransactionRepo

type PgTransactionRepo struct {
	pool *pgxpool.Pool
}

func NewPgTransactionRepo(pool *pgxpool.Pool) *PgTransactionRepo {
	return &PgTransactionRepo{pool: pool}
}

const transactionColumns = "id, customer_id, external_id, amount, currency, direction, counterparty_id, counterparty_country, channel, account_id, counterparty, metadata, idempotency_key, executed_at, created_at"

func scanTransaction(row pgx.Row) (domain.Transaction, error) {
	var t domain.Transaction
	var counterpartyJSON, metadataJSON []byte
	err := row.Scan(
		&t.ID, &t.CustomerID, &t.ExternalID, &t.Amount, &t.Currency,
		&t.Direction, &t.CounterpartyID, &t.CounterpartyCountry,
		&t.Channel, &t.AccountID, &counterpartyJSON, &metadataJSON,
		&t.IdempotencyKey,
		&t.ExecutedAt, &t.CreatedAt,
	)
	if err != nil {
		return t, err
	}
	if len(counterpartyJSON) > 0 {
		if err := json.Unmarshal(counterpartyJSON, &t.Counterparty); err != nil {
			return t, err
		}
	}
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &t.Metadata); err != nil {
			return t, err
		}
	}
	return t, nil
}

func (r *PgTransactionRepo) Get(ctx context.Context, id string) (*domain.Transaction, error) {
	t, err := scanTransaction(r.pool.QueryRow(ctx,
		`SELECT `+transactionColumns+` FROM transactions WHERE id = $1 AND purge_marked_at IS NULL`, id,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "transaction", ID: id}
		}
		return nil, err
	}
	return &t, nil
}

func (r *PgTransactionRepo) ListByCustomer(ctx context.Context, customerID string, limit, offset int) ([]domain.Transaction, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+transactionColumns+`
		FROM transactions WHERE customer_id = $1 AND purge_marked_at IS NULL ORDER BY executed_at DESC LIMIT $2 OFFSET $3`,
		customerID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []domain.Transaction
	for rows.Next() {
		t, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		txns = append(txns, t)
	}
	return txns, rows.Err()
}

func (r *PgTransactionRepo) ListByCustomerCursor(ctx context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.Transaction, error) {
	baseQuery := `SELECT ` + transactionColumns + `
		FROM transactions WHERE customer_id = $1 AND purge_marked_at IS NULL`

	var (
		rows pgx.Rows
		err  error
	)
	if after == nil {
		rows, err = r.pool.Query(ctx, baseQuery+` ORDER BY created_at DESC, id DESC LIMIT $2`, customerID, limit)
	} else {
		rows, err = r.pool.Query(ctx, baseQuery+` AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4`,
			customerID, after.CreatedAt, after.ID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []domain.Transaction
	for rows.Next() {
		t, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		txns = append(txns, t)
	}
	return txns, rows.Err()
}

func (r *PgTransactionRepo) Create(ctx context.Context, t *domain.Transaction) error {
	var counterpartyJSON, metadataJSON []byte
	if t.Counterparty != nil {
		var err error
		if counterpartyJSON, err = json.Marshal(t.Counterparty); err != nil {
			return err
		}
	}
	if t.Metadata != nil {
		var err error
		if metadataJSON, err = json.Marshal(t.Metadata); err != nil {
			return err
		}
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO transactions (id, customer_id, external_id, amount, currency, direction, counterparty_id, counterparty_country, channel, account_id, counterparty, metadata, idempotency_key, executed_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		t.ID, t.CustomerID, t.ExternalID, t.Amount, t.Currency,
		string(t.Direction), t.CounterpartyID, t.CounterpartyCountry,
		t.Channel, t.AccountID, counterpartyJSON, metadataJSON, t.IdempotencyKey, t.ExecutedAt, t.CreatedAt,
	)
	if err != nil && isIdempotencyKeyViolation(err) {
		return &domain.ErrConflict{Entity: "transaction", ID: t.ID, Reason: "idempotency key already used"}
	}
	return err
}

// isIdempotencyKeyViolation reports whether err is specifically the
// transactions_idempotency_key_idx partial unique violation, as opposed to
// e.g. the pre-existing transactions_external_id_unique constraint (which
// callers already handle/report separately).
func isIdempotencyKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "transactions_idempotency_key_idx"
}

// PgAlertRepo

type PgAlertRepo struct {
	pool *pgxpool.Pool
}

func NewPgAlertRepo(pool *pgxpool.Pool) *PgAlertRepo {
	return &PgAlertRepo{pool: pool}
}

// alertColumns includes suppressed/suppression_reason (WL-004, additive
// columns from migrations/010_whitelist.sql) and aggregation_window_start/
// batch_run_id/batch_reviewed_at (WS-5 Task4, migrations/012_alert_dedup.sql).
const alertColumns = "id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, resolved_at, resolved_by, created_at, updated_at, suppressed, suppression_reason, aggregation_window_start, batch_run_id, batch_reviewed_at"

func scanAlertRow(row interface {
	Scan(dest ...any) error
}, a *domain.Alert) error {
	var suppressionReason *string
	var batchRunID *string
	if err := row.Scan(
		&a.ID, &a.CustomerID, &a.ScenarioID,
		&a.Severity, &a.Status, &a.Score, &a.Description,
		&a.TransactionIDs,
		&a.DetectedAt, &a.ResolvedAt, &a.ResolvedBy,
		&a.CreatedAt, &a.UpdatedAt,
		&a.Suppressed, &suppressionReason,
		&a.AggregationWindowStart, &batchRunID, &a.BatchReviewedAt,
	); err != nil {
		return err
	}
	if suppressionReason != nil {
		a.SuppressionReason = *suppressionReason
	}
	if batchRunID != nil {
		a.BatchRunID = *batchRunID
	}
	return nil
}

func (r *PgAlertRepo) scanAlert(row pgx.Row) (*domain.Alert, error) {
	var a domain.Alert
	if err := scanAlertRow(row, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *PgAlertRepo) Get(ctx context.Context, id string) (*domain.Alert, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+alertColumns+`
		FROM alerts WHERE id = $1 AND purge_marked_at IS NULL`, id,
	)
	a, err := r.scanAlert(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "alert", ID: id}
		}
		return nil, err
	}
	return a, nil
}

func (r *PgAlertRepo) ListByCustomer(ctx context.Context, customerID string, limit, offset int) ([]domain.Alert, error) {
	return r.listAlerts(ctx,
		`SELECT `+alertColumns+`
		FROM alerts WHERE customer_id = $1 AND purge_marked_at IS NULL ORDER BY detected_at DESC LIMIT $2 OFFSET $3`,
		customerID, limit, offset,
	)
}

func (r *PgAlertRepo) ListOpen(ctx context.Context, limit, offset int) ([]domain.Alert, error) {
	return r.listAlerts(ctx,
		`SELECT `+alertColumns+`
		FROM alerts WHERE purge_marked_at IS NULL AND status = 'open' ORDER BY severity DESC, detected_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
}

func (r *PgAlertRepo) ListByCustomerCursor(ctx context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.Alert, error) {
	baseQuery := `SELECT ` + alertColumns + `
		FROM alerts WHERE customer_id = $1 AND purge_marked_at IS NULL`

	if after == nil {
		return r.listAlerts(ctx, baseQuery+` ORDER BY created_at DESC, id DESC LIMIT $2`, customerID, limit)
	}
	return r.listAlerts(ctx, baseQuery+` AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4`,
		customerID, after.CreatedAt, after.ID, limit)
}

func (r *PgAlertRepo) ListOpenByCursor(ctx context.Context, limit int, after *domain.Cursor) ([]domain.Alert, error) {
	baseQuery := `SELECT ` + alertColumns + `
		FROM alerts WHERE purge_marked_at IS NULL AND status = 'open'`

	if after == nil {
		return r.listAlerts(ctx, baseQuery+` ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	}
	return r.listAlerts(ctx, baseQuery+` AND (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3`,
		after.CreatedAt, after.ID, limit)
}

// ListByFilter returns alerts matching f, for bulk operations (WS-8 Task 7,
// the case-management workflow §アラートの一括処理).
func (r *PgAlertRepo) ListByFilter(ctx context.Context, f domain.AlertBulkFilter) ([]domain.Alert, error) {
	query := `SELECT ` + alertColumns + ` FROM alerts`
	var args []any
	conditions := []string{"purge_marked_at IS NULL"}
	argIdx := 1

	if f.ScenarioID != "" {
		conditions = append(conditions, fmt.Sprintf("scenario_id = $%d", argIdx))
		args = append(args, f.ScenarioID)
		argIdx++
	}
	if f.Severity != "" {
		conditions = append(conditions, fmt.Sprintf("severity = $%d", argIdx))
		args = append(args, string(f.Severity))
		argIdx++
	}
	if f.PeriodFrom != nil {
		conditions = append(conditions, fmt.Sprintf("detected_at >= $%d", argIdx))
		args = append(args, *f.PeriodFrom)
		argIdx++
	}
	if f.PeriodTo != nil {
		conditions = append(conditions, fmt.Sprintf("detected_at <= $%d", argIdx))
		args = append(args, *f.PeriodTo)
		argIdx++
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY detected_at DESC"

	return r.listAlerts(ctx, query, args...)
}

func (r *PgAlertRepo) listAlerts(ctx context.Context, query string, args ...any) ([]domain.Alert, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []domain.Alert
	for rows.Next() {
		var a domain.Alert
		if err := scanAlertRow(rows, &a); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

func (r *PgAlertRepo) Create(ctx context.Context, a *domain.Alert) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO alerts (id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, created_at, updated_at, suppressed, suppression_reason, aggregation_window_start, batch_run_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		a.ID, a.CustomerID, a.ScenarioID,
		string(a.Severity), string(a.Status), a.Score, a.Description,
		a.TransactionIDs,
		a.DetectedAt, a.CreatedAt, a.UpdatedAt,
		a.Suppressed, nullableString(a.SuppressionReason),
		a.AggregationWindowStart, nullableString(a.BatchRunID),
	)
	return err
}

// CreateIfNotDuplicate enforces the (customer_id, scenario_id,
// aggregation_window_start) partial unique index (idx_alerts_dedup,
// migrations/012_alert_dedup.sql). A unique_violation means another alert
// already occupies that window; the caller (Task7's batch routing) is
// expected to annotate it via AnnotateBatchReviewed rather than treat this
// as an error.
func (r *PgAlertRepo) CreateIfNotDuplicate(ctx context.Context, a *domain.Alert) (bool, *domain.Alert, error) {
	err := r.Create(ctx, a)
	if err == nil {
		return true, nil, nil
	}
	if !isUniqueViolation(err) {
		return false, nil, err
	}
	existing, getErr := r.getByDedupKey(ctx, a.CustomerID, a.ScenarioID, a.AggregationWindowStart)
	if getErr != nil {
		return false, nil, getErr
	}
	return false, existing, nil
}

func (r *PgAlertRepo) getByDedupKey(ctx context.Context, customerID, scenarioID string, windowStart *time.Time) (*domain.Alert, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+alertColumns+`
		FROM alerts WHERE customer_id = $1 AND scenario_id = $2 AND aggregation_window_start = $3`,
		customerID, scenarioID, windowStart,
	)
	return r.scanAlert(row)
}

// AnnotateBatchReviewed sets batch_reviewed_at without touching status,
// severity, or any other field (Task4/Task7). batch_run_id is only backfilled
// via COALESCE if unset, so the realtime creator's original attribution
// (nil batch_run_id) is preserved rather than overwritten by the reviewing
// batch run.
func (r *PgAlertRepo) AnnotateBatchReviewed(ctx context.Context, alertID string, batchRunID string) error {
	now := time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE alerts SET batch_reviewed_at = $2, batch_run_id = COALESCE(batch_run_id, $3), updated_at = $2 WHERE id = $1`,
		alertID, now, nullableString(batchRunID),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "alert", ID: alertID}
	}
	return nil
}

func (r *PgAlertRepo) UpdateStatus(ctx context.Context, id string, status domain.AlertStatus, resolvedBy string) error {
	now := time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE alerts SET status=$2, resolved_by=$3, resolved_at=$4, updated_at=$5 WHERE id=$1`,
		id, string(status), resolvedBy, now, now,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "alert", ID: id}
	}
	return nil
}

// UpdateStatusIfUnmodified is UpdateStatus guarded by an optimistic-lock
// check against expectedUpdatedAt (the data model §3.9). Zero rows affected
// because of a stale expectedUpdatedAt (row exists but its updated_at
// moved on) is reported as *domain.ErrConflict; zero rows because the
// alert doesn't exist at all is reported as *domain.ErrNotFound.
func (r *PgAlertRepo) UpdateStatusIfUnmodified(ctx context.Context, id string, status domain.AlertStatus, resolvedBy string, expectedUpdatedAt time.Time) error {
	now := time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE alerts SET status=$2, resolved_by=$3, resolved_at=$4, updated_at=$5 WHERE id=$1 AND updated_at=$6`,
		id, string(status), resolvedBy, now, now, expectedUpdatedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.Get(ctx, id); err != nil {
			return err
		}
		return &domain.ErrConflict{Entity: "alert", ID: id, Reason: "updated_at mismatch"}
	}
	return nil
}

func (r *PgAlertRepo) EscalateSeverity(ctx context.Context, id string, severity domain.AlertSeverity) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE alerts SET severity=$2, updated_at=now() WHERE id=$1`,
		id, string(severity),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "alert", ID: id}
	}
	return nil
}

// PgAuditRepo

type PgAuditRepo struct {
	pool *pgxpool.Pool
}

func NewPgAuditRepo(pool *pgxpool.Pool) *PgAuditRepo {
	return &PgAuditRepo{pool: pool}
}

func (r *PgAuditRepo) Create(ctx context.Context, entry *domain.AuditEntry) error {
	details, _ := json.Marshal(entry.Details)
	err := r.pool.QueryRow(ctx,
		`INSERT INTO audit_logs (user_id, action, resource_type, resource_id, details, ip_address, user_agent, created_at)
		VALUES ($1, $2, $3, $4, $5, $6::inet, $7, $8) RETURNING id`,
		entry.UserID, entry.Action, entry.ResourceType, entry.ResourceID,
		details, nullableString(entry.IPAddress), entry.UserAgent, entry.CreatedAt,
	).Scan(&entry.ID)
	return err
}

// List serves ALD-001/002/004: filtered audit log retrieval for both the
// paginated listing endpoint (filter.Limit = page size + 1 lookahead) and
// the export endpoint (filter.Limit = 0, meaning unlimited). Pagination
// follows the same (created_at, id) DESC keyset convention as the other
// List*Cursor repository methods (e.g. PgCustomerRepo.ListByCursor).
func (r *PgAuditRepo) List(ctx context.Context, filter domain.AuditListFilter) ([]domain.AuditEntry, error) {
	query := `SELECT id, user_id, action, resource_type, resource_id, details, ip_address, user_agent, created_at FROM audit_logs`
	var args []any
	conditions := []string{"purge_marked_at IS NULL"}
	argIdx := 1

	if filter.ResourceType != "" {
		conditions = append(conditions, fmt.Sprintf("resource_type = $%d", argIdx))
		args = append(args, filter.ResourceType)
		argIdx++
	}
	if filter.ResourceID != "" {
		conditions = append(conditions, fmt.Sprintf("resource_id = $%d", argIdx))
		args = append(args, filter.ResourceID)
		argIdx++
	}
	if filter.UserID != "" {
		conditions = append(conditions, fmt.Sprintf("user_id = $%d", argIdx))
		args = append(args, filter.UserID)
		argIdx++
	}
	if filter.ActionCategory != "" {
		types := domain.ResourceTypesForCategory(filter.ActionCategory)
		if len(types) == 0 {
			// Unrecognized category: no resource_type qualifies, so the
			// result set is empty without needing a round-trip.
			return []domain.AuditEntry{}, nil
		}
		placeholders := make([]string, len(types))
		for i, t := range types {
			placeholders[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, t)
			argIdx++
		}
		conditions = append(conditions, fmt.Sprintf("resource_type IN (%s)", strings.Join(placeholders, ", ")))
	}
	if filter.Since != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, *filter.Since)
		argIdx++
	}
	if filter.Until != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIdx))
		args = append(args, *filter.Until)
		argIdx++
	}
	if filter.Cursor != nil {
		cursorID, err := strconv.ParseInt(filter.Cursor.ID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor id: %w", err)
		}
		conditions = append(conditions, fmt.Sprintf("(created_at, id) < ($%d, $%d)", argIdx, argIdx+1))
		args = append(args, filter.Cursor.CreatedAt, cursorID)
		argIdx += 2
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY created_at DESC, id DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, filter.Limit)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []domain.AuditEntry
	for rows.Next() {
		var e domain.AuditEntry
		var details []byte
		var ipAddr *string
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.Action, &e.ResourceType, &e.ResourceID,
			&details, &ipAddr, &e.UserAgent, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		if len(details) > 0 {
			json.Unmarshal(details, &e.Details)
		}
		if ipAddr != nil {
			e.IPAddress = *ipAddr
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// PgCaseRepo

type PgCaseRepo struct {
	pool *pgxpool.Pool
}

func NewPgCaseRepo(pool *pgxpool.Pool) *PgCaseRepo {
	return &PgCaseRepo{pool: pool}
}

func (r *PgCaseRepo) Get(ctx context.Context, id string) (*domain.Case, error) {
	var c domain.Case
	err := r.pool.QueryRow(ctx,
		`SELECT id, customer_id, alert_ids, status, priority, assigned_to, summary, reopen_reason, related_case_ids, created_at, updated_at, closed_at
		FROM cases WHERE id = $1 AND purge_marked_at IS NULL`, id,
	).Scan(&c.ID, &c.CustomerID, &c.AlertIDs, &c.Status, &c.Priority, &c.AssignedTo, &c.Summary, &c.ReopenReason, &c.RelatedCaseIDs, &c.CreatedAt, &c.UpdatedAt, &c.ClosedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "case", ID: id}
		}
		return nil, err
	}

	notes, err := r.getNotes(ctx, id)
	if err != nil {
		return nil, err
	}
	c.Notes = notes
	return &c, nil
}

func (r *PgCaseRepo) ListByCustomer(ctx context.Context, customerID string) ([]domain.Case, error) {
	return r.listCases(ctx,
		`SELECT id, customer_id, alert_ids, status, priority, assigned_to, summary, reopen_reason, related_case_ids, created_at, updated_at, closed_at
		FROM cases WHERE customer_id = $1 AND purge_marked_at IS NULL ORDER BY created_at DESC`, customerID)
}

func (r *PgCaseRepo) ListOpen(ctx context.Context, limit, offset int) ([]domain.Case, error) {
	return r.listCases(ctx,
		`SELECT id, customer_id, alert_ids, status, priority, assigned_to, summary, reopen_reason, related_case_ids, created_at, updated_at, closed_at
		FROM cases WHERE purge_marked_at IS NULL AND status != 'closed' ORDER BY priority DESC, created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
}

func (r *PgCaseRepo) ListOpenByCursor(ctx context.Context, limit int, after *domain.Cursor) ([]domain.Case, error) {
	const baseQuery = `SELECT id, customer_id, alert_ids, status, priority, assigned_to, summary, reopen_reason, related_case_ids, created_at, updated_at, closed_at
		FROM cases WHERE purge_marked_at IS NULL AND status != 'closed'`

	if after == nil {
		return r.listCases(ctx, baseQuery+` ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	}
	return r.listCases(ctx, baseQuery+` AND (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3`,
		after.CreatedAt, after.ID, limit)
}

func (r *PgCaseRepo) listCases(ctx context.Context, query string, args ...any) ([]domain.Case, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cases []domain.Case
	for rows.Next() {
		var c domain.Case
		if err := rows.Scan(&c.ID, &c.CustomerID, &c.AlertIDs, &c.Status, &c.Priority, &c.AssignedTo, &c.Summary, &c.ReopenReason, &c.RelatedCaseIDs, &c.CreatedAt, &c.UpdatedAt, &c.ClosedAt); err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, rows.Err()
}

func (r *PgCaseRepo) Create(ctx context.Context, c *domain.Case) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO cases (id, customer_id, alert_ids, status, priority, assigned_to, summary, reopen_reason, related_case_ids, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		c.ID, c.CustomerID, c.AlertIDs, string(c.Status), string(c.Priority), c.AssignedTo, c.Summary, c.ReopenReason, c.RelatedCaseIDs, c.CreatedAt, c.UpdatedAt)
	return err
}

func (r *PgCaseRepo) Update(ctx context.Context, c *domain.Case) error {
	c.UpdatedAt = time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE cases SET status=$2, priority=$3, assigned_to=$4, summary=$5, reopen_reason=$6, related_case_ids=$7, updated_at=$8, closed_at=$9 WHERE id=$1`,
		c.ID, string(c.Status), string(c.Priority), c.AssignedTo, c.Summary, c.ReopenReason, c.RelatedCaseIDs, c.UpdatedAt, c.ClosedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "case", ID: c.ID}
	}
	return nil
}

// UpdateIfUnmodified is Update guarded by an optimistic-lock check against
// expectedUpdatedAt (the data model §3.9). Zero rows affected because of a
// stale expectedUpdatedAt (row exists but its updated_at moved on) is
// reported as *domain.ErrConflict; zero rows because the case doesn't exist
// at all is reported as *domain.ErrNotFound.
func (r *PgCaseRepo) UpdateIfUnmodified(ctx context.Context, c *domain.Case, expectedUpdatedAt time.Time) error {
	newUpdatedAt := time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE cases SET status=$2, priority=$3, assigned_to=$4, summary=$5, reopen_reason=$6, related_case_ids=$7, updated_at=$8, closed_at=$9
		WHERE id=$1 AND updated_at=$10`,
		c.ID, string(c.Status), string(c.Priority), c.AssignedTo, c.Summary, c.ReopenReason, c.RelatedCaseIDs, newUpdatedAt, c.ClosedAt, expectedUpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.Get(ctx, c.ID); err != nil {
			return err
		}
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "updated_at mismatch"}
	}
	c.UpdatedAt = newUpdatedAt
	return nil
}

func (r *PgCaseRepo) AddNote(ctx context.Context, caseID string, note *domain.CaseNote) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO case_notes (id, case_id, author, content, created_at) VALUES ($1, $2, $3, $4, $5)`,
		note.ID, caseID, note.Author, note.Content, note.CreatedAt)
	return err
}

func (r *PgCaseRepo) getNotes(ctx context.Context, caseID string) ([]domain.CaseNote, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, author, content, created_at FROM case_notes WHERE case_id = $1 ORDER BY created_at ASC`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []domain.CaseNote
	for rows.Next() {
		var n domain.CaseNote
		if err := rows.Scan(&n.ID, &n.Author, &n.Content, &n.CreatedAt); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// PgAPIKeyRepo

type PgAPIKeyRepo struct {
	pool *pgxpool.Pool
}

func NewPgAPIKeyRepo(pool *pgxpool.Pool) *PgAPIKeyRepo {
	return &PgAPIKeyRepo{pool: pool}
}

func (r *PgAPIKeyRepo) GetByHash(ctx context.Context, keyHash string) (*domain.APIKey, error) {
	var k domain.APIKey
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, key_hash, role, active, created_at, last_used FROM api_keys WHERE key_hash = $1`, keyHash,
	).Scan(&k.ID, &k.Name, &k.KeyHash, &k.Role, &k.Active, &k.CreatedAt, &k.LastUsed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "api_key", ID: keyHash}
		}
		return nil, err
	}
	return &k, nil
}

func (r *PgAPIKeyRepo) Create(ctx context.Context, key *domain.APIKey) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO api_keys (id, name, key_hash, role, active, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		key.ID, key.Name, key.KeyHash, string(key.Role), key.Active, key.CreatedAt)
	return err
}

func (r *PgAPIKeyRepo) List(ctx context.Context) ([]domain.APIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, key_hash, role, active, created_at, last_used FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []domain.APIKey
	for rows.Next() {
		var k domain.APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyHash, &k.Role, &k.Active, &k.CreatedAt, &k.LastUsed); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (r *PgAPIKeyRepo) Revoke(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE api_keys SET active = FALSE WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "api_key", ID: id}
	}
	return nil
}
