package retention

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PhysicalDeletionGracePeriod gives operators 30 days between a record being
// removed from normal API reads and irreversible deletion.
const PhysicalDeletionGracePeriod = 30 * 24 * time.Hour

// PostgresPurger provides concrete PurgeJob targets for PostgreSQL-backed
// deployments. Each target marks records at the retention cutoff, then removes
// records whose mark has exceeded the grace period.
type PostgresPurger struct {
	pool *pgxpool.Pool
}

func NewPostgresPurger(pool *pgxpool.Pool) *PostgresPurger {
	return &PostgresPurger{pool: pool}
}

// CustomerReferencingTables lists every table holding a foreign key onto
// customers(id). CustomerData must account for all of them: a child row left
// behind makes DELETE FROM customers raise foreign_key_violation, which aborts
// the whole purge transaction rather than skipping the one customer involved.
//
// TestCustomerGuardCoversEveryForeignKey compares this list against the live
// PostgreSQL catalogue, so adding a foreign key in a migration without
// handling it here fails the integration suite.
var CustomerReferencingTables = []string{
	"account_customers",
	"alerts",
	"backtest_job_customers",
	"cases",
	"customer_identity_history",
	"customer_score_history",
	"pending_evaluations",
	"screening_results",
	"screening_runs",
	"str_reports",
	"transactions",
	"whitelist_entries",
}

func (p *PostgresPurger) Transactions(ctx context.Context, cutoff, now time.Time) (int, int, error) {
	return p.markAndDelete(ctx, "transactions", "executed_at", cutoff, now)
}

func (p *PostgresPurger) ScoreHistory(ctx context.Context, cutoff, now time.Time) (int, int, error) {
	return p.markAndDelete(ctx, "customer_score_history", "scored_at", cutoff, now)
}

func (p *PostgresPurger) AuditLogs(ctx context.Context, cutoff, now time.Time) (int, int, error) {
	return p.markAndDelete(ctx, "audit_logs", "created_at", cutoff, now)
}

