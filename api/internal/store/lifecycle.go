package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/domain"
)

// MemoryCaseAlertLifecycleRepo coordinates the in-memory case and alert
// repositories. It is deliberately a separate adapter so the ordinary CRUD
// interfaces remain small while tests and local development still exercise
// the same all-or-nothing lifecycle path as PostgreSQL.
type MemoryCaseAlertLifecycleRepo struct {
	cases  *MemoryCaseRepo
	alerts *MemoryAlertRepo
}

func NewMemoryCaseAlertLifecycleRepo(cases *MemoryCaseRepo, alerts *MemoryAlertRepo) *MemoryCaseAlertLifecycleRepo {
	return &MemoryCaseAlertLifecycleRepo{cases: cases, alerts: alerts}
}

func (r *MemoryCaseAlertLifecycleRepo) UpdateCaseAndAlerts(ctx context.Context, c *domain.Case, expectedUpdatedAt time.Time, transitions []domain.AlertStatusTransition) error {
	if r == nil || r.cases == nil || r.alerts == nil {
		return errors.New("case/alert lifecycle repository is not configured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Always lock in case-then-alert order. No other repository method takes
	// both locks, so this ordering prevents a cross-repository deadlock while
	// making validation and mutation one memory transaction.
	r.cases.mu.Lock()
	defer r.cases.mu.Unlock()
	r.alerts.mu.Lock()
	defer r.alerts.mu.Unlock()

	currentCase, ok := r.cases.data[c.ID]
	if !ok {
		return &domain.ErrNotFound{Entity: "case", ID: c.ID}
	}
	if !currentCase.UpdatedAt.Equal(expectedUpdatedAt) {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "updated_at mismatch"}
	}
	if currentCase.Status != c.Status && !domain.ValidCaseStatusTransition(currentCase.Status, c.Status) {
		return &domain.ErrInvalidStateTransition{Entity: "case", ID: c.ID, From: string(currentCase.Status), To: string(c.Status)}
	}

	seen := make(map[string]struct{}, len(transitions))
	for _, transition := range transitions {
		if _, duplicate := seen[transition.ID]; duplicate {
			return fmt.Errorf("duplicate linked alert: %s", transition.ID)
		}
		seen[transition.ID] = struct{}{}

		a, ok := r.alerts.data[transition.ID]
		if !ok {
			return &domain.ErrNotFound{Entity: "alert", ID: transition.ID}
		}
		if !a.UpdatedAt.Equal(transition.ExpectedUpdatedAt) || a.Status != transition.From {
			return &domain.ErrConflict{Entity: "alert", ID: transition.ID, Reason: "linked alert changed concurrently"}
		}
		if transition.From != transition.To && !domain.ValidAlertStatusTransition(transition.From, transition.To) {
			return &domain.ErrInvalidStateTransition{Entity: "alert", ID: transition.ID, From: string(transition.From), To: string(transition.To)}
		}
		if transition.From != transition.To && domain.IsAlertTerminal(transition.To) && strings.TrimSpace(transition.ResolvedBy) == "" {
			return fmt.Errorf("resolved_by is required for terminal alert status")
		}
	}

	now := time.Now()
	c.UpdatedAt = now
	r.cases.data[c.ID] = c
	for _, transition := range transitions {
		if transition.From == transition.To {
			continue
		}
		a := r.alerts.data[transition.ID]
		a.Status = transition.To
		a.UpdatedAt = now
		if domain.IsAlertTerminal(transition.To) {
			a.ResolvedBy = transition.ResolvedBy
			a.ResolvedAt = &now
		} else {
			a.ResolvedBy = ""
			a.ResolvedAt = nil
		}
	}
	return nil
}

// PgCaseAlertLifecycleRepo applies the same coordinated update in a single
// PostgreSQL transaction. It accepts DBTX so migration/seed tests can supply
// either a pool or a transaction-compatible test double.
type PgCaseAlertLifecycleRepo struct {
	pool DBTX
}

func NewPgCaseAlertLifecycleRepo(pool DBTX) *PgCaseAlertLifecycleRepo {
	return &PgCaseAlertLifecycleRepo{pool: pool}
}

func (r *PgCaseAlertLifecycleRepo) UpdateCaseAndAlerts(ctx context.Context, c *domain.Case, expectedUpdatedAt time.Time, transitions []domain.AlertStatusTransition) error {
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

	var currentStatus domain.CaseStatus
	var currentCaseUpdatedAt time.Time
	err = tx.QueryRow(ctx,
		`SELECT status, updated_at FROM cases WHERE id = $1 AND purge_marked_at IS NULL`, c.ID,
	).Scan(&currentStatus, &currentCaseUpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &domain.ErrNotFound{Entity: "case", ID: c.ID}
		}
		return err
	}
	if !currentCaseUpdatedAt.Equal(expectedUpdatedAt) {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "updated_at mismatch"}
	}
	if currentStatus != c.Status && !domain.ValidCaseStatusTransition(currentStatus, c.Status) {
		return &domain.ErrInvalidStateTransition{Entity: "case", ID: c.ID, From: string(currentStatus), To: string(c.Status)}
	}

	seen := make(map[string]struct{}, len(transitions))
	for _, transition := range transitions {
		if _, duplicate := seen[transition.ID]; duplicate {
			return fmt.Errorf("duplicate linked alert: %s", transition.ID)
		}
		seen[transition.ID] = struct{}{}

		var currentStatus domain.AlertStatus
		var currentUpdatedAt time.Time
		err = tx.QueryRow(ctx,
			`SELECT status, updated_at FROM alerts WHERE id = $1 AND purge_marked_at IS NULL`, transition.ID,
		).Scan(&currentStatus, &currentUpdatedAt)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &domain.ErrNotFound{Entity: "alert", ID: transition.ID}
			}
			return err
		}
		if !currentUpdatedAt.Equal(transition.ExpectedUpdatedAt) || currentStatus != transition.From {
			return &domain.ErrConflict{Entity: "alert", ID: transition.ID, Reason: "linked alert changed concurrently"}
		}
		if transition.From != transition.To && !domain.ValidAlertStatusTransition(transition.From, transition.To) {
			return &domain.ErrInvalidStateTransition{Entity: "alert", ID: transition.ID, From: string(transition.From), To: string(transition.To)}
		}
		if transition.From != transition.To && domain.IsAlertTerminal(transition.To) && strings.TrimSpace(transition.ResolvedBy) == "" {
			return fmt.Errorf("resolved_by is required for terminal alert status")
		}
	}

	now := time.Now()
	tag, err := tx.Exec(ctx,
		`UPDATE cases SET status=$2, priority=$3, assigned_to=$4, summary=$5, reopen_reason=$6, related_case_ids=$7, updated_at=$8, closed_at=$9
		 WHERE id=$1 AND updated_at=$10`,
		c.ID, string(c.Status), string(c.Priority), c.AssignedTo, c.Summary, c.ReopenReason,
		nonNilStrings(c.RelatedCaseIDs), now, c.ClosedAt, expectedUpdatedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "updated_at mismatch"}
	}

	for _, transition := range transitions {
		if transition.From == transition.To {
			continue
		}
		var resolvedBy any
		var resolvedAt any
		if domain.IsAlertTerminal(transition.To) {
			resolvedBy = transition.ResolvedBy
			resolvedAt = now
		}
		tag, err := tx.Exec(ctx,
			`UPDATE alerts SET status=$2, resolved_by=$3, resolved_at=$4, updated_at=$5
			 WHERE id=$1 AND status=$6 AND updated_at=$7`,
			transition.ID, string(transition.To), resolvedBy, resolvedAt, now,
			string(transition.From), transition.ExpectedUpdatedAt,
		)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return &domain.ErrConflict{Entity: "alert", ID: transition.ID, Reason: "linked alert changed concurrently"}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	c.UpdatedAt = now
	return nil
}
