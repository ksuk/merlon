package backtest

import (
	"context"
	"log/slog"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	merlonmetrics "github.com/ksuk/merlon/api/internal/metrics"
)

const (
	DefaultQueueTimeout     = 10 * time.Minute
	ExecutionFailureMessage = "backtest execution failed; retry is available"
	QueueExpirationMessage  = "backtest execution did not start before the queue deadline; retry is available"
)

type Lifecycle struct {
	Jobs         domain.BacktestJobRepository
	Audit        domain.AuditRepository
	QueueTimeout time.Duration
	Now          func() time.Time
}

func (l *Lifecycle) queueTimeout() time.Duration {
	if l.QueueTimeout <= 0 {
		return DefaultQueueTimeout
	}
	return l.QueueTimeout
}

func (l *Lifecycle) now() time.Time {
	if l.Now != nil {
		return l.Now().UTC()
	}
	return time.Now().UTC()
}

func (l *Lifecycle) Run(ctx context.Context, poll time.Duration) error {
	if poll <= 0 {
		poll = time.Minute
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		if _, err := l.RunOnce(ctx); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "expire stale backtest jobs", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (l *Lifecycle) RunOnce(ctx context.Context) ([]string, error) {
	expired, err := l.Jobs.ExpireQueued(ctx, l.now().Add(-l.queueTimeout()), QueueExpirationMessage)
	if err != nil {
		return nil, err
	}
	for _, id := range expired {
		l.recordExpiration(ctx, id)
		merlonmetrics.BacktestJobTransitionsTotal.WithLabelValues("queue_expired").Inc()
	}
	return expired, nil
}

func (l *Lifecycle) recordExpiration(ctx context.Context, jobID string) {
	if l.Audit == nil {
		return
	}
	entry := &domain.AuditEntry{
		UserID:       "system:backtest-lifecycle",
		Action:       "backtest_queue_expired",
		ResourceType: "backtests",
		ResourceID:   jobID,
		Details:      map[string]string{"status": string(domain.BacktestJobFailed)},
		CreatedAt:    l.now(),
	}
	if err := l.Audit.Create(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "record backtest queue expiration audit", "job_id", jobID, "error", err)
	}
}
