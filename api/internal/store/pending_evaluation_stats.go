package store

import (
	"context"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// MaxPendingRetries mirrors batch.maxPendingRetries. It lives here as well so
// the stats query can say which records the automatic budget has given up on
// without the store importing the batch package.
//
// If the budget moves, both must move: a stats figure that disagrees with the
// job's own cut-off would tell an operator records are revivable when the
// recovery loop has already stopped touching them.
const MaxPendingRetries = 5

// pendingBacklogStatuses is the unresolved set. FAILED belongs here: the
// automatic budget gave up on it, but nobody has closed the monitoring gap.
var pendingBacklogStatuses = map[domain.PendingEvaluationStatus]bool{
	domain.PendingEvaluationStatusPendingReview: true,
	domain.PendingEvaluationStatusProcessing:    true,
	domain.PendingEvaluationStatusFailed:        true,
}

func (r *MemoryPendingEvaluationRepo) PendingEvaluationStats(_ context.Context, asOf time.Time) (domain.PendingEvaluationStats, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	stats := domain.PendingEvaluationStats{ByStatus: map[string]int{}, EvaluatedAt: asOf}
	for _, pe := range r.data {
		stats.ByStatus[string(pe.Status)]++
		if !pendingBacklogStatuses[pe.Status] {
			continue
		}
		stats.Backlog++
		if pe.Status == domain.PendingEvaluationStatusFailed {
			stats.Failed++
		}
		if pe.RetryCount >= MaxPendingRetries {
			stats.Exhausted++
		}
		if stats.OldestCreatedAt == nil || pe.CreatedAt.Before(*stats.OldestCreatedAt) {
			created := pe.CreatedAt
			stats.OldestCreatedAt = &created
		}
	}
	if stats.OldestCreatedAt != nil {
		stats.OldestAgeSeconds = int64(asOf.Sub(*stats.OldestCreatedAt).Seconds())
	}
	return stats, nil
}

func (r *PgPendingEvaluationRepo) PendingEvaluationStats(ctx context.Context, asOf time.Time) (domain.PendingEvaluationStats, error) {
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	stats := domain.PendingEvaluationStats{ByStatus: map[string]int{}, EvaluatedAt: asOf}
	// One grouped scan rather than a count per status, so the figures cannot
	// be taken at different instants and disagree with each other.
	rows, err := r.pool.Query(ctx, `
		SELECT status,
		       count(*),
		       count(*) FILTER (WHERE retry_count >= $1),
		       min(created_at)
		  FROM pending_evaluations
		 WHERE purge_marked_at IS NULL
		 GROUP BY status`, MaxPendingRetries)
	if err != nil {
		return domain.PendingEvaluationStats{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			status    string
			count     int
			exhausted int
			oldest    *time.Time
		)
		if err := rows.Scan(&status, &count, &exhausted, &oldest); err != nil {
			return domain.PendingEvaluationStats{}, err
		}
		stats.ByStatus[status] = count
		if !pendingBacklogStatuses[domain.PendingEvaluationStatus(status)] {
			continue
		}
		stats.Backlog += count
		stats.Exhausted += exhausted
		if domain.PendingEvaluationStatus(status) == domain.PendingEvaluationStatusFailed {
			stats.Failed += count
		}
		if oldest != nil && (stats.OldestCreatedAt == nil || oldest.Before(*stats.OldestCreatedAt)) {
			created := *oldest
			stats.OldestCreatedAt = &created
		}
	}
	if err := rows.Err(); err != nil {
		return domain.PendingEvaluationStats{}, err
	}
	if stats.OldestCreatedAt != nil {
		stats.OldestAgeSeconds = int64(asOf.Sub(*stats.OldestCreatedAt).Seconds())
	}
	return stats, nil
}