// AlertCaseData applies the configured policy to alerts and cases. Case notes
// are deleted before their parent cases to keep the foreign key valid.
func (p *PostgresPurger) AlertCaseData(ctx context.Context, cutoff, now time.Time) (int, int, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx) // no-op after Commit

	alertsMarkedTag, err := tx.Exec(ctx, `
		UPDATE alerts SET purge_marked_at = $1
		WHERE purge_marked_at IS NULL
		  AND status IN ('closed_true_positive', 'closed_false_positive')
		  AND resolved_at <= $2`, now, cutoff)
	if err != nil {
		return 0, 0, err
	}
	graceCutoff := now.Add(-PhysicalDeletionGracePeriod)
	alertsDeletedTag, err := tx.Exec(ctx, `DELETE FROM alerts WHERE purge_marked_at <= $1`, graceCutoff)
	if err != nil {
		return int(alertsMarkedTag.RowsAffected()), 0, err
	}
	alertsMarked := int(alertsMarkedTag.RowsAffected())
	alertsDeleted := int(alertsDeletedTag.RowsAffected())

	caseMarked, err := tx.Exec(ctx,
		`UPDATE cases SET purge_marked_at = $1
		 WHERE purge_marked_at IS NULL AND status = 'closed' AND closed_at <= $2`, now, cutoff)
	if err != nil {
		return alertsMarked, alertsDeleted, err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM case_notes n USING cases c WHERE n.case_id = c.id AND c.purge_marked_at <= $1`, graceCutoff); err != nil {
		return alertsMarked + int(caseMarked.RowsAffected()), alertsDeleted, err
	}
	caseDeleted, err := tx.Exec(ctx, `DELETE FROM cases WHERE purge_marked_at <= $1`, graceCutoff)
	if err != nil {
		return alertsMarked + int(caseMarked.RowsAffected()), alertsDeleted, err
	}
	if err := tx.Commit(ctx); err != nil {
		return alertsMarked + int(caseMarked.RowsAffected()), alertsDeleted, err
	}
	return alertsMarked + int(caseMarked.RowsAffected()), alertsDeleted + int(caseDeleted.RowsAffected()), nil
}

// CustomerData marks customers from their last transaction, or from creation
// when they have no transactions. Physical deletion waits for separately
// retained child records so a longer child-data retention policy is honored.
func (p *PostgresPurger) CustomerData(ctx context.Context, cutoff, now time.Time) (int, int, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx) // no-op after Commit

	markedTag, err := tx.Exec(ctx, `
		UPDATE customers c SET purge_marked_at = $1
		WHERE c.purge_marked_at IS NULL
		  AND COALESCE((SELECT MAX(t.executed_at) FROM transactions t WHERE t.customer_id = c.id), c.created_at) <= $2
		  AND NOT EXISTS (SELECT 1 FROM alerts a WHERE a.customer_id = c.id AND a.resolved_at IS NULL)
		  AND NOT EXISTS (SELECT 1 FROM cases cs WHERE cs.customer_id = c.id AND cs.status <> 'closed')`, now, cutoff)
	if err != nil {
		return 0, 0, err
	}

	graceCutoff := now.Add(-PhysicalDeletionGracePeriod)
	if _, err := tx.Exec(ctx,
		`UPDATE screening_results s SET purge_marked_at = $1 FROM customers c WHERE s.customer_id = c.id AND s.purge_marked_at IS NULL AND c.purge_marked_at = $1`, now); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	// Migration 045 made screening_result_history a child of screening_results
	// and customer_identity_history and screening_runs children of customers.
	// Every child is marked and deleted before its parent; deleting the parent
	// first raised foreign_key_violation and aborted the whole purge.
	if _, err := tx.Exec(ctx,
		`UPDATE screening_result_history h SET purge_marked_at = $1 FROM screening_results s WHERE h.screening_result_id = s.id AND h.purge_marked_at IS NULL AND s.purge_marked_at = $1`, now); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM screening_result_history WHERE purge_marked_at <= $1`, graceCutoff); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM screening_results WHERE purge_marked_at <= $1`, graceCutoff); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE customer_identity_history h SET purge_marked_at = $1 FROM customers c WHERE h.customer_id = c.id AND h.purge_marked_at IS NULL AND c.purge_marked_at = $1`, now); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM customer_identity_history WHERE purge_marked_at <= $1`, graceCutoff); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE screening_runs r SET purge_marked_at = $1 FROM customers c WHERE r.customer_id = c.id AND r.purge_marked_at IS NULL AND c.purge_marked_at = $1`, now); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM screening_runs WHERE purge_marked_at <= $1`, graceCutoff); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	// Scope account cleanup to links removed by this purge. A global orphan
	// cleanup would delete unrelated, newly-created accounts that have not yet
	// been linked to a customer.
	if _, err := tx.Exec(ctx, `
		WITH removed_links AS (
			DELETE FROM account_customers ac USING customers c
			WHERE ac.customer_id = c.id AND c.purge_marked_at <= $1
			RETURNING ac.account_id
		)
		DELETE FROM accounts a USING (SELECT DISTINCT account_id FROM removed_links) removed
		WHERE a.id = removed.account_id
		  AND NOT EXISTS (SELECT 1 FROM account_customers ac WHERE ac.account_id = a.id)
		  AND NOT EXISTS (SELECT 1 FROM transactions t WHERE t.account_id = a.id)`, graceCutoff); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}

	// The guard must name every table holding a foreign key onto customers.
	// A missing one does not skip that customer: the DELETE raises 23503 and
	// rolls back the entire purge, so one unready customer stops retention for
	// all of them. CustomerReferencingTables is the checked list, and
	// TestCustomerGuardCoversEveryForeignKey compares it against the live
	// catalogue so a future migration cannot reopen this hole.
	deletedTag, err := tx.Exec(ctx, `
		DELETE FROM customers c
		WHERE c.purge_marked_at <= $1
		  AND NOT EXISTS (SELECT 1 FROM transactions t WHERE t.customer_id = c.id)
		  AND NOT EXISTS (SELECT 1 FROM alerts a WHERE a.customer_id = c.id)
		  AND NOT EXISTS (SELECT 1 FROM cases cs WHERE cs.customer_id = c.id)
		  AND NOT EXISTS (SELECT 1 FROM customer_score_history sh WHERE sh.customer_id = c.id)
		  AND NOT EXISTS (SELECT 1 FROM customer_identity_history ih WHERE ih.customer_id = c.id)
		  AND NOT EXISTS (SELECT 1 FROM screening_runs sr WHERE sr.customer_id = c.id)
		  AND NOT EXISTS (SELECT 1 FROM screening_results res WHERE res.customer_id = c.id)
		  AND NOT EXISTS (SELECT 1 FROM pending_evaluations pe WHERE pe.customer_id = c.id)
		  AND NOT EXISTS (SELECT 1 FROM whitelist_entries we WHERE we.customer_id = c.id)
		  AND NOT EXISTS (SELECT 1 FROM backtest_job_customers bjc WHERE bjc.customer_id = c.id)
		  AND NOT EXISTS (SELECT 1 FROM str_reports sr2 WHERE sr2.customer_id = c.id)
		  AND NOT EXISTS (SELECT 1 FROM account_customers ac WHERE ac.customer_id = c.id)`, graceCutoff)
	if err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	return int(markedTag.RowsAffected()), int(deletedTag.RowsAffected()), nil
}

// PendingEvaluationData purges resolved and failed monitoring gaps together
// with their transition history. Only terminal records are eligible: an open
// gap is unfinished work and must survive any retention pass.
func (p *PostgresPurger) PendingEvaluationData(ctx context.Context, cutoff, now time.Time) (int, int, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx) // no-op after Commit

	markedTag, err := tx.Exec(ctx, `
		UPDATE pending_evaluations SET purge_marked_at = $1
		WHERE purge_marked_at IS NULL
		  AND status IN ('RESOLVED', 'FAILED')
		  AND created_at <= $2`, now, cutoff)
	if err != nil {
		return 0, 0, err
	}

	graceCutoff := now.Add(-PhysicalDeletionGracePeriod)
	if _, err := tx.Exec(ctx, `
		UPDATE pending_evaluation_history h SET purge_marked_at = $1
		FROM pending_evaluations e
		WHERE h.pending_evaluation_id = e.id AND h.purge_marked_at IS NULL AND e.purge_marked_at = $1`, now); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM pending_evaluation_history WHERE purge_marked_at <= $1`, graceCutoff); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	deletedTag, err := tx.Exec(ctx, `DELETE FROM pending_evaluations WHERE purge_marked_at <= $1`, graceCutoff)
	if err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	return int(markedTag.RowsAffected()), int(deletedTag.RowsAffected()), nil
}

