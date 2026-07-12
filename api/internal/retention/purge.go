package retention

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// PurgeFunc performs the logical-then-physical purge (the audit design §6 自動パー
// ジ) for one data category, given the cutoff (now - retention_days) before
// which records are eligible for purge.
type PurgeFunc func(ctx context.Context, cutoff, now time.Time) (logicallyDeleted, physicallyDeleted int, err error)

// PurgeResult summarizes one category's purge pass.
type PurgeResult struct {
	Category          string
	LogicallyDeleted  int
	PhysicallyDeleted int
}

// PurgeJob is the general-purpose automatic purge framework (RET-003).
// Targets maps data_category (retention_policies.data_category) to the
// PurgeFunc that knows how to purge that category's table(s); a category
// with no registered Target is skipped. This WS implements the framework
// and its audit trail without wiring concrete Targets for
// transactions/alerts/cases/customer_score_history, since their
// logical-delete lifecycle columns are introduced by WS-11 — additional
// categories plug in a PurgeFunc without changing Run's control flow.
type PurgeJob struct {
	Retention domain.RetentionRepository
	Audit     domain.AuditRepository
	Targets   map[string]PurgeFunc
}

// Run evaluates every configured retention policy against now and invokes
// the matching Target. It requires a start audit record before any mutation
// and writes a completion record after the targets finish.
func (j *PurgeJob) Run(ctx context.Context, now time.Time) ([]PurgeResult, error) {
	if j.Audit == nil {
		return nil, errors.New("purge audit repository is required")
	}
	policies, err := j.Retention.List(ctx)
	if err != nil {
		return nil, err
	}

	// Establish an audit trail before any target can commit an irreversible
	// deletion. The completion record below carries the result counts; this
	// start record remains evidence if a target or the final audit write fails.
	if err := j.Audit.Create(ctx, &domain.AuditEntry{
		Action:       "purge_execution_started",
		ResourceType: "retention_policy",
		CreatedAt:    now,
	}); err != nil {
		return nil, err
	}

	var results []PurgeResult
	for _, p := range policies {
		fn, ok := j.Targets[p.DataCategory]
		if !ok {
			continue
		}
		cutoff := now.AddDate(0, 0, -p.RetentionDays)
		logical, physical, err := fn(ctx, cutoff, now)
		if err != nil {
			return results, err
		}
		results = append(results, PurgeResult{
			Category:          p.DataCategory,
			LogicallyDeleted:  logical,
			PhysicallyDeleted: physical,
		})
	}

	if err := j.Audit.Create(ctx, &domain.AuditEntry{
		Action:       "purge_execution",
		ResourceType: "retention_policy",
		Details:      map[string]string{"categories_processed": strconv.Itoa(len(results))},
		CreatedAt:    now,
	}); err != nil {
		return results, err
	}

	return results, nil
}
