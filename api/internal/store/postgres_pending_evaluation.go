package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/domain"
)

// PgPendingEvaluationRepo implements domain.PendingEvaluationRepository
// against pending_evaluations (migrations/009_evaluation_queue.sql, OPS-005).
type PgPendingEvaluationRepo struct {
	pool DBTX
}

func NewPgPendingEvaluationRepo(pool DBTX) *PgPendingEvaluationRepo {
	return &PgPendingEvaluationRepo{pool: pool}
}

const pendingEvaluationColumns = "id, customer_id, transaction_ids, status, reason, batch_run_id, alert_ids, retry_count, manual_retry_count, resolved_at, last_attempt_at, next_retry_at, escalated_at, version, created_at, updated_at"

func scanPendingEvaluation(row pgx.Row) (*domain.PendingEvaluation, error) {
	var pe domain.PendingEvaluation
	err := row.Scan(
		&pe.ID, &pe.CustomerID, &pe.TransactionIDs, &pe.Status, &pe.Reason,
		&pe.BatchRunID, &pe.AlertIDs, &pe.RetryCount, &pe.ManualRetryCount, &pe.ResolvedAt, &pe.LastAttemptAt, &pe.NextRetryAt, &pe.EscalatedAt, &pe.Version, &pe.CreatedAt, &pe.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	pe.ID = compactUUID(pe.ID)
	for i := range pe.TransactionIDs {
		pe.TransactionIDs[i] = compactUUID(pe.TransactionIDs[i])
	}
	if pe.BatchRunID != nil {
		compacted := compactUUID(*pe.BatchRunID)
		pe.BatchRunID = &compacted
	}
	for i := range pe.AlertIDs {
		pe.AlertIDs[i] = compactUUID(pe.AlertIDs[i])
	}
	return &pe, nil
}

// Create inserts pe, matching the PgCustomerRepo/PgTransactionRepo convention
// of the caller (server handler) supplying pe.ID via generateID() up front
// rather than relying on the column's gen_random_uuid() default.
func (r *PgPendingEvaluationRepo) Create(ctx context.Context, pe *domain.PendingEvaluation) error {
	transactionIDs := pe.TransactionIDs
	if transactionIDs == nil {
		transactionIDs = []string{}
	}
	return r.pool.QueryRow(ctx,
		`INSERT INTO pending_evaluations (id, customer_id, transaction_ids, status, reason, batch_run_id, version)
		VALUES ($1, $2, $3, $4, $5, $6, 1)
		RETURNING created_at, updated_at`,
		pe.ID, pe.CustomerID, transactionIDs, string(pe.Status), pe.Reason, pe.BatchRunID,
	).Scan(&pe.CreatedAt, &pe.UpdatedAt)
}

func (r *PgPendingEvaluationRepo) GetLatestByTransaction(ctx context.Context, transactionID string) (*domain.PendingEvaluation, error) {
	pe, err := scanPendingEvaluation(r.pool.QueryRow(ctx,
		`SELECT `+pendingEvaluationColumns+` FROM pending_evaluations
		 WHERE $1::uuid = ANY(transaction_ids)
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`, domain.CanonicalUUID(transactionID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "pending_evaluation", ID: transactionID}
	}
	return pe, err
}

func (r *PgPendingEvaluationRepo) ListPendingEvaluations(ctx context.Context, filter domain.PendingEvaluationFilter, limit int) ([]domain.PendingEvaluation, error) {
	query := `SELECT ` + pendingEvaluationColumns + ` FROM pending_evaluations WHERE 1=1`
	args := []any{}
	if filter.CustomerID != "" {
		query += fmt.Sprintf(" AND customer_id=$%d", len(args)+1)
		args = append(args, domain.CanonicalUUID(filter.CustomerID))
	}
	if filter.BatchRunID != "" {
		query += fmt.Sprintf(" AND batch_run_id=$%d", len(args)+1)
		args = append(args, domain.CanonicalUUID(filter.BatchRunID))
	}
	if len(filter.Status) > 0 {
		query += fmt.Sprintf(" AND status = ANY($%d::text[])", len(args)+1)
		statuses := make([]string, len(filter.Status))
		for i, s := range filter.Status {
			statuses[i] = string(s)
		}
		args = append(args, statuses)
	}
	if filter.CreatedFrom != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", len(args)+1)
		args = append(args, *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		query += fmt.Sprintf(" AND created_at <= $%d", len(args)+1)
		args = append(args, *filter.CreatedTo)
	}
	if filter.Cursor != nil {
		query += fmt.Sprintf(" AND (created_at,id::text)<($%d,$%d)", len(args)+1, len(args)+2)
		args = append(args, filter.Cursor.CreatedAt, filter.Cursor.ID)
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.PendingEvaluation{}
	for rows.Next() {
		pe, err := scanPendingEvaluation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *pe)
	}
	return out, rows.Err()
}

func (r *PgPendingEvaluationRepo) ListPendingHistory(ctx context.Context, id string, limit int) ([]domain.PendingEvaluationHistoryEntry, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text,pending_evaluation_id::text,from_status,to_status,action,reason,actor,retry_count,created_at FROM pending_evaluation_history WHERE pending_evaluation_id=$1 ORDER BY created_at ASC,id ASC LIMIT $2`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.PendingEvaluationHistoryEntry{}
	for rows.Next() {
		var h domain.PendingEvaluationHistoryEntry
		if err := rows.Scan(&h.ID, &h.PendingEvaluationID, &h.FromStatus, &h.ToStatus, &h.Action, &h.Reason, &h.Actor, &h.RetryCount, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *PgPendingEvaluationRepo) TransitionPendingEvaluation(ctx context.Context, id, action, actor, reason string, expectedVersion int) (*domain.PendingEvaluation, error) {
	if expectedVersion <= 0 {
		return nil, &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "expected version is required"}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var pe domain.PendingEvaluation
	var batchRunID *string
	var alertIDs []string
	err = tx.QueryRow(ctx, `SELECT id::text,customer_id::text,transaction_ids,status,reason,batch_run_id::text,alert_ids,retry_count,manual_retry_count,resolved_at,last_attempt_at,next_retry_at,escalated_at,version,created_at,updated_at FROM pending_evaluations WHERE id=$1 FOR UPDATE`, id).Scan(&pe.ID, &pe.CustomerID, &pe.TransactionIDs, &pe.Status, &pe.Reason, &batchRunID, &alertIDs, &pe.RetryCount, &pe.ManualRetryCount, &pe.ResolvedAt, &pe.LastAttemptAt, &pe.NextRetryAt, &pe.EscalatedAt, &pe.Version, &pe.CreatedAt, &pe.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "pending_evaluation", ID: id}
	}
	if err != nil {
		return nil, err
	}
	pe.BatchRunID = batchRunID
	pe.AlertIDs = alertIDs
	if pe.Version != expectedVersion {
		return nil, &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "version mismatch"}
	}
	from := pe.Status
	now := time.Now().UTC()
	next := pe.NextRetryAt
	var resolved, escalated *time.Time
	// clearEscalation distinguishes "this action did not escalate" from "this
	// action un-escalates": both leave escalated nil, but only the second may
	// overwrite a previously recorded escalation.
	clearEscalation := false
	retry := pe.RetryCount
	manualRetry := pe.ManualRetryCount
	status := pe.Status
	switch action {
	case "retry":
		if status == domain.PendingEvaluationStatusResolved {
			return nil, &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "already resolved"}
		}
		// FAILED is where the automatic retry budget was spent. Reviving from
		// there is a deliberate operator act, not a continuation of the loop
		// that gave up, so it goes through manual_retry and its own counter.
		if status == domain.PendingEvaluationStatusFailed {
			return nil, &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "record has failed; use manual_retry to revive it"}
		}
		status = domain.PendingEvaluationStatusPendingReview
		retry++
		nextAt := now.Add(time.Duration(1<<minInt(retry, 8)) * time.Second)
		next = &nextAt
		pe.LastAttemptAt = &now
	case "manual_retry":
		if status == domain.PendingEvaluationStatusResolved {
			return nil, &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "already resolved"}
		}
		status = domain.PendingEvaluationStatusPendingReview
		manualRetry++
		next = &now
		pe.LastAttemptAt = &now
		clearEscalation = true
	case "process":
		if status != domain.PendingEvaluationStatusPendingReview {
			return nil, &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "record is not pending review"}
		}
		status = domain.PendingEvaluationStatusProcessing
		pe.LastAttemptAt = &now
		next = nil
	case "resolve":
		if status == domain.PendingEvaluationStatusResolved {
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return &pe, nil
		}
		status = domain.PendingEvaluationStatusResolved
		resolved = &now
		next = nil
	case "escalate":
		if status == domain.PendingEvaluationStatusResolved {
			return nil, &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "already resolved"}
		}
		status = domain.PendingEvaluationStatusFailed
		escalated = &now
		next = nil
	default:
		return nil, &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "unknown action"}
	}
	version := pe.Version + 1
	if _, err := tx.Exec(ctx, `UPDATE pending_evaluations SET status=$2,retry_count=$3,manual_retry_count=$4,resolved_at=$5,last_attempt_at=COALESCE($6,last_attempt_at),next_retry_at=$7,escalated_at=CASE WHEN $8::boolean THEN NULL ELSE COALESCE($9,escalated_at) END,version=$10,updated_at=$11 WHERE id=$1 AND version=$12`, id, status, retry, manualRetry, resolved, pe.LastAttemptAt, next, clearEscalation, escalated, version, now, pe.Version); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO pending_evaluation_history(id,pending_evaluation_id,from_status,to_status,action,reason,actor,retry_count,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, wave3ID(), id, from, status, action, reason, actor, retry, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	pe.Status = status
	pe.RetryCount = retry
	pe.ManualRetryCount = manualRetry
	pe.Version = version
	pe.UpdatedAt = now
	pe.ResolvedAt = resolved
	pe.NextRetryAt = next
	if clearEscalation {
		pe.EscalatedAt = nil
	} else if escalated != nil {
		pe.EscalatedAt = escalated
	}
	return &pe, nil
}

// SetPendingEvaluationAlertIDs persists the recovered alert links under the
// same optimistic-lock boundary used by status transitions. It is called
// inside the recovery transaction after PROCESSING is claimed and before the
// final RESOLVED transition.
func (r *PgPendingEvaluationRepo) SetPendingEvaluationAlertIDs(ctx context.Context, id string, alertIDs []string, expectedVersion int) error {
	if expectedVersion <= 0 {
		return &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "expected version is required"}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status domain.PendingEvaluationStatus
	var retry int
	var version int
	if err := tx.QueryRow(ctx, `SELECT status,retry_count,version FROM pending_evaluations WHERE id=$1 FOR UPDATE`, id).Scan(&status, &retry, &version); errors.Is(err, pgx.ErrNoRows) {
		return &domain.ErrNotFound{Entity: "pending_evaluation", ID: id}
	} else if err != nil {
		return err
	}
	if version != expectedVersion {
		return &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "version mismatch"}
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE pending_evaluations SET alert_ids=$2,version=version+1,updated_at=$3 WHERE id=$1 AND version=$4`, id, alertIDs, now, expectedVersion); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO pending_evaluation_history(id,pending_evaluation_id,from_status,to_status,action,reason,actor,retry_count,created_at) VALUES($1,$2,$3,$3,'link_alerts','recovery alert links persisted','system:pending-recovery',$4,$5)`, wave3ID(), id, status, retry, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
		`SELECT `+pendingEvaluationColumns+` FROM pending_evaluations WHERE status = $1 AND purge_marked_at IS NULL ORDER BY created_at ASC, id ASC LIMIT $2 OFFSET $3`,
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

// ListPendingByCustomer is used to make realtime/batch fail-alert enqueueing
// idempotent for one customer while an engine outage is active.
func (r *PgPendingEvaluationRepo) ListPendingByCustomer(ctx context.Context, customerID string, status domain.PendingEvaluationStatus) ([]domain.PendingEvaluation, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+pendingEvaluationColumns+` FROM pending_evaluations WHERE customer_id = $1 AND status = $2 ORDER BY created_at ASC`,
		customerID, string(status),
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

func (r *PgPendingEvaluationRepo) ListPendingByCustomers(ctx context.Context, customerIDs []string, status domain.PendingEvaluationStatus) ([]domain.PendingEvaluation, error) {
	if len(customerIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+pendingEvaluationColumns+` FROM pending_evaluations WHERE customer_id = ANY($1::text[]::uuid[]) AND status = $2 ORDER BY created_at ASC`,
		customerIDs, string(status),
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
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var from domain.PendingEvaluationStatus
	var retry int
	if err := tx.QueryRow(ctx, `SELECT status,retry_count FROM pending_evaluations WHERE id=$1 FOR UPDATE`, id).Scan(&from, &retry); errors.Is(err, pgx.ErrNoRows) {
		return &domain.ErrNotFound{Entity: "pending_evaluation", ID: id}
	} else if err != nil {
		return err
	}
	var resolvedAt *time.Time
	if status == domain.PendingEvaluationStatusResolved {
		now := time.Now().UTC()
		resolvedAt = &now
	}
	if _, err := tx.Exec(ctx, `UPDATE pending_evaluations SET status=$2,updated_at=now(),resolved_at=COALESCE($3,resolved_at),version=version+1 WHERE id=$1`, id, string(status), resolvedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO pending_evaluation_history(id,pending_evaluation_id,from_status,to_status,action,reason,actor,retry_count,created_at) VALUES($1,$2,$3,$4,'status','','',$5,now())`, wave3ID(), id, from, status, retry); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (r *PgPendingEvaluationRepo) IncrementRetry(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var from domain.PendingEvaluationStatus
	var retry int
	if err := tx.QueryRow(ctx, `SELECT status,retry_count FROM pending_evaluations WHERE id=$1 FOR UPDATE`, id).Scan(&from, &retry); errors.Is(err, pgx.ErrNoRows) {
		return &domain.ErrNotFound{Entity: "pending_evaluation", ID: id}
	} else if err != nil {
		return err
	}
	retry++
	if _, err := tx.Exec(ctx, `UPDATE pending_evaluations SET retry_count=$2,last_attempt_at=now(),updated_at=now(),version=version+1 WHERE id=$1`, id, retry); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO pending_evaluation_history(id,pending_evaluation_id,from_status,to_status,action,reason,actor,retry_count,created_at) VALUES($1,$2,$3,$3,'retry','','',$4,now())`, wave3ID(), id, from, retry); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}
