package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func testBacktestTerminalStateGuards(t *testing.T, repo domain.BacktestJobRepository, newID func() string, cleanup func(string)) {
	t.Helper()
	ctx := context.Background()
	result := &domain.BacktestResult{BacktestID: "result", TotalAlerts: 7}

	for _, terminal := range []domain.BacktestJobStatus{
		domain.BacktestJobCancelled,
		domain.BacktestJobCompleted,
		domain.BacktestJobFailed,
	} {
		t.Run(string(terminal), func(t *testing.T) {
			now := time.Now().UTC()
			job := &domain.BacktestJob{
				ID:                      newID(),
				From:                    now.Add(-time.Hour),
				To:                      now,
				CustomerIDs:             []string{"customer-1"},
				BaselineRuleSetID:       "active",
				CandidateRuleSetID:      "candidate",
				CandidateRuleDefinition: []byte(`{"scenario_id":"candidate"}`),
				SnapshotAt:              now,
			}
			if err := repo.Create(ctx, job); err != nil {
				t.Fatalf("Create: %v", err)
			}
			cleanup(job.ID)
			if claimed, err := repo.ClaimNext(ctx); err != nil || claimed == nil || claimed.ID != job.ID {
				t.Fatalf("ClaimNext = %+v, %v", claimed, err)
			}
			if err := repo.UpdateProgress(ctx, job.ID, 1, 4, nil); err != nil {
				t.Fatalf("initial UpdateProgress: %v", err)
			}

			switch terminal {
			case domain.BacktestJobCancelled:
				if err := repo.Cancel(ctx, job.ID); err != nil {
					t.Fatalf("Cancel: %v", err)
				}
			case domain.BacktestJobCompleted:
				if err := repo.Complete(ctx, job.ID, result, result, result); err != nil {
					t.Fatalf("Complete: %v", err)
				}
			case domain.BacktestJobFailed:
				if err := repo.Fail(ctx, job.ID, "original failure"); err != nil {
					t.Fatalf("Fail: %v", err)
				}
			}

			before, err := repo.Get(ctx, job.ID)
			if err != nil {
				t.Fatalf("Get before guarded updates: %v", err)
			}
			eta := int64(99)
			if err := repo.UpdateProgress(ctx, job.ID, 4, 4, &eta); err != nil {
				t.Fatalf("guarded UpdateProgress: %v", err)
			}
			if err := repo.Complete(ctx, job.ID, result, result, result); err != nil {
				t.Fatalf("guarded Complete: %v", err)
			}
			if err := repo.Fail(ctx, job.ID, "replacement failure"); err != nil {
				t.Fatalf("guarded Fail: %v", err)
			}
			after, err := repo.Get(ctx, job.ID)
			if err != nil {
				t.Fatalf("Get after guarded updates: %v", err)
			}
			if after.Status != before.Status || after.ProcessedCustomers != before.ProcessedCustomers ||
				after.TotalCustomers != before.TotalCustomers || after.Progress != before.Progress ||
				after.Error != before.Error || !after.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatalf("terminal job was mutated\nbefore=%+v\nafter=%+v", before, after)
			}
		})
	}

	missingID := newID()
	eta := int64(1)
	for name, mutate := range map[string]func() error{
		"UpdateProgress": func() error { return repo.UpdateProgress(ctx, missingID, 1, 1, &eta) },
		"Complete":       func() error { return repo.Complete(ctx, missingID, result, result, result) },
		"Fail":           func() error { return repo.Fail(ctx, missingID, "failure") },
	} {
		t.Run(name+"Missing", func(t *testing.T) {
			var notFound *domain.ErrNotFound
			if err := mutate(); !errors.As(err, &notFound) {
				t.Fatalf("error = %v, want *domain.ErrNotFound", err)
			}
		})
	}
}

func TestMemoryBacktestJobRepoTerminalStateGuards(t *testing.T) {
	sequence := 0
	testBacktestTerminalStateGuards(t, NewMemoryBacktestJobRepo(), func() string {
		sequence++
		return "memory-job-" + time.Now().UTC().Format("150405.000000000") + string(rune('a'+sequence))
	}, func(string) {})
}

