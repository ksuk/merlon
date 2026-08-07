package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// The durable manual-batch repository shipped with no test at all:
// RecordBatchRunOutcome, FindBatchRunByIdempotency, UpdateBatchRun,
// ListBatchRuns and AppendProcessedCustomerIfAbsent had no coverage, so
// nothing pinned the behaviour a resumed or retried run depends on.

func seedBatchRun(t *testing.T, repo *MemoryBatchRunRepo, id, operation string, params map[string]any) *domain.BatchRun {
	t.Helper()
	run := &domain.BatchRun{
		ID: id, JobType: operation, Operation: operation, Status: domain.BatchRunStatusRunning,
		Parameters: params, ConfigDigests: map[string]string{"cdd_weights": "sha256:abc"},
		Actor: "operator-1", StartedAt: time.Now().UTC(),
	}
	if err := repo.Create(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestMemoryBatchRunUpdateRefusesTerminalToTerminal(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryBatchRunRepo()
	run := seedBatchRun(t, repo, "run-terminal", "score", nil)

	if err := repo.UpdateBatchRun(ctx, run.ID, domain.BatchRunStatusCompleted, map[string]int{"succeeded": 3}, ""); err != nil {
		t.Fatal(err)
	}
	// The same status again is a refresh, not a change: the worker that was
	// still finishing when the run was settled may report its final counts.
	if err := repo.UpdateBatchRun(ctx, run.ID, domain.BatchRunStatusCompleted, map[string]int{"succeeded": 4}, ""); err != nil {
		t.Fatalf("idempotent refresh rejected: %v", err)
	}
	for _, status := range []domain.BatchRunStatus{domain.BatchRunStatusFailed, domain.BatchRunStatusPartial, domain.BatchRunStatusCancelled, domain.BatchRunStatusRunning} {
		err := repo.UpdateBatchRun(ctx, run.ID, status, nil, "")
		var conflict *domain.ErrConflict
		if err == nil || !asConflict(err, &conflict) {
			t.Errorf("completed -> %s = %v, want a conflict", status, err)
		}
	}
	got, err := repo.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.BatchRunStatusCompleted || got.ResultCounts["succeeded"] != 4 {
		t.Fatalf("run = %+v, want completed with the refreshed counts", got)
	}
	if got.CompletedAt == nil {
		t.Fatal("CompletedAt was not stamped on the terminal transition")
	}
}

func asConflict(err error, target **domain.ErrConflict) bool {
	conflict, ok := err.(*domain.ErrConflict)
	if ok {
		*target = conflict
	}
	return ok
}

func TestMemoryBatchRunListFiltersAndPages(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryBatchRunRepo()
	for i := range 5 {
		operation := "score"
		if i%2 == 1 {
			operation = "monitor"
		}
		run := seedBatchRun(t, repo, fmt.Sprintf("run-%02d", i), operation, nil)
		run.StartedAt = time.Date(2026, 8, 1, 0, i, 0, 0, time.UTC)
		if i == 4 {
			if err := repo.UpdateBatchRun(ctx, run.ID, domain.BatchRunStatusFailed, nil, "engine down"); err != nil {
				t.Fatal(err)
			}
		}
	}

	all, err := repo.ListBatchRuns(ctx, domain.BatchRunFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("unfiltered = %d runs, want 5", len(all))
	}
	byOperation, err := repo.ListBatchRuns(ctx, domain.BatchRunFilter{Operation: "monitor"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(byOperation) != 2 {
		t.Fatalf("operation filter = %d, want 2", len(byOperation))
	}
	byStatus, err := repo.ListBatchRuns(ctx, domain.BatchRunFilter{Status: domain.BatchRunStatusFailed}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(byStatus) != 1 {
		t.Fatalf("status filter = %d, want 1", len(byStatus))
	}

	first, err := repo.ListBatchRuns(ctx, domain.BatchRunFilter{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatalf("first page = %d, want 2", len(first))
	}
	last := first[len(first)-1]
	second, err := repo.ListBatchRuns(ctx, domain.BatchRunFilter{Cursor: &domain.Cursor{CreatedAt: last.StartedAt, ID: last.ID}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range second {
		if run.ID == last.ID {
			t.Fatalf("cursor page repeated %s", run.ID)
		}
	}
	if len(first)+len(second) != 5 {
		t.Fatalf("paged %d runs, want 5", len(first)+len(second))
	}
}

func TestMemoryBatchRunFindByIdempotency(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryBatchRunRepo()
	seedBatchRun(t, repo, "run-idem", "score", map[string]any{"idempotency_key": "key-1"})

	found, err := repo.FindBatchRunByIdempotency(ctx, "score", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if found == nil || found.ID != "run-idem" {
		t.Fatalf("found = %+v, want the existing run", found)
	}
	// A different operation with the same key is a different run: the key is
	// scoped to the operation, not global.
	other, err := repo.FindBatchRunByIdempotency(ctx, "monitor", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if other != nil {
		t.Fatalf("cross-operation lookup returned %+v", other)
	}
	if missing, err := repo.FindBatchRunByIdempotency(ctx, "score", "unknown"); err != nil || missing != nil {
		t.Fatalf("unknown key = %+v, %v; want nil, nil", missing, err)
	}
	if blank, err := repo.FindBatchRunByIdempotency(ctx, "score", ""); err != nil || blank != nil {
		t.Fatalf("blank key = %+v, %v; want nil, nil", blank, err)
	}
}

func TestMemoryBatchRunRecordOutcomeRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryBatchRunRepo()
	run := seedBatchRun(t, repo, "run-outcome", "score", nil)

	outcome := domain.BatchRunCustomerOutcome{
		CustomerID: "cust-1", Status: domain.BatchRunCustomerSucceeded,
		AlertIDs: []string{"alert-1", "alert-2"}, Attempt: 1, UpdatedAt: time.Now().UTC(),
	}
	if err := repo.RecordBatchRunOutcome(ctx, run.ID, outcome); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := stored.CustomerOutcomes[domain.CanonicalIdentifier("cust-1")]
	if !ok {
		t.Fatalf("outcomes = %+v, want an entry for cust-1", stored.CustomerOutcomes)
	}
	if got.Status != domain.BatchRunCustomerSucceeded || len(got.AlertIDs) != 2 {
		t.Fatalf("outcome = %+v, want the alert links preserved", got)
	}

	// A retry overwrites the customer's outcome rather than appending a second.
	retry := outcome
	retry.Status = domain.BatchRunCustomerFailed
	retry.Error = "scoring unavailable"
	retry.Attempt = 2
	if err := repo.RecordBatchRunOutcome(ctx, run.ID, retry); err != nil {
		t.Fatal(err)
	}
	stored, err = repo.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.CustomerOutcomes) != 1 {
		t.Fatalf("outcomes = %d, want one per customer", len(stored.CustomerOutcomes))
	}
	if stored.CustomerOutcomes[domain.CanonicalIdentifier("cust-1")].Attempt != 2 {
		t.Fatal("the retry did not replace the earlier outcome")
	}
}

// The checkpoint is what makes a resumed run skip work it already did, so two
// workers claiming the same customer must produce exactly one claim.
func TestMemoryBatchRunAppendProcessedCustomerIfAbsentIsIdempotentUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryBatchRunRepo()
	run := seedBatchRun(t, repo, "run-claim", "score", nil)

	const workers = 8
	var wg sync.WaitGroup
	claims := make(chan bool, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := repo.AppendProcessedCustomerIfAbsent(ctx, run.ID, "cust-9")
			if err != nil {
				t.Error(err)
				return
			}
			claims <- claimed
		}()
	}
	wg.Wait()
	close(claims)
	won := 0
	for claimed := range claims {
		if claimed {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("%d workers claimed the same customer, want exactly 1", won)
	}
	stored, err := repo.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.ProcessedCustomerIDs) != 1 {
		t.Fatalf("checkpoint = %v, want one entry", stored.ProcessedCustomerIDs)
	}
}

func TestMemoryBatchRunPreservesConfigDigests(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryBatchRunRepo()
	run := seedBatchRun(t, repo, "run-digest", "score", nil)

	stored, err := repo.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ConfigDigests["cdd_weights"] != "sha256:abc" {
		t.Fatalf("config_digests = %v, want the pinned digest that makes the run reproducible", stored.ConfigDigests)
	}
	if err := repo.UpdateBatchRun(ctx, run.ID, domain.BatchRunStatusCompleted, map[string]int{"succeeded": 1}, ""); err != nil {
		t.Fatal(err)
	}
	settled, err := repo.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.ConfigDigests["cdd_weights"] != "sha256:abc" {
		t.Fatal("finalising the run dropped its config digests")
	}
}
