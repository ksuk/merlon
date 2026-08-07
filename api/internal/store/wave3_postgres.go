package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/domain"
)

// PgWave3Repo contains only Wave 3 additions.  Existing repositories remain
// usable by older workers, while these methods use one pgx transaction for
// durable screening persistence and review/case creation.
type PgWave3Repo struct{ pool DBTX }

func NewPgWave3Repo(pool DBTX) *PgWave3Repo { return &PgWave3Repo{pool: pool} }

func wave3JSON(v any) []byte { b, _ := json.Marshal(v); return b }

const screeningWorkflowResultColumns = `id, customer_id, list_id, list_type, entry_id, matched_name, similarity, status,
 false_positive_reason, reviewed_by, reviewed_at, screened_at, created_at, COALESCE(run_id::text,''),
 suppressed, COALESCE(suppression_reason,''), degraded, degraded_sources, match_evidence,
 COALESCE(case_id,''), version, updated_at`

// scanWorkflowResult and scanWorkflowResultRows differ only in the row type
// pgx hands back, so both delegate the column order to one place: a column
// added to screeningWorkflowResultColumns cannot be wired into one scanner and
// forgotten in the other.
type workflowResultScanner interface {
	Scan(dest ...any) error
}

func scanWorkflowResultFrom(src workflowResultScanner) (*domain.ScreeningResultRecord, error) {
	var out domain.ScreeningResultRecord
	var reason, reviewedBy, runID, suppressionReason, caseID *string
	var evidence []byte
	if err := src.Scan(&out.ID, &out.CustomerID, &out.ListID, &out.ListType, &out.EntryID, &out.MatchedName,
		&out.Similarity, &out.Status, &reason, &reviewedBy, &out.ReviewedAt, &out.ScreenedAt, &out.CreatedAt,
		&runID, &out.Suppressed, &suppressionReason, &out.Degraded, &out.DegradedSources, &evidence,
		&caseID, &out.Version, &out.UpdatedAt); err != nil {
		return nil, err
	}
	if reason != nil {
		out.FalsePositiveReason = *reason
	}
	if reviewedBy != nil {
		out.ReviewedBy = *reviewedBy
	}
	if runID != nil {
		out.RunID = domain.CanonicalUUID(*runID)
	}
	if suppressionReason != nil {
		out.SuppressionReason = *suppressionReason
	}
	if caseID != nil {
		out.CaseID = *caseID
	}
	out.MatchEvidence = map[string]any{}
	if len(evidence) > 0 {
		_ = json.Unmarshal(evidence, &out.MatchEvidence)
	}
	return &out, nil
}

const screeningRunColumns = `id::text, customer_id::text, list_ids, config_digests, status, result_count,
 COALESCE(error,''), actor, degraded, degraded_sources, started_at, completed_at, created_at`

func scanScreeningRun(src workflowResultScanner) (*domain.ScreeningRun, error) {
	var run domain.ScreeningRun
	var id, customerID string
	var ids, digests []byte
	if err := src.Scan(&id, &customerID, &ids, &digests, &run.Status, &run.ResultCount, &run.Error,
		&run.Actor, &run.Degraded, &run.DegradedSources, &run.StartedAt, &run.CompletedAt, &run.CreatedAt); err != nil {
		return nil, err
	}
	run.ID = domain.CanonicalUUID(id)
	run.CustomerID = domain.CanonicalUUID(customerID)
	_ = json.Unmarshal(ids, &run.ListIDs)
	_ = json.Unmarshal(digests, &run.ConfigDigests)
	if run.ListIDs == nil {
		run.ListIDs = []string{}
	}
	if run.ConfigDigests == nil {
		run.ConfigDigests = map[string]string{}
	}
	return &run, nil
}

func scanWorkflowResult(row pgx.Row) (*domain.ScreeningResultRecord, error) {
	return scanWorkflowResultFrom(row)
}

