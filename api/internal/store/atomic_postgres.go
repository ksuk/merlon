package store

import (
	"context"
	"fmt"

	"github.com/ksuk/merlon/api/internal/domain"
)

// PgAtomicMutationRepo binds every Wave 2 durable mutation repository to one
// pgx transaction. External webhook delivery is deliberately outside this
// unit of work; the required durable event and audit rows are committed
// before a response is written.
type PgAtomicMutationRepo struct {
	pool DBTX
}

func NewPgAtomicMutationRepo(pool DBTX) *PgAtomicMutationRepo {
	return &PgAtomicMutationRepo{pool: pool}
}

func (r *PgAtomicMutationRepo) RunAtomic(ctx context.Context, fn func(domain.AtomicMutationRepositories) error) error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("atomic PostgreSQL repository is not configured")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	repos := domain.AtomicMutationRepositories{
		Customers:          NewPgCustomerRepo(tx, nil),
		Transactions:       NewPgTransactionRepo(tx),
		Alerts:             NewPgAlertRepo(tx),
		Reports:            NewPgSTRReportRepo(tx),
		Audit:              NewPgAuditRepo(tx),
		Cases:              NewPgCaseRepo(tx),
		CaseAlertLifecycle: NewPgCaseAlertLifecycleRepo(tx),
		Investigation:      NewPgCaseInvestigationRepo(tx),
		AlertDecisions:     NewPgAlertDecisionRepo(tx),
		EventOutbox:        NewPgEventOutboxRepo(tx),
	}
	if err := fn(repos); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}
