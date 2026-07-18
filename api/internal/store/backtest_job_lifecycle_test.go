package store

import (
	"context"
	"errors"
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
