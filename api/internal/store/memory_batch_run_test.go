package store

import (
	"context"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestMemoryBatchRunRepo_CreateAndGet(t *testing.T) {
	repo := NewMemoryBatchRunRepo()
	ctx := context.Background()

	r := &domain.BatchRun{ID: newTestUUID(), JobType: "tm_batch_evaluation", Status: domain.BatchRunStatusRunning}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}

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
}

func TestMemoryBatchRunRepo_GetLatestRunningReturnsNilWhenNoneRunning(t *testing.T) {
	repo := NewMemoryBatchRunRepo()
	ctx := context.Background()

	r := &domain.BatchRun{ID: newTestUUID(), JobType: "tm_batch_evaluation", Status: domain.BatchRunStatusRunning}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Complete(ctx, r.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got, err := repo.GetLatestRunning(ctx, "tm_batch_evaluation")
	if err != nil {
		t.Fatalf("GetLatestRunning: %v", err)
	}
	if got != nil {
		t.Errorf("GetLatestRunning = %+v, want nil (no running run)", got)
	}
}

func TestMemoryBatchRunRepo_GetLatestRunningFindsRunningRun(t *testing.T) {
	repo := NewMemoryBatchRunRepo()
	ctx := context.Background()

	done := &domain.BatchRun{ID: newTestUUID(), JobType: "tm_batch_evaluation", Status: domain.BatchRunStatusRunning}
	if err := repo.Create(ctx, done); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Complete(ctx, done.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	running := &domain.BatchRun{ID: newTestUUID(), JobType: "tm_batch_evaluation", Status: domain.BatchRunStatusRunning}
	if err := repo.Create(ctx, running); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetLatestRunning(ctx, "tm_batch_evaluation")
	if err != nil {
		t.Fatalf("GetLatestRunning: %v", err)
	}
	if got == nil || got.ID != running.ID {
		t.Errorf("GetLatestRunning = %+v, want run %s", got, running.ID)
	}
}

func TestMemoryBatchRunRepo_AppendProcessedCustomerAccumulates(t *testing.T) {
	repo := NewMemoryBatchRunRepo()
	ctx := context.Background()

	r := &domain.BatchRun{ID: newTestUUID(), JobType: "tm_batch_evaluation", Status: domain.BatchRunStatusRunning}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.AppendProcessedCustomer(ctx, r.ID, "cust1"); err != nil {
		t.Fatalf("AppendProcessedCustomer: %v", err)
	}
	if err := repo.AppendProcessedCustomer(ctx, r.ID, "cust2"); err != nil {
		t.Fatalf("AppendProcessedCustomer: %v", err)
	}

	got, err := repo.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.ProcessedCustomerIDs) != 2 || got.ProcessedCustomerIDs[0] != "cust1" || got.ProcessedCustomerIDs[1] != "cust2" {
		t.Errorf("ProcessedCustomerIDs = %v, want [cust1 cust2]", got.ProcessedCustomerIDs)
	}
}

func TestMemoryBatchRunRepo_FailSetsStatus(t *testing.T) {
	repo := NewMemoryBatchRunRepo()
	ctx := context.Background()

	r := &domain.BatchRun{ID: newTestUUID(), JobType: "tm_batch_evaluation", Status: domain.BatchRunStatusRunning}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Fail(ctx, r.ID); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	got, err := repo.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.BatchRunStatusFailed {
		t.Errorf("Status = %s, want failed", got.Status)
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt should be set on failure")
	}
}
