package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
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
	pool DBTX
	// encryptor transparently encrypts/decrypts customers.attributes' direct
	// PII fields (the data model §3.1, WS-11 Task 7). Nil disables encryption
	// entirely (encryption not configured), leaving attributes untouched.
	encryptor *crypto.Encryptor
}

func NewPgCustomerRepo(pool DBTX, encryptor *crypto.Encryptor) *PgCustomerRepo {
	return &PgCustomerRepo{pool: pool, encryptor: encryptor}
}

const customerColumns = `id, external_id, customer_type, country_code, status, product_types, attributes, risk_score, risk_tier, last_scored_at, created_at, updated_at, edd_requested_at, edd_stage1_last_sent_at, edd_stage2_notified_at, edd_stage3_notified_at, anonymized_at`

func (r *PgCustomerRepo) Get(ctx context.Context, id string) (*domain.Customer, error) {
	return r.scanCustomer(ctx, `SELECT `+customerColumns+` FROM customers WHERE id = $1 AND purge_marked_at IS NULL`, domain.CanonicalUUID(id))
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
	c.ID = domain.CanonicalUUID(c.ID)

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
	c.ID = domain.CanonicalUUID(c.ID)

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

// DashboardRiskTierCounts keeps dashboard totals independent of the list
// endpoint's page-size limits. PostgreSQL's NULL aggregate bucket is exposed
// as "unscored", matching the memory repository and UI contract.
func (r *PgCustomerRepo) DashboardRiskTierCounts(ctx context.Context) (map[string]int, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT COALESCE(risk_tier::text, 'unscored'), COUNT(*)
		 FROM customers WHERE purge_marked_at IS NULL
		 GROUP BY risk_tier`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var tier string
		var count int64
		if err := rows.Scan(&tier, &count); err != nil {
			return nil, err
		}
		counts[tier] = int(count)
	}
	return counts, rows.Err()
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
		// baseQuery already contains the soft-delete predicate. Appending a
		// second WHERE made every cursor page fail in PostgreSQL.
		rows, err = r.pool.Query(ctx, baseQuery+` AND (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3`,
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

func (r *PgCustomerRepo) ListSearch(ctx context.Context, search string, limit, offset int) ([]domain.Customer, error) {
	pattern := "%" + search + "%"
	rows, err := r.pool.Query(ctx,
		`SELECT `+customerColumns+` FROM customers
		 WHERE purge_marked_at IS NULL
		 AND (id::text ILIKE $1 OR external_id ILIKE $1 OR country_code ILIKE $1 OR attributes->>'name' ILIKE $1)
		 ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		pattern, limit, offset,
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

func (r *PgCustomerRepo) ListByCursorSearch(ctx context.Context, limit int, after *domain.Cursor, search string) ([]domain.Customer, error) {
	pattern := "%" + search + "%"
	baseQuery := `SELECT ` + customerColumns + ` FROM customers
		WHERE purge_marked_at IS NULL
		AND (id::text ILIKE $1 OR external_id ILIKE $1 OR country_code ILIKE $1 OR attributes->>'name' ILIKE $1)`

	var (
		rows pgx.Rows
		err  error
	)
	if after == nil {
		rows, err = r.pool.Query(ctx, baseQuery+` ORDER BY created_at DESC, id DESC LIMIT $2`, pattern, limit)
	} else {
		rows, err = r.pool.Query(ctx, baseQuery+` AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4`,
			pattern, after.CreatedAt, after.ID, limit)
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
	c.ID = domain.CanonicalUUID(c.ID)
	encryptedAttrs, err := encryptDirectPII(r.encryptor, c.Attributes)
	if err != nil {
		return err
	}
	attrs, _ := json.Marshal(encryptedAttrs)
	status := c.Status
	if status == "" {
		status = domain.CustomerStatusActive
	}
	productTypes := c.ProductTypes
	if productTypes == nil {
		productTypes = []string{}
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO customers (id, external_id, customer_type, country_code, status, product_types, attributes, risk_score, risk_tier, last_scored_at, created_at, updated_at, edd_requested_at, edd_stage1_last_sent_at, edd_stage2_notified_at, edd_stage3_notified_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		c.ID, c.ExternalID, c.CustomerType, c.CountryCode, status,
		productTypes, attrs,
		c.RiskScore, riskTierToNullable(c.RiskTier), c.LastScoredAt,
		c.CreatedAt, c.UpdatedAt,
		c.EddRequestedAt, c.EddStage1LastSentAt, c.EddStage2NotifiedAt, c.EddStage3NotifiedAt,
	)
	return err
}

func (r *PgCustomerRepo) Update(ctx context.Context, c *domain.Customer) error {
	c.ID = domain.CanonicalUUID(c.ID)
	encryptedAttrs, err := encryptDirectPII(r.encryptor, c.Attributes)
	if err != nil {
		return err
	}
	attrs, _ := json.Marshal(encryptedAttrs)
	productTypes := c.ProductTypes
	if productTypes == nil {
		productTypes = []string{}
	}
	c.UpdatedAt = time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE customers SET external_id=$2, customer_type=$3, country_code=$4, status=$5, product_types=$6, attributes=$7, risk_score=$8, risk_tier=$9, last_scored_at=$10, updated_at=$11, edd_requested_at=$12, edd_stage1_last_sent_at=$13, edd_stage2_notified_at=$14, edd_stage3_notified_at=$15, anonymized_at=$16 WHERE id=$1`,
		c.ID, c.ExternalID, c.CustomerType, c.CountryCode, c.Status,
		productTypes, attrs,
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
	id = domain.CanonicalUUID(id)
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
	rec.ID = domain.CanonicalUUID(rec.ID)
	rec.CustomerID = domain.CanonicalUUID(rec.CustomerID)
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
	customerID = domain.CanonicalUUID(customerID)
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
		rec.ID = domain.CanonicalUUID(rec.ID)
		rec.CustomerID = domain.CanonicalUUID(rec.CustomerID)
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
	pool DBTX
}

func NewPgTransactionRepo(pool DBTX) *PgTransactionRepo {
	return &PgTransactionRepo{pool: pool}
}

const transactionColumns = "id, customer_id, external_id, amount, currency, direction, counterparty_id, counterparty_country, channel, account_id, counterparty, metadata, idempotency_key, executed_at, created_at"

func scanTransaction(row pgx.Row) (domain.Transaction, error) {
	var t domain.Transaction
	var counterpartyID, counterpartyCountry, channel *string
	var counterpartyJSON, metadataJSON []byte
	err := row.Scan(
		&t.ID, &t.CustomerID, &t.ExternalID, &t.Amount, &t.Currency,
		&t.Direction, &counterpartyID, &counterpartyCountry,
		&channel, &t.AccountID, &counterpartyJSON, &metadataJSON,
		&t.IdempotencyKey,
		&t.ExecutedAt, &t.CreatedAt,
	)
	if err != nil {
		return t, err
	}
	if counterpartyID != nil {
		t.CounterpartyID = *counterpartyID
	}
	if counterpartyCountry != nil {
		t.CounterpartyCountry = *counterpartyCountry
	}
	if channel != nil {
		t.Channel = *channel
	}
	t.ID = domain.CanonicalUUID(t.ID)
	t.CustomerID = domain.CanonicalUUID(t.CustomerID)
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
		`SELECT `+transactionColumns+` FROM transactions WHERE id = $1 AND purge_marked_at IS NULL`, domain.CanonicalUUID(id),
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
	customerID = domain.CanonicalUUID(customerID)
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
	customerID = domain.CanonicalUUID(customerID)
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

func (r *PgTransactionRepo) CountExecutedSince(ctx context.Context, since time.Time) (int, error) {
	var count int64
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions WHERE purge_marked_at IS NULL AND executed_at >= $1`, since,
	).Scan(&count); err != nil {
		return 0, err
	}
	return int(count), nil
}

func (r *PgTransactionRepo) ListByCustomerEventRange(ctx context.Context, customerID string, from, to, createdBefore time.Time, limit int, after *domain.TransactionEventCursor) ([]domain.Transaction, error) {
	customerID = domain.CanonicalUUID(customerID)
	query := `SELECT ` + transactionColumns + ` FROM transactions WHERE customer_id=$1 AND purge_marked_at IS NULL AND executed_at >= $2 AND executed_at < $3 AND created_at <= $4`
	args := []any{customerID, from, to, createdBefore, limit}
	if after != nil {
		query += ` AND (executed_at,id) > ($5,$6)`
		args = []any{customerID, from, to, createdBefore, after.ExecutedAt, after.ID, limit}
	}
	query += ` ORDER BY executed_at ASC, id ASC LIMIT $` + strconv.Itoa(len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Transaction
	for rows.Next() {
		t, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *PgTransactionRepo) Create(ctx context.Context, t *domain.Transaction) error {
	t.ID = domain.CanonicalUUID(t.ID)
	t.CustomerID = domain.CanonicalUUID(t.CustomerID)
	if t.AccountID != nil {
		accountID := domain.CanonicalUUID(*t.AccountID)
		t.AccountID = &accountID
	}
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
	pool DBTX
}

func NewPgAlertRepo(pool DBTX) *PgAlertRepo {
	return &PgAlertRepo{pool: pool}
}

// alertColumns includes suppressed/suppression_reason (WL-004, additive
// columns from migrations/010_whitelist.sql) and aggregation_window_start/
// batch_run_id/batch_reviewed_at (WS-5 Task4, migrations/012_alert_dedup.sql).
const alertColumns = "id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, resolved_at, resolved_by, created_at, updated_at, suppressed, suppression_reason, aggregation_window_start, batch_run_id, batch_reviewed_at, assigned_to, assigned_team, due_at, disposition, disposition_rationale"

const alertRiskRankSQL = `CASE severity::text
	WHEN 'critical' THEN 4
	WHEN 'high' THEN 3
	WHEN 'medium' THEN 2
	WHEN 'low' THEN 1
	ELSE 0 END`

func scanAlertRow(row interface {
	Scan(dest ...any) error
}, a *domain.Alert) error {
	var score *float64
	var description *string
	var suppressionReason *string
	var batchRunID *string
	var resolvedBy *string
	var assignedTo, assignedTeam, disposition, dispositionRationale *string
	if err := row.Scan(
		&a.ID, &a.CustomerID, &a.ScenarioID,
		&a.Severity, &a.Status, &score, &description,
		&a.TransactionIDs,
		&a.DetectedAt, &a.ResolvedAt, &resolvedBy,
		&a.CreatedAt, &a.UpdatedAt,
		&a.Suppressed, &suppressionReason,
		&a.AggregationWindowStart, &batchRunID, &a.BatchReviewedAt,
		&assignedTo, &assignedTeam, &a.DueAt, &disposition, &dispositionRationale,
	); err != nil {
		return err
	}
	if score != nil {
		a.Score = *score
	}
	if description != nil {
		a.Description = *description
	}
	if suppressionReason != nil {
		a.SuppressionReason = *suppressionReason
	}
	if resolvedBy != nil {
		a.ResolvedBy = *resolvedBy
	}
	if batchRunID != nil {
		a.BatchRunID = *batchRunID
	}
	if assignedTo != nil {
		a.AssignedTo = *assignedTo
	}
	if assignedTeam != nil {
		a.AssignedTeam = *assignedTeam
	}
	if disposition != nil {
		a.Disposition = *disposition
	}
	if dispositionRationale != nil {
		a.DispositionRationale = *dispositionRationale
	}
	a.ID = domain.CanonicalUUID(a.ID)
	a.CustomerID = domain.CanonicalUUID(a.CustomerID)
	for i := range a.TransactionIDs {
		a.TransactionIDs[i] = domain.CanonicalUUID(a.TransactionIDs[i])
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
	id = domain.CanonicalUUID(id)
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
	customerID = domain.CanonicalUUID(customerID)
	return r.listAlerts(ctx,
		`SELECT `+alertColumns+`
		FROM alerts WHERE customer_id = $1 AND purge_marked_at IS NULL ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		customerID, limit, offset,
	)
}

func (r *PgAlertRepo) ListByCustomerRisk(ctx context.Context, customerID string, limit, offset int) ([]domain.Alert, error) {
	customerID = domain.CanonicalUUID(customerID)
	return r.listAlerts(ctx,
		`SELECT `+alertColumns+`
		FROM alerts WHERE customer_id = $1 AND purge_marked_at IS NULL ORDER BY `+alertRiskRankSQL+` DESC, created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		customerID, limit, offset,
	)
}

func (r *PgAlertRepo) ListOpen(ctx context.Context, limit, offset int) ([]domain.Alert, error) {
	return r.listAlerts(ctx,
		`SELECT `+alertColumns+`
		FROM alerts WHERE purge_marked_at IS NULL AND status IN ('open', 'investigating', 'escalated') ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
}

func (r *PgAlertRepo) ListOpenByRisk(ctx context.Context, limit, offset int) ([]domain.Alert, error) {
	return r.listAlerts(ctx,
		`SELECT `+alertColumns+`
		FROM alerts WHERE purge_marked_at IS NULL AND status IN ('open', 'investigating', 'escalated') ORDER BY `+alertRiskRankSQL+` DESC, created_at DESC, id DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
}

func (r *PgAlertRepo) ListByCustomerCursor(ctx context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.Alert, error) {
	customerID = domain.CanonicalUUID(customerID)
	baseQuery := `SELECT ` + alertColumns + `
		FROM alerts WHERE customer_id = $1 AND purge_marked_at IS NULL`

	if after == nil {
		return r.listAlerts(ctx, baseQuery+` ORDER BY created_at DESC, id DESC LIMIT $2`, customerID, limit)
	}
	return r.listAlerts(ctx, baseQuery+` AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4`,
		customerID, after.CreatedAt, after.ID, limit)
}

func (r *PgAlertRepo) ListByCustomerRiskCursor(ctx context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.Alert, error) {
	customerID = domain.CanonicalUUID(customerID)
	baseQuery := `SELECT ` + alertColumns + `
		FROM alerts WHERE customer_id = $1 AND purge_marked_at IS NULL`
	if after == nil {
		return r.listAlerts(ctx, baseQuery+` ORDER BY `+alertRiskRankSQL+` DESC, created_at DESC, id DESC LIMIT $2`, customerID, limit)
	}
	return r.listAlerts(ctx, baseQuery+` AND (`+alertRiskRankSQL+`, created_at, id) < ($2, $3, $4) ORDER BY `+alertRiskRankSQL+` DESC, created_at DESC, id DESC LIMIT $5`,
		customerID, after.Rank, after.CreatedAt, after.ID, limit)
}

func (r *PgAlertRepo) ListOpenByCursor(ctx context.Context, limit int, after *domain.Cursor) ([]domain.Alert, error) {
	baseQuery := `SELECT ` + alertColumns + `
		FROM alerts WHERE purge_marked_at IS NULL AND status IN ('open', 'investigating', 'escalated')`

	if after == nil {
		return r.listAlerts(ctx, baseQuery+` ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	}
	return r.listAlerts(ctx, baseQuery+` AND (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3`,
		after.CreatedAt, after.ID, limit)
}

func (r *PgAlertRepo) ListOpenByRiskCursor(ctx context.Context, limit int, after *domain.Cursor) ([]domain.Alert, error) {
	baseQuery := `SELECT ` + alertColumns + `
		FROM alerts WHERE purge_marked_at IS NULL AND status IN ('open', 'investigating', 'escalated')`
	if after == nil {
		return r.listAlerts(ctx, baseQuery+` ORDER BY `+alertRiskRankSQL+` DESC, created_at DESC, id DESC LIMIT $1`, limit)
	}
	return r.listAlerts(ctx, baseQuery+` AND (`+alertRiskRankSQL+`, created_at, id) < ($1, $2, $3) ORDER BY `+alertRiskRankSQL+` DESC, created_at DESC, id DESC LIMIT $4`,
		after.Rank, after.CreatedAt, after.ID, limit)
}

func (r *PgAlertRepo) DashboardUnresolvedCounts(ctx context.Context) (map[string]int, map[string]int, error) {
	byStatus := make(map[string]int)
	statusRows, err := r.pool.Query(ctx,
		`SELECT status::text, COUNT(*)
		 FROM alerts
		 WHERE purge_marked_at IS NULL AND status IN ('open', 'investigating', 'escalated')
		 GROUP BY status`)
	if err != nil {
		return nil, nil, err
	}
	for statusRows.Next() {
		var status string
		var count int64
		if err := statusRows.Scan(&status, &count); err != nil {
			statusRows.Close()
			return nil, nil, err
		}
		byStatus[status] = int(count)
	}
	if err := statusRows.Err(); err != nil {
		statusRows.Close()
		return nil, nil, err
	}
	statusRows.Close()

	bySeverity := make(map[string]int)
	severityRows, err := r.pool.Query(ctx,
		`SELECT severity::text, COUNT(*)
		 FROM alerts
		 WHERE purge_marked_at IS NULL AND status IN ('open', 'investigating', 'escalated')
		 GROUP BY severity`)
	if err != nil {
		return nil, nil, err
	}
	defer severityRows.Close()
	for severityRows.Next() {
		var severity string
		var count int64
		if err := severityRows.Scan(&severity, &count); err != nil {
			return nil, nil, err
		}
		bySeverity[severity] = int(count)
	}
	return byStatus, bySeverity, severityRows.Err()
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
	a.ID = domain.CanonicalUUID(a.ID)
	a.CustomerID = domain.CanonicalUUID(a.CustomerID)
	for i := range a.TransactionIDs {
		a.TransactionIDs[i] = domain.CanonicalUUID(a.TransactionIDs[i])
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO alerts (id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, created_at, updated_at, suppressed, suppression_reason, aggregation_window_start, batch_run_id, assigned_to, assigned_team, due_at, disposition, disposition_rationale)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)`,
		a.ID, a.CustomerID, a.ScenarioID,
		string(a.Severity), string(a.Status), a.Score, a.Description,
		a.TransactionIDs,
		a.DetectedAt, a.CreatedAt, a.UpdatedAt,
		a.Suppressed, nullableString(a.SuppressionReason),
		a.AggregationWindowStart, nullableString(a.BatchRunID),
		nullableString(a.AssignedTo), nullableString(a.AssignedTeam), a.DueAt, nullableString(a.Disposition), a.DispositionRationale,
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
	customerID = domain.CanonicalUUID(customerID)
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
	alertID = domain.CanonicalUUID(alertID)
	batchRunID = domain.CanonicalUUID(batchRunID)
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
	id = domain.CanonicalUUID(id)
	if domain.IsAlertTerminal(status) && strings.TrimSpace(resolvedBy) == "" {
		return fmt.Errorf("resolved_by is required for terminal alert status")
	}
	now := time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE alerts SET status=$2::alert_status,
			resolved_by=CASE WHEN $2::alert_status IN ('closed_true_positive', 'closed_false_positive') THEN NULLIF($3, '') ELSE NULL END,
			resolved_at=CASE WHEN $2::alert_status IN ('closed_true_positive', 'closed_false_positive') THEN $4 ELSE NULL END,
			disposition=CASE WHEN $2::alert_status = 'investigating' THEN NULL ELSE disposition END,
			disposition_rationale=CASE WHEN $2::alert_status = 'investigating' THEN '' ELSE disposition_rationale END,
			updated_at=$4
		 WHERE id=$1
		   AND ((status = 'open' AND $2::alert_status IN ('investigating', 'escalated'))
		    OR (status IN ('investigating', 'escalated') AND $2::alert_status IN ('investigating', 'escalated', 'closed_true_positive', 'closed_false_positive'))
		    OR (status IN ('closed_true_positive', 'closed_false_positive') AND $2::alert_status = 'investigating'))`,
		id, string(status), resolvedBy, now,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return r.alertStatusUpdateFailure(ctx, id, status)
	}
	return nil
}

// UpdateStatusIfUnmodified is UpdateStatus guarded by an optimistic-lock
// check against expectedUpdatedAt (the data model §3.9). Zero rows affected
// because of a stale expectedUpdatedAt (row exists but its updated_at
// moved on) is reported as *domain.ErrConflict; zero rows because the
// alert doesn't exist at all is reported as *domain.ErrNotFound.
func (r *PgAlertRepo) UpdateStatusIfUnmodified(ctx context.Context, id string, status domain.AlertStatus, resolvedBy string, expectedUpdatedAt time.Time) error {
	id = domain.CanonicalUUID(id)
	if domain.IsAlertTerminal(status) && strings.TrimSpace(resolvedBy) == "" {
		return fmt.Errorf("resolved_by is required for terminal alert status")
	}
	now := time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE alerts SET status=$2::alert_status,
			resolved_by=CASE WHEN $2::alert_status IN ('closed_true_positive', 'closed_false_positive') THEN NULLIF($3, '') ELSE NULL END,
			resolved_at=CASE WHEN $2::alert_status IN ('closed_true_positive', 'closed_false_positive') THEN $4 ELSE NULL END,
			disposition=CASE WHEN $2::alert_status = 'investigating' THEN NULL ELSE disposition END,
			disposition_rationale=CASE WHEN $2::alert_status = 'investigating' THEN '' ELSE disposition_rationale END,
			updated_at=$4
		 WHERE id=$1 AND updated_at=$5
		   AND ((status = 'open' AND $2::alert_status IN ('investigating', 'escalated'))
		    OR (status IN ('investigating', 'escalated') AND $2::alert_status IN ('investigating', 'escalated', 'closed_true_positive', 'closed_false_positive')) OR
		    (status IN ('closed_true_positive', 'closed_false_positive') AND $2::alert_status = 'investigating'))`,
		id, string(status), resolvedBy, now, expectedUpdatedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		current, getErr := r.Get(ctx, id)
		if getErr != nil {
			return getErr
		}
		if !current.UpdatedAt.Equal(expectedUpdatedAt) {
			return &domain.ErrConflict{Entity: "alert", ID: id, Reason: "updated_at mismatch"}
		}
		return invalidAlertStatusTransition(current, status)
	}
	return nil
}

// UpdateStatusWithRationale is the disposition path used by the Wave 2
// operator workflow. The status transition and its rationale are committed
// together so a successful response can never be missing the decision reason.
func (r *PgAlertRepo) UpdateStatusWithRationale(ctx context.Context, id string, status domain.AlertStatus, resolvedBy, rationale string, expectedUpdatedAt *time.Time) error {
	id = domain.CanonicalUUID(id)
	if domain.IsAlertTerminal(status) && (strings.TrimSpace(resolvedBy) == "" || strings.TrimSpace(rationale) == "") {
		return fmt.Errorf("resolved_by and rationale are required for terminal alert status")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	query := `UPDATE alerts SET status=$2::alert_status,
		resolved_by=CASE WHEN $2::alert_status IN ('closed_true_positive', 'closed_false_positive') THEN NULLIF($3, '') ELSE NULL END,
		resolved_at=CASE WHEN $2::alert_status IN ('closed_true_positive', 'closed_false_positive') THEN $4 ELSE NULL END,
		disposition=CASE WHEN $2::alert_status IN ('closed_true_positive', 'closed_false_positive') THEN $5 WHEN $2::alert_status = 'investigating' THEN NULL ELSE disposition END,
		disposition_rationale=CASE WHEN $2::alert_status IN ('closed_true_positive', 'closed_false_positive') THEN $6 WHEN $2::alert_status = 'investigating' THEN '' ELSE disposition_rationale END,
		updated_at=$4 WHERE id=$1`
	args := []any{id, string(status), resolvedBy, now, string(status), strings.TrimSpace(rationale)}
	if expectedUpdatedAt != nil {
		query += ` AND updated_at=$7`
		args = append(args, *expectedUpdatedAt)
	}
	query += ` AND ((status = 'open' AND $2::alert_status IN ('investigating', 'escalated')) OR
		(status IN ('investigating', 'escalated') AND $2::alert_status IN ('investigating', 'escalated', 'closed_true_positive', 'closed_false_positive')) OR
		(status IN ('closed_true_positive', 'closed_false_positive') AND $2::alert_status = 'investigating'))`
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		current, getErr := r.Get(ctx, id)
		if getErr != nil {
			return getErr
		}
		if expectedUpdatedAt != nil && !current.UpdatedAt.Equal(*expectedUpdatedAt) {
			return &domain.ErrConflict{Entity: "alert", ID: id, Reason: "updated_at mismatch"}
		}
		return invalidAlertStatusTransition(current, status)
	}
	return nil
}

func (r *PgAlertRepo) CloseFalsePositiveWithRationale(ctx context.Context, id, resolvedBy, rationale string, expectedUpdatedAt *time.Time) error {
	id = domain.CanonicalUUID(id)
	if strings.TrimSpace(resolvedBy) == "" || strings.TrimSpace(rationale) == "" {
		return fmt.Errorf("resolved_by and rationale are required for bulk false-positive close")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	query := `UPDATE alerts SET status='closed_false_positive', resolved_by=$2, resolved_at=$3,
		disposition='closed_false_positive', disposition_rationale=$4, updated_at=$3
		WHERE id=$1 AND purge_marked_at IS NULL AND status IN ('open', 'investigating', 'escalated')`
	args := []any{id, strings.TrimSpace(resolvedBy), now, strings.TrimSpace(rationale)}
	if expectedUpdatedAt != nil {
		query += ` AND updated_at=$5`
		args = append(args, *expectedUpdatedAt)
	}
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		current, getErr := r.Get(ctx, id)
		if getErr != nil {
			return getErr
		}
		if expectedUpdatedAt != nil && !current.UpdatedAt.Equal(*expectedUpdatedAt) {
			return &domain.ErrConflict{Entity: "alert", ID: id, Reason: "updated_at mismatch"}
		}
		return invalidAlertStatusTransition(current, domain.AlertStatusClosedFalsePositive)
	}
	return nil
}

func (r *PgAlertRepo) CloseFalsePositive(ctx context.Context, id string, resolvedBy string) error {
	id = domain.CanonicalUUID(id)
	if strings.TrimSpace(resolvedBy) == "" {
		return fmt.Errorf("resolved_by is required for terminal alert status")
	}
	now := time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE alerts
		 SET status='closed_false_positive', resolved_by=$2, resolved_at=$3, updated_at=$3
		 WHERE id=$1 AND purge_marked_at IS NULL AND status IN ('open', 'investigating', 'escalated')`,
		id, resolvedBy, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		current, getErr := r.Get(ctx, id)
		if getErr != nil {
			return getErr
		}
		return &domain.ErrInvalidStateTransition{Entity: "alert", ID: id, From: string(current.Status), To: string(domain.AlertStatusClosedFalsePositive)}
	}
	return nil
}

func (r *PgAlertRepo) alertStatusUpdateFailure(ctx context.Context, id string, status domain.AlertStatus) error {
	current, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	return invalidAlertStatusTransition(current, status)
}

func invalidAlertStatusTransition(current *domain.Alert, status domain.AlertStatus) error {
	if !domain.ValidAlertStatusTransition(current.Status, status) {
		return &domain.ErrInvalidStateTransition{Entity: "alert", ID: current.ID, From: string(current.Status), To: string(status)}
	}
	return &domain.ErrConflict{Entity: "alert", ID: current.ID, Reason: "status changed concurrently"}
}

func (r *PgAlertRepo) EscalateSeverity(ctx context.Context, id string, severity domain.AlertSeverity) error {
	id = domain.CanonicalUUID(id)
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
	pool DBTX
}

func NewPgAuditRepo(pool DBTX) *PgAuditRepo {
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
		// These columns were nullable in the original audit schema.  In
		// particular, migration-repair rows may have no actor, target, JSON
		// details, or user-agent.  Scan through nullable holders so a legacy
		// row remains readable instead of turning GET /audit into a 500.
		var userID, resourceID, userAgent *string
		var details *[]byte
		// PostgreSQL's INET codec does not scan into *string in pgx v5.
		// Use a nullable *netip.Addr so both IPv4/IPv6 and SQL NULL are
		// decoded without turning a valid audit row into a 500 response.
		var ipAddr *netip.Addr
		if err := rows.Scan(
			&e.ID, &userID, &e.Action, &e.ResourceType, &resourceID,
			&details, &ipAddr, &userAgent, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		if userID != nil {
			e.UserID = *userID
		}
		if resourceID != nil {
			e.ResourceID = *resourceID
		}
		if userAgent != nil {
			e.UserAgent = *userAgent
		}
		if details != nil && len(*details) > 0 {
			json.Unmarshal(*details, &e.Details)
		}
		if ipAddr != nil {
			e.IPAddress = ipAddr.String()
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

// nonNilStrings coalesces a nil slice to an empty (non-nil) one. pgx encodes
// a nil []string as SQL NULL, which violates the alert_ids/related_case_ids
// NOT NULL DEFAULT '{}' columns on cases (migrations/004, 014) whenever a
// caller builds a domain.Case without explicitly initializing those fields
// (e.g. handleCreateCase never sets RelatedCaseIDs, and demogen's generated
// cases never set it either — a DEFAULT only applies when the column is
// omitted from the INSERT, not when NULL is bound explicitly). A non-nil
// empty slice round-trips through pgx as an empty Postgres array instead.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// PgCaseRepo

type PgCaseRepo struct {
	pool DBTX
}

const caseColumns = "id, customer_id, alert_ids, status, priority, COALESCE(assigned_to, ''), COALESCE(assigned_team, ''), due_at, summary, reopen_reason, related_case_ids, investigation_disposition, str_candidate, disposition_rationale, COALESCE(str_report_id, ''), str_filed_at, COALESCE(str_filed_by, ''), COALESCE(str_filing_channel, ''), COALESCE(str_destination, ''), COALESCE(str_external_reference, ''), created_at, updated_at, closed_at"

func caseScanArgs(c *domain.Case) []any {
	return []any{&c.ID, &c.CustomerID, &c.AlertIDs, &c.Status, &c.Priority, &c.AssignedTo, &c.AssignedTeam, &c.DueAt,
		&c.Summary, &c.ReopenReason, &c.RelatedCaseIDs, &c.InvestigationDisposition, &c.STRCandidate, &c.DispositionRationale,
		&c.STRReportID, &c.STRFiledAt, &c.STRFiledBy, &c.STRFilingChannel, &c.STRDestination, &c.STRExternalReference,
		&c.CreatedAt, &c.UpdatedAt, &c.ClosedAt}
}

func normalizeCaseIdentifiers(c *domain.Case) {
	if c == nil {
		return
	}
	c.ID = domain.CanonicalIdentifier(c.ID)
	c.CustomerID = domain.CanonicalUUID(c.CustomerID)
	for i := range c.AlertIDs {
		c.AlertIDs[i] = domain.CanonicalUUID(c.AlertIDs[i])
	}
	for i := range c.RelatedCaseIDs {
		c.RelatedCaseIDs[i] = domain.CanonicalIdentifier(c.RelatedCaseIDs[i])
	}
	c.STRReportID = domain.CanonicalIdentifier(c.STRReportID)
}

func NewPgCaseRepo(pool DBTX) *PgCaseRepo {
	return &PgCaseRepo{pool: pool}
}

func (r *PgCaseRepo) Get(ctx context.Context, id string) (*domain.Case, error) {
	id = domain.CanonicalIdentifier(id)
	var c domain.Case
	err := r.pool.QueryRow(ctx,
		`SELECT `+caseColumns+` FROM cases WHERE id = $1 AND purge_marked_at IS NULL`, id,
	).Scan(caseScanArgs(&c)...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "case", ID: id}
		}
		return nil, err
	}
	normalizeCaseIdentifiers(&c)

	notes, err := r.getNotes(ctx, id)
	if err != nil {
		return nil, err
	}
	c.Notes = notes
	return &c, nil
}

func (r *PgCaseRepo) ListByCustomer(ctx context.Context, customerID string) ([]domain.Case, error) {
	customerID = domain.CanonicalUUID(customerID)
	return r.listCases(ctx,
		`SELECT `+caseColumns+` FROM cases WHERE customer_id = $1 AND purge_marked_at IS NULL ORDER BY created_at DESC, id DESC`, customerID)
}

func (r *PgCaseRepo) ListByCustomerOffset(ctx context.Context, customerID string, limit, offset int) ([]domain.Case, error) {
	customerID = domain.CanonicalUUID(customerID)
	return r.listCases(ctx,
		`SELECT `+caseColumns+` FROM cases WHERE customer_id = $1 AND purge_marked_at IS NULL ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, customerID, limit, offset)
}

const caseRiskRankSQL = `CASE priority
	WHEN 'critical' THEN 4
	WHEN 'high' THEN 3
	WHEN 'medium' THEN 2
	WHEN 'low' THEN 1
	ELSE 0 END`

func (r *PgCaseRepo) ListByCustomerRiskOffset(ctx context.Context, customerID string, limit, offset int) ([]domain.Case, error) {
	customerID = domain.CanonicalUUID(customerID)
	return r.listCases(ctx,
		`SELECT `+caseColumns+` FROM cases WHERE customer_id = $1 AND purge_marked_at IS NULL ORDER BY `+caseRiskRankSQL+` DESC, created_at DESC, id DESC LIMIT $2 OFFSET $3`, customerID, limit, offset)
}

func (r *PgCaseRepo) ListByCustomerCursor(ctx context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.Case, error) {
	customerID = domain.CanonicalUUID(customerID)
	const baseQuery = `SELECT ` + caseColumns + ` FROM cases WHERE customer_id = $1 AND purge_marked_at IS NULL`

	if after == nil {
		return r.listCases(ctx, baseQuery+` ORDER BY created_at DESC, id DESC LIMIT $2`, customerID, limit)
	}
	return r.listCases(ctx, baseQuery+` AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4`,
		customerID, after.CreatedAt, after.ID, limit)
}

func (r *PgCaseRepo) ListByCustomerRiskCursor(ctx context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.Case, error) {
	customerID = domain.CanonicalUUID(customerID)
	baseQuery := `SELECT ` + caseColumns + ` FROM cases WHERE customer_id = $1 AND purge_marked_at IS NULL`
	if after == nil {
		return r.listCases(ctx, baseQuery+` ORDER BY `+caseRiskRankSQL+` DESC, created_at DESC, id DESC LIMIT $2`, customerID, limit)
	}
	return r.listCases(ctx, baseQuery+` AND (`+caseRiskRankSQL+`, created_at, id) < ($2, $3, $4) ORDER BY `+caseRiskRankSQL+` DESC, created_at DESC, id DESC LIMIT $5`,
		customerID, after.Rank, after.CreatedAt, after.ID, limit)
}

func (r *PgCaseRepo) ListOpen(ctx context.Context, limit, offset int) ([]domain.Case, error) {
	return r.listCases(ctx,
		`SELECT `+caseColumns+` FROM cases WHERE purge_marked_at IS NULL AND status IN ('open', 'new', 'investigating', 'escalated', 'reopened') ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`, limit, offset)
}

func (r *PgCaseRepo) ListOpenByRisk(ctx context.Context, limit, offset int) ([]domain.Case, error) {
	return r.listCases(ctx,
		`SELECT `+caseColumns+` FROM cases WHERE purge_marked_at IS NULL AND status IN ('open', 'new', 'investigating', 'escalated', 'reopened') ORDER BY `+caseRiskRankSQL+` DESC, created_at DESC, id DESC LIMIT $1 OFFSET $2`, limit, offset)
}

func (r *PgCaseRepo) ListOpenByCursor(ctx context.Context, limit int, after *domain.Cursor) ([]domain.Case, error) {
	const baseQuery = `SELECT ` + caseColumns + ` FROM cases WHERE purge_marked_at IS NULL AND status IN ('open', 'new', 'investigating', 'escalated', 'reopened')`

	if after == nil {
		return r.listCases(ctx, baseQuery+` ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	}
	return r.listCases(ctx, baseQuery+` AND (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3`,
		after.CreatedAt, after.ID, limit)
}

func (r *PgCaseRepo) ListOpenByRiskCursor(ctx context.Context, limit int, after *domain.Cursor) ([]domain.Case, error) {
	baseQuery := `SELECT ` + caseColumns + ` FROM cases WHERE purge_marked_at IS NULL AND status IN ('open', 'new', 'investigating', 'escalated', 'reopened')`
	if after == nil {
		return r.listCases(ctx, baseQuery+` ORDER BY `+caseRiskRankSQL+` DESC, created_at DESC, id DESC LIMIT $1`, limit)
	}
	return r.listCases(ctx, baseQuery+` AND (`+caseRiskRankSQL+`, created_at, id) < ($1, $2, $3) ORDER BY `+caseRiskRankSQL+` DESC, created_at DESC, id DESC LIMIT $4`,
		after.Rank, after.CreatedAt, after.ID, limit)
}

func (r *PgCaseRepo) DashboardUnresolvedCounts(ctx context.Context) (map[string]int, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT status, COUNT(*)
		 FROM cases
		 WHERE purge_marked_at IS NULL AND status IN ('open', 'new', 'investigating', 'escalated', 'reopened')
		 GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = int(count)
	}
	return counts, rows.Err()
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
		if err := rows.Scan(caseScanArgs(&c)...); err != nil {
			return nil, err
		}
		normalizeCaseIdentifiers(&c)
		cases = append(cases, c)
	}
	return cases, rows.Err()
}

func (r *PgCaseRepo) Create(ctx context.Context, c *domain.Case) error {
	normalizeCaseIdentifiers(c)
	_, err := r.pool.Exec(ctx,
		`INSERT INTO cases (id, customer_id, alert_ids, status, priority, assigned_to, assigned_team, due_at, summary, reopen_reason, related_case_ids, investigation_disposition, str_candidate, disposition_rationale, str_report_id, str_filed_at, str_filed_by, str_filing_channel, str_destination, str_external_reference, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)`,
		c.ID, c.CustomerID, nonNilStrings(c.AlertIDs), string(c.Status), string(c.Priority), nullableString(c.AssignedTo), nullableString(c.AssignedTeam), c.DueAt, c.Summary, c.ReopenReason, nonNilStrings(c.RelatedCaseIDs), c.InvestigationDisposition, c.STRCandidate, c.DispositionRationale, nullableString(c.STRReportID), c.STRFiledAt, nullableString(c.STRFiledBy), nullableString(c.STRFilingChannel), nullableString(c.STRDestination), nullableString(c.STRExternalReference), c.CreatedAt, c.UpdatedAt)
	return err
}

func (r *PgCaseRepo) Update(ctx context.Context, c *domain.Case) error {
	normalizeCaseIdentifiers(c)
	current, err := r.Get(ctx, c.ID)
	if err != nil {
		return err
	}
	if current.Status != c.Status && !domain.ValidCaseStatusTransition(current.Status, c.Status) {
		return &domain.ErrInvalidStateTransition{Entity: "case", ID: c.ID, From: string(current.Status), To: string(c.Status)}
	}
	c.UpdatedAt = time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE cases SET status=$2, priority=$3, assigned_to=$4, assigned_team=$5, due_at=$6, summary=$7, reopen_reason=$8, related_case_ids=$9, investigation_disposition=$10, str_candidate=$11, disposition_rationale=$12, str_report_id=$13, str_filed_at=$14, str_filed_by=$15, str_filing_channel=$16, str_destination=$17, str_external_reference=$18, updated_at=$19, closed_at=$20 WHERE id=$1`,
		c.ID, string(c.Status), string(c.Priority), nullableString(c.AssignedTo), nullableString(c.AssignedTeam), c.DueAt, c.Summary, c.ReopenReason, nonNilStrings(c.RelatedCaseIDs), c.InvestigationDisposition, c.STRCandidate, c.DispositionRationale, nullableString(c.STRReportID), c.STRFiledAt, nullableString(c.STRFiledBy), nullableString(c.STRFilingChannel), nullableString(c.STRDestination), nullableString(c.STRExternalReference), c.UpdatedAt, c.ClosedAt)
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
	normalizeCaseIdentifiers(c)
	current, err := r.Get(ctx, c.ID)
	if err != nil {
		return err
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "updated_at mismatch"}
	}
	if current.Status != c.Status && !domain.ValidCaseStatusTransition(current.Status, c.Status) {
		return &domain.ErrInvalidStateTransition{Entity: "case", ID: c.ID, From: string(current.Status), To: string(c.Status)}
	}
	newUpdatedAt := time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE cases SET status=$2, priority=$3, assigned_to=$4, assigned_team=$5, due_at=$6, summary=$7, reopen_reason=$8, related_case_ids=$9, investigation_disposition=$10, str_candidate=$11, disposition_rationale=$12, str_report_id=$13, str_filed_at=$14, str_filed_by=$15, str_filing_channel=$16, str_destination=$17, str_external_reference=$18, updated_at=$19, closed_at=$20
		WHERE id=$1 AND updated_at=$21`,
		c.ID, string(c.Status), string(c.Priority), nullableString(c.AssignedTo), nullableString(c.AssignedTeam), c.DueAt, c.Summary, c.ReopenReason, nonNilStrings(c.RelatedCaseIDs), c.InvestigationDisposition, c.STRCandidate, c.DispositionRationale, nullableString(c.STRReportID), c.STRFiledAt, nullableString(c.STRFiledBy), nullableString(c.STRFilingChannel), nullableString(c.STRDestination), nullableString(c.STRExternalReference), newUpdatedAt, c.ClosedAt, expectedUpdatedAt)
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
