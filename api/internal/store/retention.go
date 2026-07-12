package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/domain"
)

// pgCheckViolationCode is the PostgreSQL SQLSTATE for a CHECK constraint
// violation (retention_days_positive or the legacy retention_no_shorten).
const pgCheckViolationCode = "23514"

// PgRetentionRepo implements domain.RetentionRepository against
// retention_policies (migrations/017_retention.sql).
type PgRetentionRepo struct {
	pool *pgxpool.Pool
}

func NewPgRetentionRepo(pool *pgxpool.Pool) *PgRetentionRepo {
	return &PgRetentionRepo{pool: pool}
}

const retentionColumns = "id, data_category, retention_days, min_retention_days, updated_by, created_at, updated_at"

func scanRetentionPolicy(row pgx.Row) (*domain.RetentionPolicy, error) {
	var p domain.RetentionPolicy
	var updatedBy *string
	if err := row.Scan(
		&p.ID, &p.DataCategory, &p.RetentionDays, &p.MinRetentionDays,
		&updatedBy, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if updatedBy != nil {
		p.UpdatedBy = *updatedBy
	}
	return &p, nil
}

func (r *PgRetentionRepo) List(ctx context.Context) ([]domain.RetentionPolicy, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+retentionColumns+` FROM retention_policies ORDER BY data_category`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.RetentionPolicy
	for rows.Next() {
		p, err := scanRetentionPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *PgRetentionRepo) Get(ctx context.Context, dataCategory string) (*domain.RetentionPolicy, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+retentionColumns+` FROM retention_policies WHERE data_category = $1`, dataCategory)
	p, err := scanRetentionPolicy(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "retention_policy", ID: dataCategory}
		}
		return nil, err
	}
	return p, nil
}

// Update applies a positive retention period. The current migration leaves
// MinRetentionDays nil, while legacy/custom deployments with a configured
// minimum continue to receive ErrRetentionShorten.
func (r *PgRetentionRepo) Update(ctx context.Context, dataCategory string, retentionDays int, updatedBy string) (*domain.RetentionPolicy, error) {
	if retentionDays <= 0 {
		return nil, &domain.ErrInvalidRetentionDays{Days: retentionDays}
	}
	row := r.pool.QueryRow(ctx,
		`UPDATE retention_policies SET retention_days = $1, updated_by = $2, updated_at = now()
		WHERE data_category = $3
		RETURNING `+retentionColumns,
		retentionDays, nullableString(updatedBy), dataCategory,
	)
	p, err := scanRetentionPolicy(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "retention_policy", ID: dataCategory}
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgCheckViolationCode {
			min := 0
			if existing, getErr := r.Get(ctx, dataCategory); getErr == nil && existing.MinRetentionDays != nil {
				min = *existing.MinRetentionDays
			}
			return nil, &domain.ErrRetentionShorten{DataCategory: dataCategory, RequestedDays: retentionDays, MinDays: min}
		}
		return nil, err
	}
	return p, nil
}