func scanWorkflowResultRows(rows pgx.Rows) (*domain.ScreeningResultRecord, error) {
	return scanWorkflowResultFrom(rows)
}

func (r *PgWave3Repo) PersistScreeningRun(ctx context.Context, run *domain.ScreeningRun, results []domain.ScreeningResultRecord) error {
	if run == nil || run.ID == "" {
		return &domain.ErrConflict{Entity: "screening_run", Reason: "id is required"}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = run.StartedAt
	}
	if run.CompletedAt == nil {
		run.CompletedAt = &now
	}
	if run.Status == "" {
		run.Status = domain.ScreeningRunCompleted
	}
	run.ResultCount = len(results)
	if _, err := tx.Exec(ctx, `INSERT INTO screening_runs(id,customer_id,list_ids,config_digests,status,result_count,error,actor,degraded,degraded_sources,started_at,completed_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, run.ID, domain.CanonicalUUID(run.CustomerID), wave3JSON(run.ListIDs), wave3JSON(run.ConfigDigests), run.Status, run.ResultCount, nullableString(run.Error), run.Actor, run.Degraded, nonNilStrings(run.DegradedSources), run.StartedAt, run.CompletedAt, run.CreatedAt); err != nil {
		return err
	}
	for i := range results {
		result := &results[i]
		if result.ID == "" {
			result.ID = wave3ID()
		}
		result.RunID = run.ID
		if result.Version == 0 {
			result.Version = 1
		}
		if result.CreatedAt.IsZero() {
			result.CreatedAt = run.CreatedAt
		}
		if result.UpdatedAt.IsZero() {
			result.UpdatedAt = result.CreatedAt
		}
		if result.MatchEvidence == nil {
			result.MatchEvidence = map[string]any{}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO screening_results(id,customer_id,list_id,list_type,entry_id,matched_name,similarity,status,false_positive_reason,reviewed_by,reviewed_at,screened_at,created_at,run_id,suppressed,suppression_reason,degraded,degraded_sources,match_evidence,case_id,version,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`, result.ID, domain.CanonicalUUID(result.CustomerID), result.ListID, result.ListType, result.EntryID, result.MatchedName, result.Similarity, result.Status, nullableString(result.FalsePositiveReason), nullableString(result.ReviewedBy), result.ReviewedAt, result.ScreenedAt, result.CreatedAt, run.ID, result.Suppressed, result.SuppressionReason, result.Degraded, nonNilStrings(result.DegradedSources), wave3JSON(result.MatchEvidence), nullableString(result.CaseID), result.Version, result.UpdatedAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *PgWave3Repo) GetScreeningRun(ctx context.Context, id string) (*domain.ScreeningRun, error) {
	run, err := scanScreeningRun(r.pool.QueryRow(ctx, `SELECT `+screeningRunColumns+` FROM screening_runs WHERE id=$1 AND purge_marked_at IS NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "screening_run", ID: id}
	}
	return run, err
}

func (r *PgWave3Repo) GetScreeningResult(ctx context.Context, id string) (*domain.ScreeningResultRecord, error) {
	result, err := scanWorkflowResult(r.pool.QueryRow(ctx, `SELECT `+screeningWorkflowResultColumns+` FROM screening_results WHERE id=$1 AND purge_marked_at IS NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "screening_result", ID: id}
	}
	return result, err
}

func (r *PgWave3Repo) ListScreeningRuns(ctx context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.ScreeningRun, error) {
	query := `SELECT ` + screeningRunColumns + ` FROM screening_runs WHERE purge_marked_at IS NULL`
	args := []any{}
	n := 1
	if customerID != "" {
		query += fmt.Sprintf(" AND customer_id=$%d", n)
		args = append(args, domain.CanonicalUUID(customerID))
		n++
	}
	if after != nil {
		query += fmt.Sprintf(" AND (created_at,id::text)<($%d,$%d)", n, n+1)
		args = append(args, after.CreatedAt, after.ID)
		n += 2
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d", n)
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ScreeningRun{}
	for rows.Next() {
		run, err := scanScreeningRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *run)
	}
	return out, rows.Err()
}

func (r *PgWave3Repo) ListScreeningResults(ctx context.Context, filter domain.ScreeningResultFilter, limit int) ([]domain.ScreeningResultRecord, error) {
	query := `SELECT ` + screeningWorkflowResultColumns + ` FROM screening_results WHERE purge_marked_at IS NULL`
	args := []any{}
	n := 1
	if filter.CustomerID != "" {
		query += fmt.Sprintf(" AND customer_id=$%d", n)
		args = append(args, domain.CanonicalUUID(filter.CustomerID))
		n++
	}
	if filter.Status != "" {
		query += fmt.Sprintf(" AND status=$%d", n)
		args = append(args, filter.Status)
		n++
	}
	if filter.ListID != "" {
		query += fmt.Sprintf(" AND list_id=$%d", n)
		args = append(args, filter.ListID)
		n++
	}
	if filter.From != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", n)
		args = append(args, *filter.From)
		n++
	}
	if filter.To != nil {
		query += fmt.Sprintf(" AND created_at <= $%d", n)
		args = append(args, *filter.To)
		n++
	}
	if filter.Suppressed != nil {
		query += fmt.Sprintf(" AND suppressed = $%d", n)
		args = append(args, *filter.Suppressed)
		n++
	}
	if filter.Cursor != nil {
		query += fmt.Sprintf(" AND (created_at,id)<($%d,$%d)", n, n+1)
		args = append(args, filter.Cursor.CreatedAt, filter.Cursor.ID)
		n += 2
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d", n)
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ScreeningResultRecord{}
	for rows.Next() {
		x, err := scanWorkflowResultRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

func (r *PgWave3Repo) CountByCustomer(ctx context.Context, customerID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM screening_results WHERE customer_id=$1 AND purge_marked_at IS NULL`, domain.CanonicalUUID(customerID)).Scan(&count)
	return count, err
}

func (r *PgWave3Repo) ReviewScreeningResult(ctx context.Context, id string, to domain.ScreeningResultStatus, reason, actor string, expectedVersion int) (*domain.ScreeningReviewOutcome, error) {
	if expectedVersion <= 0 {
		return nil, &domain.ErrConflict{Entity: "screening_result", ID: id, Reason: "expected version is required"}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `SELECT `+screeningWorkflowResultColumns+` FROM screening_results WHERE id=$1 AND purge_marked_at IS NULL FOR UPDATE`, id)
	result, err := scanWorkflowResult(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "screening_result", ID: id}
	}
	if err != nil {
		return nil, err
	}
	if result.Version != expectedVersion {
		return nil, &domain.ErrConflict{Entity: "screening_result", ID: id, Reason: "version mismatch"}
	}
	from := result.Status
	if err := result.ApplyStatusTransition(to, reason); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	result.ReviewedBy = actor
	result.ReviewedAt = &now
	result.Version++
	result.UpdatedAt = now
	out := &domain.ScreeningReviewOutcome{Result: result}
	caseID := result.CaseID
	if to == domain.ScreeningResultStatusTruePositive && caseID == "" {
		caseID = wave3ID()
		summary := fmt.Sprintf("Screening true positive: %s matched %s (%s)", result.MatchedName, result.ListID, result.ListType)
		if _, err := tx.Exec(ctx, `INSERT INTO cases(id,customer_id,alert_ids,status,priority,summary,created_at,updated_at) VALUES($1,$2,'{}','new','critical',$3,$4,$4)`, caseID, domain.CanonicalUUID(result.CustomerID), summary, now); err != nil {
			return nil, err
		}
		out.CaseID = caseID
		out.CaseCreated = true
		result.CaseID = caseID
	} else {
		out.CaseID = caseID
	}
	if _, err := tx.Exec(ctx, `UPDATE screening_results SET status=$2,false_positive_reason=$3,reviewed_by=$4,reviewed_at=$5,case_id=$6,version=$7,updated_at=$8 WHERE id=$1 AND version=$9`, id, result.Status, nullableString(result.FalsePositiveReason), nullableString(actor), now, nullableString(caseID), result.Version, result.UpdatedAt, result.Version-1); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO screening_result_history(id,screening_result_id,from_status,to_status,rationale,actor,version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, wave3ID(), id, from, to, reason, actor, result.Version, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *PgWave3Repo) ListScreeningResultHistory(ctx context.Context, id string, limit int) ([]domain.ScreeningResultHistoryEntry, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text,screening_result_id,from_status,to_status,rationale,actor,version,created_at FROM screening_result_history WHERE screening_result_id=$1 ORDER BY created_at ASC,id ASC LIMIT $2`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ScreeningResultHistoryEntry{}
	for rows.Next() {
		var x domain.ScreeningResultHistoryEntry
		if err := rows.Scan(&x.ID, &x.ScreeningResultID, &x.FromStatus, &x.ToStatus, &x.Rationale, &x.Actor, &x.Version, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (r *PgWave3Repo) ListScreeningSources(ctx context.Context, configuredIDs []string, thresholdFor func(string) time.Duration) ([]domain.ScreeningSourceStatus, error) {
	rows, err := r.pool.Query(ctx, `SELECT ids.list_id,COALESCE(s.list_type,''),s.imported_at,f.last_attempt_at,f.last_success_at,f.last_failure_at,COALESCE(f.consecutive_failures,0),COALESCE(f.last_error,'') FROM unnest($1::text[]) AS ids(list_id) LEFT JOIN screening_list_snapshots s ON s.list_id=ids.list_id LEFT JOIN screening_list_failures f ON f.list_id=ids.list_id ORDER BY ids.list_id`, configuredIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	out := []domain.ScreeningSourceStatus{}
	for rows.Next() {
		var snapshot domain.ScreeningSourceSnapshot
		var imported, attempt, success, failure *time.Time
		if err := rows.Scan(&snapshot.ListID, &snapshot.ListType, &imported, &attempt, &success, &failure, &snapshot.ConsecutiveFailures, &snapshot.Diagnostic); err != nil {
			return nil, err
		}
		snapshot.LastAttemptAt = attempt
		snapshot.LastSuccessAt = success
		snapshot.LastFailureAt = failure
		// The snapshot table predates the failure tracker. Treat an existing
		// imported snapshot as the last successful import so legacy rows do not
		// appear as never_imported after the source directory is enabled.
		if snapshot.LastSuccessAt == nil && imported != nil {
			snapshot.LastSuccessAt = imported
		}
		snapshot.SnapshotMissing = imported == nil
		out = append(out, domain.ClassifyScreeningSource(snapshot, resolveSourceThreshold(thresholdFor, snapshot.ListID), now))
	}
	return out, rows.Err()
}

func (r *PgWave3Repo) SaveBacktestMetadata(ctx context.Context, m *domain.BacktestMetadata) error {
	if m == nil || m.JobID == "" {
		return &domain.ErrConflict{Entity: "backtest_metadata", Reason: "job_id is required"}
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO backtest_job_metadata(job_id,rationale,cohort_preview,baseline_snapshot,candidate_snapshot,rerun_of,created_at) VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,$7) ON CONFLICT(job_id) DO UPDATE SET rationale=EXCLUDED.rationale,cohort_preview=EXCLUDED.cohort_preview,baseline_snapshot=EXCLUDED.baseline_snapshot,candidate_snapshot=EXCLUDED.candidate_snapshot`, m.JobID, m.Rationale, wave3JSON(m.CohortPreview), wave3JSON(m.BaselineSnapshot), wave3JSON(m.CandidateSnapshot), m.RerunOf, m.CreatedAt)
	return err
}
func (r *PgWave3Repo) GetBacktestMetadata(ctx context.Context, id string) (*domain.BacktestMetadata, error) {
	var m domain.BacktestMetadata
	var cohort, base, candidate []byte
	var rerun *string
	err := r.pool.QueryRow(ctx, `SELECT job_id::text,rationale,cohort_preview,baseline_snapshot,candidate_snapshot,COALESCE(rerun_of::text,''),created_at FROM backtest_job_metadata WHERE job_id=$1`, id).Scan(&m.JobID, &m.Rationale, &cohort, &base, &candidate, &rerun, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "backtest_metadata", ID: id}
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(cohort, &m.CohortPreview)
	_ = json.Unmarshal(base, &m.BaselineSnapshot)
	_ = json.Unmarshal(candidate, &m.CandidateSnapshot)
	if rerun != nil {
		m.RerunOf = domain.CanonicalUUID(*rerun)
	}
	return &m, nil
}

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func (r *PgWave3Repo) CreateTargetManifest(ctx context.Context, m *domain.TargetManifest) error {
	if m == nil || m.ID == "" {
		return &domain.ErrConflict{Entity: "target_manifest", Reason: "id is required"}
	}
	if m.Version == 0 {
		m.Version = 1
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	if m.Token == "" {
		m.Token = wave3ID()
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO target_manifests(id,operation,target_mode,customer_ids,filter,sample_customer_ids,target_count,criteria,rule_set_id,rule_set_version,config_digests,token_hash,idempotency_key,rationale,status,version,expires_at,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,''),$14,$15,$16,$17,$18,$19)`, m.ID, m.Operation, m.TargetMode, wave3JSON(m.CustomerIDs), wave3JSON(m.Filter), wave3JSON(m.SampleCustomerIDs), m.TargetCount, m.Criteria, m.RuleSetID, m.RuleSetVersion, wave3JSON(m.ConfigDigests), tokenHash(m.Token), m.IdempotencyKey, m.Rationale, m.Status, m.Version, m.ExpiresAt, m.CreatedBy, m.CreatedAt)
	return err
}
func (r *PgWave3Repo) GetTargetManifest(ctx context.Context, id string) (*domain.TargetManifest, error) {
	var m domain.TargetManifest
	var ids, filter, sample, digests []byte
	var confirmed *time.Time
	var run *string
	err := r.pool.QueryRow(ctx, `SELECT id::text,operation,target_mode,customer_ids,filter,sample_customer_ids,target_count,criteria,rule_set_id,rule_set_version,config_digests,'',COALESCE(idempotency_key,''),rationale,status,version,expires_at,created_by,created_at,confirmed_at,COALESCE(run_id::text,'') FROM target_manifests WHERE id=$1`, id).Scan(&m.ID, &m.Operation, &m.TargetMode, &ids, &filter, &sample, &m.TargetCount, &m.Criteria, &m.RuleSetID, &m.RuleSetVersion, &digests, &m.Token, &m.IdempotencyKey, &m.Rationale, &m.Status, &m.Version, &m.ExpiresAt, &m.CreatedBy, &m.CreatedAt, &confirmed, &run)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "target_manifest", ID: id}
	}
	if err != nil {
		return nil, err
	}
	m.ConfirmedAt = confirmed
	if run != nil {
		m.RunID = domain.CanonicalUUID(*run)
	}
	_ = json.Unmarshal(ids, &m.CustomerIDs)
	_ = json.Unmarshal(filter, &m.Filter)
	_ = json.Unmarshal(sample, &m.SampleCustomerIDs)
	_ = json.Unmarshal(digests, &m.ConfigDigests)
	if m.CustomerIDs == nil {
		m.CustomerIDs = []string{}
	}
	if m.SampleCustomerIDs == nil {
		m.SampleCustomerIDs = []string{}
	}
	return &m, nil
}
func (r *PgWave3Repo) ConfirmTargetManifest(ctx context.Context, id, token, actor, rationale, idempotencyKey string, expectedVersion int) (*domain.TargetManifest, error) {
	if expectedVersion <= 0 {
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "expected version is required"}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var hash string
	var version int
	var status, existingKey string
	var expires time.Time
	err = tx.QueryRow(ctx, `SELECT token_hash,version,status,COALESCE(idempotency_key,''),expires_at FROM target_manifests WHERE id=$1 FOR UPDATE`, id).Scan(&hash, &version, &status, &existingKey, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "target_manifest", ID: id}
	}
	if err != nil {
		return nil, err
	}
	if tokenHash(token) != hash {
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "stale token"}
	}
	if (status == "confirmed" || status == "consumed") && idempotencyKey != "" && idempotencyKey == existingKey {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return r.GetTargetManifest(ctx, id)
	}
	if version != expectedVersion {
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "version mismatch"}
	}
	if time.Now().UTC().After(expires) {
		_, _ = tx.Exec(ctx, `UPDATE target_manifests SET status='expired',version=version+1 WHERE id=$1`, id)
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "manifest expired"}
	}
	if status == "confirmed" || status == "consumed" {
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "already confirmed"}
	}
	_, err = tx.Exec(ctx, `UPDATE target_manifests SET status='confirmed',version=version+1,created_by=$2,rationale=COALESCE(NULLIF($3,''),rationale),idempotency_key=COALESCE(NULLIF($4,''),idempotency_key),confirmed_at=now() WHERE id=$1 AND version=$5`, id, actor, rationale, idempotencyKey, version)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetTargetManifest(ctx, id)
}

