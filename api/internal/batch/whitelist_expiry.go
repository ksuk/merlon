// Package batch runs periodic maintenance jobs (currently whitelist expiry).
package batch

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// expiringSoonWindowDays is the lookahead window for the review/expiry
// notification (WL-006, whitelist.md §2). The spec does not pin an exact
// number of days for this notification; 30 is used as a reasonable default
// pending WS-8 notify-package integration (see task doc ws06-whitelist.md
// Task 4).
const expiringSoonWindowDays = 30

// NotifyFunc is called with the active entries expiring within
// expiringSoonWindowDays that have not yet lapsed. TODO(WS-8): route this
// through api/internal/notify once it exists, instead of the caller-supplied
// callback.
type NotifyFunc func(entries []domain.WhitelistEntry)

// ExpiryJobResult reports what RunWhitelistExpiryJob did on a single run.
type ExpiryJobResult struct {
	Expired      int
	ExpiringSoon int
}

// RunWhitelistExpiryJob is the idempotent daily expiry check (WL-006,
// whitelist.md §2: "期限切れ判定ジョブは冪等に実装する"). It transitions
// active entries whose valid_until has passed to status=expired, then
// reports (and optionally notifies about) active entries expiring within
// expiringSoonWindowDays. Running it twice in a row is safe: entries already
// moved to status=expired are excluded from the next run's overdue query
// because that query is scoped to status=active.
func RunWhitelistExpiryJob(ctx context.Context, repo domain.WhitelistRepository, notify NotifyFunc) (ExpiryJobResult, error) {
	var result ExpiryJobResult

	overdue, err := repo.ListExpiringSoon(ctx, 0)
	if err != nil {
		return result, err
	}

	for _, e := range overdue {
		entry := e
		entry.Status = domain.WhitelistEntryStatusExpired
		if err := repo.UpdateWithVersion(ctx, &entry, e.Version); err != nil {
			var conflict *domain.ErrConflict
			if errors.As(err, &conflict) {
				// Already handled by a concurrent run; idempotency (WL-006).
				continue
			}
			return result, err
		}
		result.Expired++
	}

	soon, err := repo.ListExpiringSoon(ctx, expiringSoonWindowDays)
	if err != nil {
		return result, err
	}
	result.ExpiringSoon = len(soon)
	if notify != nil && len(soon) > 0 {
		notify(soon)
	}

	return result, nil
}

// StartExpiryTicker runs RunWhitelistExpiryJob on a fixed interval until ctx
// is cancelled. It is kept separate from RunWhitelistExpiryJob so the job
// logic itself stays trivially testable without a ticker/goroutine involved.
func StartExpiryTicker(ctx context.Context, repo domain.WhitelistRepository, interval time.Duration, notify NotifyFunc) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := RunWhitelistExpiryJob(ctx, repo, notify); err != nil {
					slog.ErrorContext(ctx, "whitelist expiry job failed", "error", err)
				}
			}
		}
	}()
}