func TestPostgresBacktestJobRepoTerminalStateGuards(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgBacktestJobRepo(pool)
	testBacktestTerminalStateGuards(t, repo, func() string {
		raw := newTestUUID()
		return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
	}, func(id string) {
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM backtest_jobs WHERE id=$1`, id)
		})
	})
}

func testBacktestRetryLifecycle(t *testing.T, repo domain.BacktestJobRepository, newID func() string, cleanup func(string)) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	job := &domain.BacktestJob{
		ID: newID(), From: now.Add(-time.Hour), To: now,
		CustomerIDs: []string{"customer-1"}, BaselineRuleSetID: "active",
		CandidateRuleSetID: "candidate", SnapshotAt: now,
	}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("Create: %v", err)
	}
	cleanup(job.ID)

	queued, err := repo.Retry(ctx, job.ID)
	if err != nil || queued.Status != domain.BacktestJobQueued || queued.RetryCount != 0 {
		t.Fatalf("Retry queued = %+v, %v; want idempotent queued job", queued, err)
	}
	claimed, err := repo.ClaimNext(ctx)
	if err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("ClaimNext = %+v, %v", claimed, err)
	}
	running, err := repo.Retry(ctx, job.ID)
	if err != nil || running.Status != domain.BacktestJobRunning || running.RetryCount != 0 {
		t.Fatalf("Retry running = %+v, %v; want idempotent running job", running, err)
	}
	if err := repo.UpdateProgress(ctx, job.ID, 2, 4, nil); err != nil {
		t.Fatalf("UpdateProgress: %v", err)
	}
	if err := repo.Fail(ctx, job.ID, "execution failed; retry is available"); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	retried, err := repo.Retry(ctx, job.ID)
	if err != nil {
		t.Fatalf("Retry failed job: %v", err)
	}
	if retried.Status != domain.BacktestJobQueued || retried.RetryCount != 1 || retried.Error != "" ||
		retried.ProcessedCustomers != 0 || retried.TotalCustomers != 0 || retried.Progress != 0 ||
		retried.StartedAt != nil || retried.CompletedAt != nil || retried.ETASeconds != nil ||
		retried.Baseline != nil || retried.Candidate != nil || retried.Delta != nil || retried.OutcomeAnalysis != nil {
		t.Fatalf("retried job was not reset: %+v", retried)
	}

	again, err := repo.Retry(ctx, job.ID)
	if err != nil || again.RetryCount != 1 || again.Status != domain.BacktestJobQueued {
		t.Fatalf("duplicate Retry = %+v, %v; want unchanged retry_count", again, err)
	}

	missingID := newID()
	if _, err := repo.Retry(ctx, missingID); err == nil {
		t.Fatal("Retry missing job returned nil error")
	} else {
		var notFound *domain.ErrNotFound
		if !errors.As(err, &notFound) {
			t.Fatalf("Retry missing error = %v, want *domain.ErrNotFound", err)
		}
	}
}

func TestMemoryBacktestJobRepoRetryLifecycle(t *testing.T) {
	sequence := 0
	testBacktestRetryLifecycle(t, NewMemoryBacktestJobRepo(), func() string {
		sequence++
		return "memory-retry-" + string(rune('a'+sequence))
	}, func(string) {})
}

func TestPostgresBacktestJobRepoRetryLifecycle(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgBacktestJobRepo(pool)
	testBacktestRetryLifecycle(t, repo, func() string {
		raw := newTestUUID()
		return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
	}, func(id string) {
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM backtest_jobs WHERE id=$1`, id) })
	})
}

func testExpireQueuedBacktests(t *testing.T, repo domain.BacktestJobRepository, newID func() string, cleanup func(string)) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	queued := &domain.BacktestJob{ID: newID(), From: now.Add(-time.Hour), To: now, BaselineRuleSetID: "active", CandidateRuleSetID: "candidate", SnapshotAt: now}
	running := &domain.BacktestJob{ID: newID(), From: now.Add(-time.Hour), To: now, BaselineRuleSetID: "active", CandidateRuleSetID: "candidate", SnapshotAt: now}
	for _, job := range []*domain.BacktestJob{queued, running} {
		if err := repo.Create(ctx, job); err != nil {
			t.Fatalf("Create %s: %v", job.ID, err)
		}
		cleanup(job.ID)
	}
	claimed, err := repo.ClaimNext(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("ClaimNext first = %+v, %v", claimed, err)
	}
	queuedID := queued.ID
	if claimed.ID == queued.ID {
		queuedID = running.ID
	} else if claimed.ID != running.ID {
		t.Fatalf("ClaimNext returned unknown job = %+v", claimed)
	}
	// The first job is running. The second remains queued and is older than a
	// deliberately future cutoff, so only the queued job may expire.
	expired, err := repo.ExpireQueued(ctx, now.Add(time.Hour), "worker unavailable before queue deadline")
	if err != nil {
		t.Fatalf("ExpireQueued: %v", err)
	}
	if len(expired) != 1 || expired[0] != queuedID {
		t.Fatalf("expired = %v, want [%s]", expired, queuedID)
	}
	gotQueued, err := repo.Get(ctx, queuedID)
	if err != nil || gotQueued.Status != domain.BacktestJobFailed || gotQueued.Error != "worker unavailable before queue deadline" {
		t.Fatalf("expired job = %+v, %v", gotQueued, err)
	}
	gotRunning, err := repo.Get(ctx, claimed.ID)
	if err != nil || gotRunning.Status != domain.BacktestJobRunning {
		t.Fatalf("running job changed = %+v, %v", gotRunning, err)
	}
	second, err := repo.ExpireQueued(ctx, now.Add(time.Hour), "worker unavailable before queue deadline")
	if err != nil || len(second) != 0 {
		t.Fatalf("second ExpireQueued = %v, %v; want no-op", second, err)
	}
}