func (r *PgWave3Repo) ClaimTargetManifest(ctx context.Context, id, runID string, expectedVersion int) (*domain.TargetManifest, error) {
	if expectedVersion <= 0 || runID == "" {
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "expected version and run id are required"}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var status string
	var version int
	err = tx.QueryRow(ctx, `SELECT status,version FROM target_manifests WHERE id=$1 FOR UPDATE`, id).Scan(&status, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "target_manifest", ID: id}
	}
	if err != nil {
		return nil, err
	}
	if status != "confirmed" || version != expectedVersion {
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "target manifest is stale or already consumed"}
	}
	if _, err := tx.Exec(ctx, `UPDATE target_manifests SET status='consumed',run_id=$2,version=version+1 WHERE id=$1 AND version=$3`, id, runID, expectedVersion); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetTargetManifest(ctx, id)
}

func (r *PgWave3Repo) AppendCustomerIdentityHistory(ctx context.Context, e *domain.CustomerIdentityHistoryEntry) error {
	if e.ID == "" {
		e.ID = wave3ID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO customer_identity_history(id,customer_id,changed_fields,actor,rationale,created_at) VALUES($1,$2,$3,$4,$5,$6)`, e.ID, domain.CanonicalUUID(e.CustomerID), wave3JSON(e.ChangedFields), e.Actor, e.Rationale, e.CreatedAt)
	return err
}
func (r *PgWave3Repo) ListCustomerIdentityHistory(ctx context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.CustomerIdentityHistoryEntry, error) {
	query := `SELECT id::text,customer_id::text,changed_fields,actor,rationale,created_at FROM customer_identity_history WHERE customer_id=$1`
	args := []any{domain.CanonicalUUID(customerID)}
	if after != nil {
		query += ` AND (created_at,id::text)<($2,$3)`
		args = append(args, after.CreatedAt, after.ID)
	}
	query += fmt.Sprintf(` ORDER BY created_at DESC,id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.CustomerIdentityHistoryEntry{}
	for rows.Next() {
		var e domain.CustomerIdentityHistoryEntry
		var fields []byte
		if err := rows.Scan(&e.ID, &e.CustomerID, &fields, &e.Actor, &e.Rationale, &e.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(fields, &e.ChangedFields)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Keep these imports/functions referenced in older Go toolchains where the
// compiler otherwise treats the helper-only packages as unused after build
// tags alter the PG integration files.
var _ = sort.Strings
var _ = strings.TrimSpace
