package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/merlon-aml/merlon/api/internal/domain"
)

type PgCustomerRepo struct {
	pool *pgxpool.Pool
}

func NewPgCustomerRepo(pool *pgxpool.Pool) *PgCustomerRepo {
	return &PgCustomerRepo{pool: pool}
}

func (r *PgCustomerRepo) Get(ctx context.Context, id string) (*domain.Customer, error) {
	return r.scanCustomer(ctx, `SELECT id, external_id, customer_type, country_code, product_types, attributes, risk_score, risk_tier, last_scored_at, created_at, updated_at FROM customers WHERE id = $1`, id)
}

func (r *PgCustomerRepo) GetByExternalID(ctx context.Context, externalID string) (*domain.Customer, error) {
	return r.scanCustomer(ctx, `SELECT id, external_id, customer_type, country_code, product_types, attributes, risk_score, risk_tier, last_scored_at, created_at, updated_at FROM customers WHERE external_id = $1`, externalID)
}

func (r *PgCustomerRepo) scanCustomer(ctx context.Context, query string, arg any) (*domain.Customer, error) {
	var c domain.Customer
	var attrs []byte
	var products []string
	var riskTier *string

	err := r.pool.QueryRow(ctx, query, arg).Scan(
		&c.ID, &c.ExternalID, &c.CustomerType, &c.CountryCode,
		&products, &attrs,
		&c.RiskScore, &riskTier, &c.LastScoredAt,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "customer", ID: fmt.Sprintf("%v", arg)}
		}
		return nil, err
	}

	c.ProductTypes = products
	c.Attributes = make(map[string]string)
	if len(attrs) > 0 {
		json.Unmarshal(attrs, &c.Attributes)
	}
	if riskTier != nil {
		rt := domain.RiskTier(*riskTier)
		c.RiskTier = &rt
	}
	return &c, nil
}

func (r *PgCustomerRepo) List(ctx context.Context, limit, offset int) ([]domain.Customer, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, external_id, customer_type, country_code, product_types, attributes, risk_score, risk_tier, last_scored_at, created_at, updated_at FROM customers ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var customers []domain.Customer
	for rows.Next() {
		var c domain.Customer
		var attrs []byte
		var products []string
		var riskTier *string

		if err := rows.Scan(
			&c.ID, &c.ExternalID, &c.CustomerType, &c.CountryCode,
			&products, &attrs,
			&c.RiskScore, &riskTier, &c.LastScoredAt,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}

		c.ProductTypes = products
		c.Attributes = make(map[string]string)
		if len(attrs) > 0 {
			json.Unmarshal(attrs, &c.Attributes)
		}
		if riskTier != nil {
			rt := domain.RiskTier(*riskTier)
			c.RiskTier = &rt
		}
		customers = append(customers, c)
	}
	return customers, rows.Err()
}

func (r *PgCustomerRepo) ListByCursor(ctx context.Context, limit int, after *domain.Cursor) ([]domain.Customer, error) {
	const baseQuery = `SELECT id, external_id, customer_type, country_code, product_types, attributes, risk_score, risk_tier, last_scored_at, created_at, updated_at FROM customers`

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
		var c domain.Customer
		var attrs []byte
		var products []string
		var riskTier *string

		if err := rows.Scan(
			&c.ID, &c.ExternalID, &c.CustomerType, &c.CountryCode,
			&products, &attrs,
			&c.RiskScore, &riskTier, &c.LastScoredAt,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}

		c.ProductTypes = products
		c.Attributes = make(map[string]string)
		if len(attrs) > 0 {
			json.Unmarshal(attrs, &c.Attributes)
		}
		if riskTier != nil {
			rt := domain.RiskTier(*riskTier)
			c.RiskTier = &rt
		}
		customers = append(customers, c)
	}
	return customers, rows.Err()
}

func (r *PgCustomerRepo) Create(ctx context.Context, c *domain.Customer) error {
	attrs, _ := json.Marshal(c.Attributes)
	_, err := r.pool.Exec(ctx,
		`INSERT INTO customers (id, external_id, customer_type, country_code, product_types, attributes, risk_score, risk_tier, last_scored_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		c.ID, c.ExternalID, c.CustomerType, c.CountryCode,
		c.ProductTypes, attrs,
		c.RiskScore, riskTierToNullable(c.RiskTier), c.LastScoredAt,
		c.CreatedAt, c.UpdatedAt,
	)
	return err
}

func (r *PgCustomerRepo) Update(ctx context.Context, c *domain.Customer) error {
	attrs, _ := json.Marshal(c.Attributes)
	c.UpdatedAt = time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE customers SET external_id=$2, customer_type=$3, country_code=$4, product_types=$5, attributes=$6, risk_score=$7, risk_tier=$8, last_scored_at=$9, updated_at=$10 WHERE id=$1`,
		c.ID, c.ExternalID, c.CustomerType, c.CountryCode,
		c.ProductTypes, attrs,
		c.RiskScore, riskTierToNullable(c.RiskTier), c.LastScoredAt,
		c.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "customer", ID: c.ID}
	}
	return nil
}

