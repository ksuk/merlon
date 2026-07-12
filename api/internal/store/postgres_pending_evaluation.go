package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/domain"
)

// PgPendingEvaluationRepo implements domain.PendingEvaluationRepository
// against pending_evaluations (migrations/009_evaluation_queue.sql, OPS-005).
type PgPendingEvaluationRepo struct {
	pool *pgxpool.Pool
}

func NewPgPendingEvaluationRepo(pool *pgxpool.Pool) *PgPendingEvaluationRepo {
	return &PgPendingEvaluationRepo{pool: pool}
}

const pendingEvaluationColumns = "id, customer_id, transaction_ids, status, reason, batch_run_id, retry_count, resolved_at, created_at, updated_at"

func scanPendingEvaluation(row pgx.Row) (*domain.PendingEvaluation, error) {
	var pe domain.PendingEvaluation
	err := row.Scan(
		&pe.ID, &pe.CustomerID, &pe.TransactionIDs, &pe.Status, &pe.Reason,
		&pe.BatchRunID, &pe.RetryCount, &pe.ResolvedAt, &pe.CreatedAt, &pe.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &pe, nil
}

// Create inserts pe, matching the PgCustomerRepo/PgTransactionRepo convention
// of the caller (server handler) supplying pe.ID via generateID() up front
// rather than relying on the column's gen_random_uuid() default.
func (r *PgPendingEvaluationRepo) Create(ctx context.Context, pe *domain.PendingEvaluation) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO pending_evaluations (id, customer_id, transaction_ids, status, reason, batch_run_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at`,
		pe.ID, pe.CustomerID, pe.TransactionIDs, string(pe.Status), pe.Reason, pe.BatchRunID,
	).Scan(&pe.CreatedAt, &pe.UpdatedAt)
}

func (r *PgPendingEvaluationRepo) Get(ctx context.Context, id string) (*domain.PendingEvaluation, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+pendingEvaluationColumns+` FROM pending_evaluations WHERE id = $1`, id,
	)
	pe, err := scanPendingEvaluation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "pending_evaluation", ID: id}
		}
		return nil, err
	}
	return pe, nil
}

func (r *PgPendingEvaluationRepo) ListByStatus(ctx context.Context, status domain.PendingEvaluationStatus, limit, offset int) ([]domain.PendingEvaluation, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+pendingEvaluationColumns+` FROM pending_evaluations WHERE status = $1 ORDER BY created_at ASC LIMIT $2 OFFSET $3`,
		string(status), limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.PendingEvaluation
	for rows.Next() {
		pe, err := scanPendingEvaluation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *pe)
	}
	return out, rows.Err()
}

func (r *PgPendingEvaluationRepo) UpdateStatus(ctx context.Context, id string, status domain.PendingEvaluationStatus) error {
	var resolvedAtClause string
	if status == domain.PendingEvaluationStatusResolved {
		resolvedAtClause = ", resolved_at = now()"
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE pending_evaluations SET status = $2, updated_at = now()`+resolvedAtClause+` WHERE id = $1`,
		id, string(status),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "pending_evaluation", ID: id}
	}
	return nil
}

func (r *PgPendingEvaluationRepo) IncrementRetry(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE pending_evaluations SET retry_count = retry_count + 1, updated_at = now() WHERE id = $1`,
		id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "pending_evaluation", ID: id}
	}
	return nil
}
