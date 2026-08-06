package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ksuk/merlon/api/internal/domain"
)

// PgBatchRunRepo implements domain.BatchRunRepository against batch_runs
// (migrations/013_batch_runs.sql, WS-5 Task6).
type PgBatchRunRepo struct {
	pool DBTX
}

func NewPgBatchRunRepo(pool DBTX) *PgBatchRunRepo {
	return &PgBatchRunRepo{pool: pool}
}

const batchRunColumns = "id, job_type, status, started_at, completed_at, processed_customer_ids, operation, parameters, target_manifest_id::text, config_digests, actor, result_counts, error, rerun_of::text, customer_outcomes, updated_at"

func scanBatchRun(row pgx.Row) (*domain.BatchRun, error) {
	var r domain.BatchRun
	var parameters, digests, counts, outcomes []byte
	var target, rerun *string
	err := row.Scan(&r.ID, &r.JobType, &r.Status, &r.StartedAt, &r.CompletedAt, &r.ProcessedCustomerIDs, &r.Operation, &parameters, &target, &digests, &r.Actor, &counts, &r.Error, &rerun, &outcomes, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.ID = compactUUID(r.ID)
	for i := range r.ProcessedCustomerIDs {
		r.ProcessedCustomerIDs[i] = compactUUID(r.ProcessedCustomerIDs[i])
	}
	if target != nil {
		r.TargetManifestID = compactUUID(*target)
	}
	if rerun != nil {
		r.RerunOf = compactUUID(*rerun)
	}
	_ = json.Unmarshal(parameters, &r.Parameters)
	_ = json.Unmarshal(digests, &r.ConfigDigests)
	_ = json.Unmarshal(counts, &r.ResultCounts)
	_ = json.Unmarshal(outcomes, &r.CustomerOutcomes)
	return &r, nil
}

func compactUUID(value string) string {
	return domain.CanonicalUUID(value)
}

// Create inserts run, matching the PgPendingEvaluationRepo convention of the
// caller supplying run.ID via generateID() up front.
func (r *PgBatchRunRepo) Create(ctx context.Context, run *domain.BatchRun) error {
	err := r.pool.QueryRow(ctx,
		`INSERT INTO batch_runs (id, job_type, status, operation, parameters, target_manifest_id, config_digests, actor, result_counts, error, rerun_of, customer_outcomes)
		VALUES ($1, $2, $3, COALESCE(NULLIF($4,''),$2), $5, NULLIF($6,'')::uuid, $7, $8, $9, $10, NULLIF($11,'')::uuid, $12)
		RETURNING started_at`,
		run.ID, run.JobType, string(run.Status), run.Operation, wave3JSON(run.Parameters), run.TargetManifestID, wave3JSON(run.ConfigDigests), run.Actor, wave3JSON(run.ResultCounts), run.Error, run.RerunOf, wave3JSON(run.CustomerOutcomes),
	).Scan(&run.StartedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_batch_runs_operation_idempotency" {
			return &domain.ErrConflict{Entity: "batch_run", ID: run.ID, Reason: "idempotency key already used"}
		}
	}
	return err
}

func (r *PgBatchRunRepo) ListBatchRuns(ctx context.Context, filter domain.BatchRunFilter, limit int) ([]domain.BatchRun, error) {
	query := `SELECT ` + batchRunColumns + ` FROM batch_runs WHERE 1=1`
	args := []any{}
	if filter.Operation != "" {
		query += fmt.Sprintf(" AND operation=$%d", len(args)+1)
		args = append(args, filter.Operation)
	}
	if filter.Status != "" {
		query += fmt.Sprintf(" AND status=$%d", len(args)+1)
		args = append(args, string(filter.Status))
	}
	if filter.Cursor != nil {
		query += fmt.Sprintf(" AND (started_at,id::text)<($%d,$%d)", len(args)+1, len(args)+2)
		args = append(args, filter.Cursor.CreatedAt, filter.Cursor.ID)
	}
	query += fmt.Sprintf(" ORDER BY started_at DESC,id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.BatchRun{}
	for rows.Next() {
		x, err := scanBatchRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}
func (r *PgBatchRunRepo) UpdateBatchRun(ctx context.Context, id string, status domain.BatchRunStatus, resultCounts map[string]int, failure string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE batch_runs SET status=$2,result_counts=$3,error=$4,updated_at=now(),completed_at=CASE WHEN $2<>'running' THEN now() ELSE completed_at END WHERE id=$1`, id, status, wave3JSON(resultCounts), failure)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "batch_run", ID: id}
	}
	return nil
}

func (r *PgBatchRunRepo) RecordBatchRunOutcome(ctx context.Context, runID string, outcome domain.BatchRunCustomerOutcome) error {
	if outcome.CustomerID == "" {
		return &domain.ErrConflict{Entity: "batch_run_outcome", ID: runID, Reason: "customer_id is required"}
	}
	if outcome.UpdatedAt.IsZero() {
		outcome.UpdatedAt = time.Now().UTC()
	}
	payload := wave3JSON(outcome)
	_, err := r.pool.Exec(ctx, `UPDATE batch_runs SET customer_outcomes=customer_outcomes || jsonb_build_object($2,$3::jsonb),updated_at=$4 WHERE id=$1`, runID, domain.CanonicalIdentifier(outcome.CustomerID), payload, outcome.UpdatedAt)
	return err
}
func (r *PgBatchRunRepo) FindBatchRunByIdempotency(ctx context.Context, operation, key string) (*domain.BatchRun, error) {
	if key == "" {
		return nil, nil
	}
	row := r.pool.QueryRow(ctx, `SELECT `+batchRunColumns+` FROM batch_runs WHERE operation=$1 AND parameters->>'idempotency_key'=$2 ORDER BY started_at DESC LIMIT 1`, operation, key)
	run, err := scanBatchRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return run, err
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

func (r *PgBatchRunRepo) AppendProcessedCustomerIfAbsent(ctx context.Context, id string, customerID string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var processed []string
	if err := tx.QueryRow(ctx, `SELECT processed_customer_ids FROM batch_runs WHERE id=$1 FOR UPDATE`, id).Scan(&processed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, &domain.ErrNotFound{Entity: "batch_run", ID: id}
		}
		return false, err
	}
	canonical := domain.CanonicalIdentifier(customerID)
	for _, existing := range processed {
		if domain.CanonicalIdentifier(existing) == canonical {
			if err := tx.Commit(ctx); err != nil {
				return false, err
			}
			return false, nil
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE batch_runs SET processed_customer_ids=array_append(processed_customer_ids,$2),updated_at=now() WHERE id=$1`, id, customerID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
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