func (r *PgCustomerRepo) SaveScoreRecord(ctx context.Context, rec *domain.ScoreRecord) error {
	factors, _ := json.Marshal(rec.Factors)
	_, err := r.pool.Exec(ctx,
		`INSERT INTO customer_score_history (id, customer_id, score, tier, factors, rule_set_id, rule_set_version, scored_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		rec.ID, rec.CustomerID, rec.Score, string(rec.Tier),
		factors, rec.RuleSetID, rec.RuleSetVersion, rec.ScoredAt,
	)
	return err
}

func (r *PgCustomerRepo) ListScoreHistory(ctx context.Context, customerID string, limit int) ([]domain.ScoreRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, customer_id, score, tier, factors, rule_set_id, rule_set_version, scored_at
		FROM customer_score_history WHERE customer_id = $1 ORDER BY scored_at DESC LIMIT $2`,
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
			&factors, &rec.RuleSetID, &rec.RuleSetVersion, &rec.ScoredAt,
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

func (r *PgTransactionRepo) Get(ctx context.Context, id string) (*domain.Transaction, error) {
	var t domain.Transaction
	err := r.pool.QueryRow(ctx,
		`SELECT id, customer_id, external_id, amount, currency, direction, counterparty_id, counterparty_country, channel, executed_at, created_at FROM transactions WHERE id = $1`, id,
	).Scan(
		&t.ID, &t.CustomerID, &t.ExternalID, &t.Amount, &t.Currency,
		&t.Direction, &t.CounterpartyID, &t.CounterpartyCountry,
		&t.Channel, &t.ExecutedAt, &t.CreatedAt,
	)
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
		`SELECT id, customer_id, external_id, amount, currency, direction, counterparty_id, counterparty_country, channel, executed_at, created_at
		FROM transactions WHERE customer_id = $1 ORDER BY executed_at DESC LIMIT $2 OFFSET $3`,
		customerID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []domain.Transaction
	for rows.Next() {
		var t domain.Transaction
		if err := rows.Scan(
			&t.ID, &t.CustomerID, &t.ExternalID, &t.Amount, &t.Currency,
			&t.Direction, &t.CounterpartyID, &t.CounterpartyCountry,
			&t.Channel, &t.ExecutedAt, &t.CreatedAt,
		); err != nil {
			return nil, err
		}
		txns = append(txns, t)
	}
	return txns, rows.Err()
}

func (r *PgTransactionRepo) ListByCustomerCursor(ctx context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.Transaction, error) {
	const baseQuery = `SELECT id, customer_id, external_id, amount, currency, direction, counterparty_id, counterparty_country, channel, executed_at, created_at
		FROM transactions WHERE customer_id = $1`

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
		var t domain.Transaction
		if err := rows.Scan(
			&t.ID, &t.CustomerID, &t.ExternalID, &t.Amount, &t.Currency,
			&t.Direction, &t.CounterpartyID, &t.CounterpartyCountry,
			&t.Channel, &t.ExecutedAt, &t.CreatedAt,
		); err != nil {
			return nil, err
		}
		txns = append(txns, t)
	}
	return txns, rows.Err()
}

func (r *PgTransactionRepo) Create(ctx context.Context, t *domain.Transaction) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO transactions (id, customer_id, external_id, amount, currency, direction, counterparty_id, counterparty_country, channel, executed_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		t.ID, t.CustomerID, t.ExternalID, t.Amount, t.Currency,
		string(t.Direction), t.CounterpartyID, t.CounterpartyCountry,
		t.Channel, t.ExecutedAt, t.CreatedAt,
	)
	return err
}

// PgAlertRepo

type PgAlertRepo struct {
	pool *pgxpool.Pool
}

func NewPgAlertRepo(pool *pgxpool.Pool) *PgAlertRepo {
	return &PgAlertRepo{pool: pool}
}

// alertColumns includes suppressed/suppression_reason (WL-004, additive
// columns from migrations/010_whitelist.sql).
const alertColumns = "id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, resolved_at, resolved_by, created_at, updated_at, suppressed, suppression_reason"

func scanAlertRow(row interface {
	Scan(dest ...any) error
}, a *domain.Alert) error {
	var suppressionReason *string
	if err := row.Scan(
		&a.ID, &a.CustomerID, &a.ScenarioID,
		&a.Severity, &a.Status, &a.Score, &a.Description,
		&a.TransactionIDs,
		&a.DetectedAt, &a.ResolvedAt, &a.ResolvedBy,
		&a.CreatedAt, &a.UpdatedAt,
		&a.Suppressed, &suppressionReason,
	); err != nil {
		return err
	}
	if suppressionReason != nil {
		a.SuppressionReason = *suppressionReason
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
		FROM alerts WHERE id = $1`, id,
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
		FROM alerts WHERE customer_id = $1 ORDER BY detected_at DESC LIMIT $2 OFFSET $3`,
		customerID, limit, offset,
	)
}

