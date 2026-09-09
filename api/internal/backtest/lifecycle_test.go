package backtest

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

func TestLifecycleExpiresStaleQueuedJobsAndAuditsTransition(t *testing.T) {
	ctx := context.Background()
	jobs := store.NewMemoryBacktestJobRepo()
	audit := store.NewMemoryAuditRepo()
	now := time.Now().UTC()
	job := &domain.BacktestJob{ID: "stale-job", From: now.Add(-time.Hour), To: now, BaselineRuleSetID: "active", CandidateRuleSetID: "candidate", SnapshotAt: now}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	lifecycle := &Lifecycle{Jobs: jobs, Audit: audit, QueueTimeout: time.Minute, Now: func() time.Time { return now.Add(2 * time.Minute) }}
	expired, err := lifecycle.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 || expired[0] != job.ID {
		t.Fatalf("expired=%v", expired)
	}
	got, err := jobs.Get(ctx, job.ID)
	if err != nil || got.Status != domain.BacktestJobFailed || got.Error != QueueExpirationMessage {
		t.Fatalf("job=%+v err=%v", got, err)
	}
	entries, err := audit.List(ctx, domain.AuditListFilter{ResourceType: "backtests", ResourceID: job.ID})
	if err != nil || len(entries) != 1 || entries[0].Action != "backtest_queue_expired" {
		t.Fatalf("audit=%+v err=%v", entries, err)
	}
}

func TestLifecycleUsesDefaultQueueTimeout(t *testing.T) {
	lifecycle := &Lifecycle{Jobs: store.NewMemoryBacktestJobRepo()}
	if lifecycle.queueTimeout() != DefaultQueueTimeout {
		t.Fatalf("queue timeout=%s want=%s", lifecycle.queueTimeout(), DefaultQueueTimeout)
	}
}