func TestMemoryBacktestJobRepoExpiresOnlyQueuedJobs(t *testing.T) {
	sequence := 0
	testExpireQueuedBacktests(t, NewMemoryBacktestJobRepo(), func() string {
		sequence++
		return "memory-expire-" + string(rune('a'+sequence))
	}, func(string) {})
}

func TestPostgresBacktestJobRepoExpiresOnlyQueuedJobs(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgBacktestJobRepo(pool)
	testExpireQueuedBacktests(t, repo, func() string {
		raw := newTestUUID()
		return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
	}, func(id string) {
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM backtest_jobs WHERE id=$1`, id) })
	})
}

func TestPostgresBacktestJobRepoReclaimsExpiredRunningLease(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgBacktestJobRepo(pool)
	ctx := context.Background()
	now := time.Now().UTC()
	raw := newTestUUID()
	jobID := raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
	job := &domain.BacktestJob{ID: jobID, From: now.Add(-time.Hour), To: now, BaselineRuleSetID: "active", CandidateRuleSetID: "candidate", SnapshotAt: now}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM backtest_jobs WHERE id=$1`, jobID) })
	first, err := repo.ClaimNext(ctx)
	if err != nil || first == nil || first.ID != jobID {
		t.Fatalf("first claim=%+v err=%v", first, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE backtest_jobs SET lease_expires_at=now()-interval '1 second' WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := repo.ClaimNext(ctx)
	if err != nil || reclaimed == nil || reclaimed.ID != jobID || reclaimed.Status != domain.BacktestJobRunning {
		t.Fatalf("reclaimed=%+v err=%v", reclaimed, err)
	}
	if reclaimed.StartedAt == nil || first.StartedAt == nil || !reclaimed.StartedAt.Equal(*first.StartedAt) {
		t.Fatalf("started_at changed across reclaim: first=%v reclaimed=%v", first.StartedAt, reclaimed.StartedAt)
	}
}

func TestPostgresBacktestJobRepoConcurrentRetryIncrementsOnce(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgBacktestJobRepo(pool)
	ctx := context.Background()
	now := time.Now().UTC()
	raw := newTestUUID()
	jobID := raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
	job := &domain.BacktestJob{ID: jobID, From: now.Add(-time.Hour), To: now, BaselineRuleSetID: "active", CandidateRuleSetID: "candidate", SnapshotAt: now}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM backtest_jobs WHERE id=$1`, jobID) })
	if _, err := repo.ClaimNext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.Fail(ctx, jobID, "failed"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Retry(ctx, jobID)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Retry: %v", err)
		}
	}
	got, err := repo.Get(ctx, jobID)
	if err != nil || got.Status != domain.BacktestJobQueued || got.RetryCount != 1 {
		t.Fatalf("job=%+v err=%v", got, err)
	}
}

func TestPostgresBacktestJobRepoRetryClearsPartialResults(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgBacktestJobRepo(pool)
	ctx := context.Background()
	now := time.Now().UTC()
	raw := newTestUUID()
	jobID := raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
	job := &domain.BacktestJob{ID: jobID, From: now.Add(-time.Hour), To: now, BaselineRuleSetID: "active", CandidateRuleSetID: "candidate", SnapshotAt: now}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM backtest_jobs WHERE id=$1`, jobID) })
	if _, err := repo.ClaimNext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.Fail(ctx, jobID, "failed"); err != nil {
		t.Fatal(err)
	}
	customerID := newTestUUID()
	if _, err := pool.Exec(ctx, `INSERT INTO backtest_job_affected_customers(job_id,scenario_id,customer_id,delta_kind) VALUES($1,'scenario',$2,'added')`, jobID, customerID); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveBacktestOutcomeAnalysis(ctx, jobID, &domain.BacktestOutcomeAnalysis{MatcherVersion: "v1", SnapshotAt: now}, []domain.BacktestOutcomeDetail{{ID: "detail-" + newTestUUID(), JobID: jobID, Variant: domain.OutcomeVariantCandidate, CandidateID: "candidate", CustomerID: customerID, Label: "unlabeled", MatcherVersion: "v1", SnapshotAt: now}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Retry(ctx, jobID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"backtest_job_affected_customers", "backtest_outcome_details"} {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE job_id=$1`, jobID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	got, err := repo.Get(ctx, jobID)
	if err != nil || got.OutcomeAnalysis != nil {
		t.Fatalf("job=%+v err=%v", got, err)
	}
}
