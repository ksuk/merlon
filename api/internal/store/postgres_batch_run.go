package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/domain"
)

// PgBatchRunRepo implements domain.BatchRunRepository against batch_runs
// (migrations/013_batch_runs.sql, WS-5 Task6).
type PgBatchRunRepo struct {
	pool *pgxpool.Pool
}

func NewPgBatchRunRepo(pool *pgxpool.Pool) *PgBatchRunRepo {
	return &PgBatchRunRepo{pool: pool}
}

const batchRunColumns = "id, job_type, status, started_at, completed_at, processed_customer_ids"

func scanBatchRun(row pgx.Row) (*domain.BatchRun, error) {
	var r domain.BatchRun
	err := row.Scan(&r.ID, &r.JobType, &r.Status, &r.StartedAt, &r.CompletedAt, &r.ProcessedCustomerIDs)
	if err != nil {
		return nil, err
	}
	r.ID = compactUUID(r.ID)
	for i := range r.ProcessedCustomerIDs {
		r.ProcessedCustomerIDs[i] = compactUUID(r.ProcessedCustomerIDs[i])
	}
	return &r, nil
}

func compactUUID(value string) string {
	return strings.ReplaceAll(value, "-", "")
}

// Create inserts run, matching the PgPendingEvaluationRepo convention of the
// caller supplying run.ID via generateID() up front.
func (r *PgBatchRunRepo) Create(ctx context.Context, run *domain.BatchRun) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO batch_runs (id, job_type, status)
		VALUES ($1, $2, $3)
		RETURNING started_at`,
		run.ID, run.JobType, string(run.Status),
	).Scan(&run.StartedAt)
}

func (r *PgBatchRunRepo) Get(ctx context.Context, id string) (*domain.BatchRun, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+batchRunColumns+` FROM batch_runs WHERE id = $1`, id)
	run, err := scanBatchRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "batch_run", ID: id}
		}
		return nil, err
	}
	return run, nil
}

func (r *PgBatchRunRepo) GetLatestRunning(ctx context.Context, jobType string) (*domain.BatchRun, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+batchRunColumns+` FROM batch_runs
		WHERE job_type = $1 AND status = 'running'
		ORDER BY started_at DESC LIMIT 1`,
		jobType,
	)
	run, err := scanBatchRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return run, nil
}

func (r *PgBatchRunRepo) AppendProcessedCustomer(ctx context.Context, id string, customerID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE batch_runs SET processed_customer_ids = array_append(processed_customer_ids, $2) WHERE id = $1`,
		id, customerID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "batch_run", ID: id}
	}
	return nil
}

func (r *PgBatchRunRepo) Complete(ctx context.Context, id string) error {
	return r.setStatus(ctx, id, domain.BatchRunStatusCompleted)
}

func (r *PgBatchRunRepo) Fail(ctx context.Context, id string) error {
	return r.setStatus(ctx, id, domain.BatchRunStatusFailed)
}

func (r *PgBatchRunRepo) setStatus(ctx context.Context, id string, status domain.BatchRunStatus) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE batch_runs SET status = $2, completed_at = now() WHERE id = $1`,
		id, string(status),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "batch_run", ID: id}
	}
	return nil
}