// BacktestData purges completed backtest jobs and their comparison snapshots.
// Without it, job history grew without bound: it was the one Wave 3 stream
// with no retention lifecycle at all.
func (p *PostgresPurger) BacktestData(ctx context.Context, cutoff, now time.Time) (int, int, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx) // no-op after Commit

	markedTag, err := tx.Exec(ctx, `
		UPDATE backtest_jobs SET purge_marked_at = $1
		WHERE purge_marked_at IS NULL
		  AND status IN ('completed', 'failed', 'cancelled')
		  AND created_at <= $2`, now, cutoff)
	if err != nil {
		return 0, 0, err
	}

	graceCutoff := now.Add(-PhysicalDeletionGracePeriod)
	if _, err := tx.Exec(ctx, `
		UPDATE backtest_job_metadata m SET purge_marked_at = $1
		FROM backtest_jobs j
		WHERE m.job_id = j.id AND m.purge_marked_at IS NULL AND j.purge_marked_at = $1`, now); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM backtest_job_metadata WHERE purge_marked_at <= $1`, graceCutoff); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	deletedTag, err := tx.Exec(ctx, `DELETE FROM backtest_jobs WHERE purge_marked_at <= $1`, graceCutoff)
	if err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	return int(markedTag.RowsAffected()), int(deletedTag.RowsAffected()), nil
}

func (p *PostgresPurger) markAndDelete(ctx context.Context, table, timestampColumn string, cutoff, now time.Time) (int, int, error) {
	// table and timestampColumn are private constants selected above, never
	// caller input.
	markedTag, err := p.pool.Exec(ctx,
		"UPDATE "+table+" SET purge_marked_at = $1 WHERE purge_marked_at IS NULL AND "+timestampColumn+" <= $2", now, cutoff)
	if err != nil {
		return 0, 0, err
	}
	deletedTag, err := p.pool.Exec(ctx,
		"DELETE FROM "+table+" WHERE purge_marked_at <= $1", now.Add(-PhysicalDeletionGracePeriod))
	if err != nil {
		return int(markedTag.RowsAffected()), 0, err
	}
	return int(markedTag.RowsAffected()), int(deletedTag.RowsAffected()), nil
}
