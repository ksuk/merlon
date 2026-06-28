package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func (r *PgAlertRepo) scanAlert(rows pgx.Row) (*domain.Alert, error) {
	var a domain.Alert
	err := rows.Scan(
		&a.ID, &a.CustomerID, &a.ScenarioID,
		&a.Severity, &a.Status, &a.Score, &a.Description,
		&a.TransactionIDs,
		&a.DetectedAt, &a.ResolvedAt, &a.ResolvedBy,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *PgAlertRepo) Get(ctx context.Context, id string) (*domain.Alert, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, resolved_at, resolved_by, created_at, updated_at
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
		`SELECT id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, resolved_at, resolved_by, created_at, updated_at
		FROM alerts WHERE customer_id = $1 ORDER BY detected_at DESC LIMIT $2 OFFSET $3`,
		customerID, limit, offset,
	)
}

func (r *PgAlertRepo) ListOpen(ctx context.Context, limit, offset int) ([]domain.Alert, error) {
	return r.listAlerts(ctx,
		`SELECT id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, resolved_at, resolved_by, created_at, updated_at
		FROM alerts WHERE status = 'open' ORDER BY severity DESC, detected_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
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
		if err := rows.Scan(
			&a.ID, &a.CustomerID, &a.ScenarioID,
			&a.Severity, &a.Status, &a.Score, &a.Description,
			&a.TransactionIDs,
			&a.DetectedAt, &a.ResolvedAt, &a.ResolvedBy,
			&a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

func (r *PgAlertRepo) Create(ctx context.Context, a *domain.Alert) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO alerts (id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		a.ID, a.CustomerID, a.ScenarioID,
		string(a.Severity), string(a.Status), a.Score, a.Description,
		a.TransactionIDs,
		a.DetectedAt, a.CreatedAt, a.UpdatedAt,
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