func (r *PgAlertRepo) ListOpen(ctx context.Context, limit, offset int) ([]domain.Alert, error) {
	return r.listAlerts(ctx,
		`SELECT `+alertColumns+`
		FROM alerts WHERE status = 'open' ORDER BY severity DESC, detected_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
}

func (r *PgAlertRepo) ListByCustomerCursor(ctx context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.Alert, error) {
	baseQuery := `SELECT ` + alertColumns + `
		FROM alerts WHERE customer_id = $1`

	if after == nil {
		return r.listAlerts(ctx, baseQuery+` ORDER BY created_at DESC, id DESC LIMIT $2`, customerID, limit)
	}
	return r.listAlerts(ctx, baseQuery+` AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4`,
		customerID, after.CreatedAt, after.ID, limit)
}

func (r *PgAlertRepo) ListOpenByCursor(ctx context.Context, limit int, after *domain.Cursor) ([]domain.Alert, error) {
	baseQuery := `SELECT ` + alertColumns + `
		FROM alerts WHERE status = 'open'`

	if after == nil {
		return r.listAlerts(ctx, baseQuery+` ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	}
	return r.listAlerts(ctx, baseQuery+` AND (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3`,
		after.CreatedAt, after.ID, limit)
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
		`INSERT INTO alerts (id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, created_at, updated_at, suppressed, suppression_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		a.ID, a.CustomerID, a.ScenarioID,
		string(a.Severity), string(a.Status), a.Score, a.Description,
		a.TransactionIDs,
		a.DetectedAt, a.CreatedAt, a.UpdatedAt,
		a.Suppressed, nullableString(a.SuppressionReason),
	)
	return err
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

func (r *PgAuditRepo) List(ctx context.Context, resourceType, resourceID string, limit int) ([]domain.AuditEntry, error) {
	query := `SELECT id, user_id, action, resource_type, resource_id, details, ip_address, user_agent, created_at FROM audit_logs`
	var args []any
	var conditions []string
	argIdx := 1

	if resourceType != "" {
		conditions = append(conditions, fmt.Sprintf("resource_type = $%d", argIdx))
		args = append(args, resourceType)
		argIdx++
	}
	if resourceID != "" {
		conditions = append(conditions, fmt.Sprintf("resource_id = $%d", argIdx))
		args = append(args, resourceID)
		argIdx++
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", argIdx)
	args = append(args, limit)

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
		`SELECT id, customer_id, alert_ids, status, priority, assigned_to, summary, created_at, updated_at, closed_at
		FROM cases WHERE id = $1`, id,
	).Scan(&c.ID, &c.CustomerID, &c.AlertIDs, &c.Status, &c.Priority, &c.AssignedTo, &c.Summary, &c.CreatedAt, &c.UpdatedAt, &c.ClosedAt)
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
		`SELECT id, customer_id, alert_ids, status, priority, assigned_to, summary, created_at, updated_at, closed_at
		FROM cases WHERE customer_id = $1 ORDER BY created_at DESC`, customerID)
}

func (r *PgCaseRepo) ListOpen(ctx context.Context, limit, offset int) ([]domain.Case, error) {
	return r.listCases(ctx,
		`SELECT id, customer_id, alert_ids, status, priority, assigned_to, summary, created_at, updated_at, closed_at
		FROM cases WHERE status != 'closed' ORDER BY priority DESC, created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
}

func (r *PgCaseRepo) ListOpenByCursor(ctx context.Context, limit int, after *domain.Cursor) ([]domain.Case, error) {
	const baseQuery = `SELECT id, customer_id, alert_ids, status, priority, assigned_to, summary, created_at, updated_at, closed_at
		FROM cases WHERE status != 'closed'`

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
		if err := rows.Scan(&c.ID, &c.CustomerID, &c.AlertIDs, &c.Status, &c.Priority, &c.AssignedTo, &c.Summary, &c.CreatedAt, &c.UpdatedAt, &c.ClosedAt); err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, rows.Err()
}

func (r *PgCaseRepo) Create(ctx context.Context, c *domain.Case) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO cases (id, customer_id, alert_ids, status, priority, assigned_to, summary, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		c.ID, c.CustomerID, c.AlertIDs, string(c.Status), string(c.Priority), c.AssignedTo, c.Summary, c.CreatedAt, c.UpdatedAt)
	return err
}

func (r *PgCaseRepo) Update(ctx context.Context, c *domain.Case) error {
	c.UpdatedAt = time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE cases SET status=$2, priority=$3, assigned_to=$4, summary=$5, updated_at=$6, closed_at=$7 WHERE id=$1`,
		c.ID, string(c.Status), string(c.Priority), c.AssignedTo, c.Summary, c.UpdatedAt, c.ClosedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "case", ID: c.ID}
	}
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
