package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/domain"
)

type PgBacktestJobRepo struct{ pool *pgxpool.Pool }

func NewPgBacktestJobRepo(pool *pgxpool.Pool) *PgBacktestJobRepo {
	return &PgBacktestJobRepo{pool: pool}
}

const backtestJobColumns = `id,status,from_at,to_at,customer_ids,customer_filter,scenario_ids,baseline_rule_set_id,candidate_rule_set_id,baseline_rule_version,candidate_rule_version,baseline_rule_definition,candidate_rule_definition,config_digests,snapshot_at,total_customers,processed_customers,progress,eta_seconds,baseline,candidate,delta,error,created_at,started_at,completed_at,updated_at`

func scanBacktestJob(row pgx.Row) (*domain.BacktestJob, error) {
	var j domain.BacktestJob
	var status string
	var jobError *string
	var baselineVersion, candidateVersion *int
	var ids, filter, scenarios, baselineDefinition, candidateDefinition, digests, baseline, candidate, delta []byte
	if err := row.Scan(&j.ID, &status, &j.From, &j.To, &ids, &filter, &scenarios, &j.BaselineRuleSetID, &j.CandidateRuleSetID, &baselineVersion, &candidateVersion, &baselineDefinition, &candidateDefinition, &digests, &j.SnapshotAt, &j.TotalCustomers, &j.ProcessedCustomers, &j.Progress, &j.ETASeconds, &baseline, &candidate, &delta, &jobError, &j.CreatedAt, &j.StartedAt, &j.CompletedAt, &j.UpdatedAt); err != nil {
		return nil, err
	}
	if jobError != nil {
		j.Error = *jobError
	}
	if baselineVersion != nil {
		j.BaselineRuleVersion = *baselineVersion
	}
	if candidateVersion != nil {
		j.CandidateRuleVersion = *candidateVersion
	}
	j.Status = domain.BacktestJobStatus(status)
	_ = json.Unmarshal(ids, &j.CustomerIDs)
	_ = json.Unmarshal(scenarios, &j.ScenarioIDs)
	if len(filter) > 0 {
		j.CustomerFilter = &domain.BacktestCustomerFilter{}
		_ = json.Unmarshal(filter, j.CustomerFilter)
	}
	_ = json.Unmarshal(digests, &j.ConfigDigests)
	if len(baselineDefinition) > 0 && string(baselineDefinition) != "null" {
		j.BaselineRuleDefinition = append([]byte(nil), baselineDefinition...)
	}
	if len(candidateDefinition) > 0 && string(candidateDefinition) != "null" {
		j.CandidateRuleDefinition = append([]byte(nil), candidateDefinition...)
	}
	if len(baseline) > 0 {
		j.Baseline = &domain.BacktestResult{}
		_ = json.Unmarshal(baseline, j.Baseline)
	}
	if len(candidate) > 0 {
		j.Candidate = &domain.BacktestResult{}
		_ = json.Unmarshal(candidate, j.Candidate)
	}
	if len(delta) > 0 {
		j.Delta = &domain.BacktestResult{}
		_ = json.Unmarshal(delta, j.Delta)
	}
	return &j, nil
}

func marshalJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func (r *PgBacktestJobRepo) Create(ctx context.Context, j *domain.BacktestJob) error {
	if j.Status == "" {
		j.Status = domain.BacktestJobQueued
	}
	return r.pool.QueryRow(ctx, `INSERT INTO backtest_jobs (id,status,from_at,to_at,customer_ids,customer_filter,scenario_ids,baseline_rule_set_id,candidate_rule_set_id,baseline_rule_version,candidate_rule_version,baseline_rule_definition,candidate_rule_definition,config_digests,snapshot_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING created_at,updated_at`, j.ID, string(j.Status), j.From, j.To, marshalJSON(j.CustomerIDs), nullableJSON(j.CustomerFilter), marshalJSON(j.ScenarioIDs), j.BaselineRuleSetID, j.CandidateRuleSetID, nullableInt(j.BaselineRuleVersion), nullableInt(j.CandidateRuleVersion), nullableJSON(j.BaselineRuleDefinition), nullableJSON(j.CandidateRuleDefinition), marshalJSON(j.ConfigDigests), j.SnapshotAt).Scan(&j.CreatedAt, &j.UpdatedAt)
}
func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
func nullableJSON(v any) []byte {
	if v == nil {
		return nil
	}
	return marshalJSON(v)
}
func (r *PgBacktestJobRepo) Get(ctx context.Context, id string) (*domain.BacktestJob, error) {
	j, err := scanBacktestJob(r.pool.QueryRow(ctx, `SELECT `+backtestJobColumns+` FROM backtest_jobs WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "backtest_job", ID: id}
	}
	return j, err
}
func (r *PgBacktestJobRepo) List(ctx context.Context, limit, offset int) ([]domain.BacktestJob, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+backtestJobColumns+` FROM backtest_jobs ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BacktestJob
	for rows.Next() {
		j, err := scanBacktestJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}
func (r *PgBacktestJobRepo) Cancel(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE backtest_jobs SET status='cancelled',updated_at=now() WHERE id=$1 AND status IN ('queued','running')`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, e := r.Get(ctx, id); e != nil {
			return e
		}
	}
	return nil
}
func (r *PgBacktestJobRepo) ClaimNext(ctx context.Context) (*domain.BacktestJob, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM backtest_jobs WHERE status='queued' OR (status='running' AND lease_expires_at < now()) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	now := time.Now().UTC()
	if _, err = tx.Exec(ctx, `UPDATE backtest_jobs SET status='running',started_at=COALESCE(started_at,$2),lease_expires_at=$2 + interval '5 minutes',updated_at=$2 WHERE id=$1`, id, now); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}
func (r *PgBacktestJobRepo) UpdateProgress(ctx context.Context, id string, processed, total int, eta *int64) error {
	tag, err := r.pool.Exec(ctx, `UPDATE backtest_jobs SET processed_customers=$2::integer,total_customers=$3::integer,progress=CASE WHEN $3::integer=0 THEN 0 ELSE $2::double precision/$3::double precision END,eta_seconds=$4,lease_expires_at=now()+interval '5 minutes',updated_at=now() WHERE id=$1 AND status='running'`, id, processed, total, eta)
	return r.backtestMutationResult(ctx, id, tag.RowsAffected(), err)
}
func (r *PgBacktestJobRepo) Complete(ctx context.Context, id string, b, c, d *domain.BacktestResult) error {
	tag, err := r.pool.Exec(ctx, `UPDATE backtest_jobs SET status='completed',progress=1,baseline=$2,candidate=$3,delta=$4,completed_at=now(),updated_at=now() WHERE id=$1 AND status='running'`, id, nullableJSON(b), nullableJSON(c), nullableJSON(d))
	return r.backtestMutationResult(ctx, id, tag.RowsAffected(), err)
}
func (r *PgBacktestJobRepo) Fail(ctx context.Context, id, reason string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE backtest_jobs SET status='failed',error=$2,updated_at=now() WHERE id=$1 AND status='running'`, id, reason)
	return r.backtestMutationResult(ctx, id, tag.RowsAffected(), err)
}

// backtestMutationResult preserves terminal jobs as no-ops while retaining
// the repository contract's typed not-found error for unknown IDs.
func (r *PgBacktestJobRepo) backtestMutationResult(ctx context.Context, id string, rowsAffected int64, err error) error {
	if err != nil || rowsAffected > 0 {
		return err
	}
	_, err = r.Get(ctx, id)
	return err
}

func (r *PgBacktestJobRepo) SaveCustomerSnapshot(ctx context.Context, jobID string, customerIDs []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO backtest_job_customer_snapshots(job_id) VALUES($1) ON CONFLICT DO NOTHING`, jobID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM backtest_job_customers WHERE job_id=$1`, jobID); err != nil {
		return err
	}
	for _, customerID := range customerIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO backtest_job_customers(job_id,customer_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, jobID, customerID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *PgBacktestJobRepo) GetCustomerSnapshot(ctx context.Context, jobID string) ([]string, bool, error) {
	var snapshotted bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backtest_job_customer_snapshots WHERE job_id=$1)`, jobID).Scan(&snapshotted); err != nil {
		return nil, false, err
	}
	if !snapshotted {
		var exists bool
		if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backtest_jobs WHERE id=$1)`, jobID).Scan(&exists); err != nil {
			return nil, false, err
		}
		if !exists {
			return nil, false, &domain.ErrNotFound{Entity: "backtest_job", ID: jobID}
		}
		return nil, false, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT customer_id::text FROM backtest_job_customers WHERE job_id=$1 ORDER BY customer_id`, jobID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, false, err
		}
		ids = append(ids, id)
	}
	return ids, true, rows.Err()
}
