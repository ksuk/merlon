package batch

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

func TestScheduler_DefaultRunTimeIs0200(t *testing.T) {
	s := NewScheduler(DefaultTMBatchSchedule, func(context.Context, string) error { return nil })
	if s.hour != 2 || s.minute != 0 {
		t.Errorf("hour=%d minute=%d, want 2:00", s.hour, s.minute)
	}
}

func TestScheduler_InvalidScheduleFallsBackToDefault(t *testing.T) {
	s := NewScheduler("not-a-time", func(context.Context, string) error { return nil })
	if s.hour != 2 || s.minute != 0 {
		t.Errorf("hour=%d minute=%d, want fallback 2:00", s.hour, s.minute)
	}
}

func TestScheduler_DurationUntilNextWithinSameDay(t *testing.T) {
	s := NewScheduler("14:30", func(context.Context, string) error { return nil })
	s.Location = time.UTC
	fixedNow := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return fixedNow }

	got := s.durationUntilNext()
	want := 4*time.Hour + 30*time.Minute
	if got != want {
		t.Errorf("durationUntilNext = %v, want %v", got, want)
	}
}

func TestScheduler_DurationUntilNextRollsOverToTomorrow(t *testing.T) {
	s := NewScheduler("02:00", func(context.Context, string) error { return nil })
	s.Location = time.UTC
	fixedNow := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return fixedNow }

	got := s.durationUntilNext()
	want := 16 * time.Hour
	if got != want {
		t.Errorf("durationUntilNext = %v, want %v", got, want)
	}
}

func TestScheduler_RunNowInvokesJobWithFreshRunID(t *testing.T) {
	var gotRunID string
	var calls int
	s := NewScheduler(DefaultTMBatchSchedule, func(_ context.Context, runID string) error {
		calls++
		gotRunID = runID
		return nil
	})

	runID, err := s.RunNow(context.Background())
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if calls != 1 {
		t.Fatalf("job called %d times, want 1", calls)
	}
	if gotRunID != runID || runID == "" {
		t.Errorf("RunNow returned %q, job saw %q", runID, gotRunID)
	}
}

// TestScheduler_KilledMidwayResumesUnprocessedOnly simulates a batch that was
// killed after processing only some customers: a batch_runs row is left
// behind with status=running and a partial ProcessedCustomerIDs list.
// Re-invoking the resumable job runner must skip those already recorded and
// process only the rest (the operational design §4.4 バッチジョブ障害復旧).
func TestScheduler_KilledMidwayResumesUnprocessedOnly(t *testing.T) {
	runs := store.NewMemoryBatchRunRepo()
	ctx := context.Background()
	jobType := "test_job"

	killedRunID := "killed-run-1"
	if err := runs.Create(ctx, &domain.BatchRun{ID: killedRunID, JobType: jobType, Status: domain.BatchRunStatusRunning}); err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	if err := runs.AppendProcessedCustomer(ctx, killedRunID, "cust1"); err != nil {
		t.Fatalf("seed AppendProcessedCustomer: %v", err)
	}

	runID, alreadyProcessed, err := ResumeOrCreateRun(ctx, runs, jobType, "brand-new-run-id")
	if err != nil {
		t.Fatalf("ResumeOrCreateRun: %v", err)
	}
	if runID != killedRunID {
		t.Fatalf("runID = %s, want resumed run %s", runID, killedRunID)
	}
	if !alreadyProcessed["cust1"] {
		t.Errorf("alreadyProcessed should contain cust1: %v", alreadyProcessed)
	}

	var processed []string
	customers := []domain.Customer{{ID: "cust1"}, {ID: "cust2"}, {ID: "cust3"}}
	err = ProcessCustomersResumably(ctx, runs, runID, customers, alreadyProcessed, func(_ context.Context, c *domain.Customer) error {
		processed = append(processed, c.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessCustomersResumably: %v", err)
	}

	if len(processed) != 2 || processed[0] != "cust2" || processed[1] != "cust3" {
		t.Errorf("processed = %v, want [cust2 cust3] (cust1 already done)", processed)
	}

	got, err := runs.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := []string{"cust1", "cust2", "cust3"}
	if len(got.ProcessedCustomerIDs) != len(want) {
		t.Fatalf("ProcessedCustomerIDs = %v, want %v", got.ProcessedCustomerIDs, want)
	}
	for i, id := range want {
		if got.ProcessedCustomerIDs[i] != id {
			t.Errorf("ProcessedCustomerIDs[%d] = %s, want %s", i, got.ProcessedCustomerIDs[i], id)
		}
	}
}

func TestScheduler_ResumeOrCreateRunStartsFreshWhenNoneRunning(t *testing.T) {
	runs := store.NewMemoryBatchRunRepo()
	ctx := context.Background()

	runID, alreadyProcessed, err := ResumeOrCreateRun(ctx, runs, "test_job", "candidate-id")
	if err != nil {
		t.Fatalf("ResumeOrCreateRun: %v", err)
	}
	if runID != "candidate-id" {
		t.Errorf("runID = %s, want candidate-id (fresh run)", runID)
	}
	if len(alreadyProcessed) != 0 {
		t.Errorf("alreadyProcessed = %v, want empty", alreadyProcessed)
	}

	got, err := runs.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.BatchRunStatusRunning {
		t.Errorf("Status = %s, want running", got.Status)
	}
}

// TestScheduler_SnapshotIngestedAtBeforeBatchStart verifies that
// SnapshotBefore excludes transactions ingested (CreatedAt) at or after the
// batch start time, so transactions arriving mid-run are left for the next
// batch instead of being evaluated twice or racing with the current run
// (the transaction-monitoring design「バッチ実行中に到着した新規取引は次回バッチの対象とする」).
func TestScheduler_SnapshotIngestedAtBeforeBatchStart(t *testing.T) {
	batchStart := time.Date(2026, 7, 5, 2, 0, 0, 0, time.UTC)

	before := domain.Transaction{ID: "before", CreatedAt: batchStart.Add(-time.Second)}
	atStart := domain.Transaction{ID: "at-start", CreatedAt: batchStart}
	after := domain.Transaction{ID: "after", CreatedAt: batchStart.Add(time.Second)}

	got := SnapshotBefore([]domain.Transaction{before, atStart, after}, batchStart)

	if len(got) != 1 || got[0].ID != "before" {
		t.Errorf("SnapshotBefore = %v, want only [before]", got)
	}
}
