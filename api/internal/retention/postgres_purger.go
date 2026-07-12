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
	if _, err := tx.Exec(ctx, `DELETE FROM screening_results WHERE purge_marked_at <= $1`, graceCutoff); err != nil {
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

	deletedTag, err := tx.Exec(ctx, `
		DELETE FROM customers c
		WHERE c.purge_marked_at <= $1
		  AND NOT EXISTS (SELECT 1 FROM transactions t WHERE t.customer_id = c.id)
		  AND NOT EXISTS (SELECT 1 FROM alerts a WHERE a.customer_id = c.id)
		  AND NOT EXISTS (SELECT 1 FROM cases cs WHERE cs.customer_id = c.id)
		  AND NOT EXISTS (SELECT 1 FROM customer_score_history sh WHERE sh.customer_id = c.id)`, graceCutoff)
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
