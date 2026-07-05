package store

import (
	"context"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

func cleanupBatchRun(t *testing.T, repo *PgBatchRunRepo, id string) {
	t.Helper()
	t.Cleanup(func() {
		repo.pool.Exec(context.Background(), `DELETE FROM batch_runs WHERE id = $1`, id)
	})
}

func TestPostgresBatchRunRepo_CreateAndGet(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgBatchRunRepo(pool)
	ctx := context.Background()

	r := &domain.BatchRun{ID: newTestUUID(), JobType: "tm_batch_evaluation", Status: domain.BatchRunStatusRunning}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	cleanupBatchRun(t, repo, r.ID)

	got, err := repo.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.JobType != "tm_batch_evaluation" || got.Status != domain.BatchRunStatusRunning {
		t.Errorf("Get returned unexpected run: %+v", got)
	}
	if got.StartedAt.IsZero() {
		t.Error("StartedAt should be set")
	}
	if len(got.ProcessedCustomerIDs) != 0 {
		t.Errorf("ProcessedCustomerIDs = %v, want empty", got.ProcessedCustomerIDs)
	}
}

func TestPostgresBatchRunRepo_GetLatestRunningFindsRunningRun(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgBatchRunRepo(pool)
	ctx := context.Background()
	jobType := "tm_batch_evaluation_test_" + newTestUUID()

	done := &domain.BatchRun{ID: newTestUUID(), JobType: jobType, Status: domain.BatchRunStatusRunning}
	if err := repo.Create(ctx, done); err != nil {
		t.Fatalf("Create: %v", err)
	}
	cleanupBatchRun(t, repo, done.ID)
	if err := repo.Complete(ctx, done.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	running := &domain.BatchRun{ID: newTestUUID(), JobType: jobType, Status: domain.BatchRunStatusRunning}
	if err := repo.Create(ctx, running); err != nil {
		t.Fatalf("Create: %v", err)
	}
	cleanupBatchRun(t, repo, running.ID)

	got, err := repo.GetLatestRunning(ctx, jobType)
	if err != nil {
		t.Fatalf("GetLatestRunning: %v", err)
	}
	if got == nil || got.ID != running.ID {
		t.Errorf("GetLatestRunning = %+v, want run %s", got, running.ID)
	}
}

func TestPostgresBatchRunRepo_GetLatestRunningReturnsNilWhenNoneRunning(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgBatchRunRepo(pool)
	ctx := context.Background()
	jobType := "tm_batch_evaluation_test_" + newTestUUID()

	got, err := repo.GetLatestRunning(ctx, jobType)
	if err != nil {
		t.Fatalf("GetLatestRunning: %v", err)
	}
	if got != nil {
		t.Errorf("GetLatestRunning = %+v, want nil", got)
	}
}

func TestPostgresBatchRunRepo_AppendProcessedCustomerAccumulates(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgBatchRunRepo(pool)
	ctx := context.Background()

	r := &domain.BatchRun{ID: newTestUUID(), JobType: "tm_batch_evaluation", Status: domain.BatchRunStatusRunning}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	cleanupBatchRun(t, repo, r.ID)

	cust1, cust2 := newTestUUID(), newTestUUID()
	if err := repo.AppendProcessedCustomer(ctx, r.ID, cust1); err != nil {
		t.Fatalf("AppendProcessedCustomer: %v", err)
	}
	if err := repo.AppendProcessedCustomer(ctx, r.ID, cust2); err != nil {
		t.Fatalf("AppendProcessedCustomer: %v", err)
	}

	got, err := repo.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.ProcessedCustomerIDs) != 2 || got.ProcessedCustomerIDs[0] != cust1 || got.ProcessedCustomerIDs[1] != cust2 {
		t.Errorf("ProcessedCustomerIDs = %v, want [%s %s]", got.ProcessedCustomerIDs, cust1, cust2)
	}
}

func TestPostgresBatchRunRepo_CompleteAndFail(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgBatchRunRepo(pool)
	ctx := context.Background()

	r1 := &domain.BatchRun{ID: newTestUUID(), JobType: "tm_batch_evaluation", Status: domain.BatchRunStatusRunning}
	if err := repo.Create(ctx, r1); err != nil {
		t.Fatalf("Create: %v", err)
	}
	cleanupBatchRun(t, repo, r1.ID)
	if err := repo.Complete(ctx, r1.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	got1, err := repo.Get(ctx, r1.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got1.Status != domain.BatchRunStatusCompleted || got1.CompletedAt == nil {
		t.Errorf("after Complete: %+v", got1)
	}

	r2 := &domain.BatchRun{ID: newTestUUID(), JobType: "tm_batch_evaluation", Status: domain.BatchRunStatusRunning}
	if err := repo.Create(ctx, r2); err != nil {
		t.Fatalf("Create: %v", err)
	}
	cleanupBatchRun(t, repo, r2.ID)
	if err := repo.Fail(ctx, r2.ID); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	got2, err := repo.Get(ctx, r2.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got2.Status != domain.BatchRunStatusFailed || got2.CompletedAt == nil {
		t.Errorf("after Fail: %+v", got2)
	}
}
