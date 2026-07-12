package store

import (
	"context"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// MemoryBatchRunRepo is the in-memory domain.BatchRunRepository used for
// MERLON_DATABASE_URL-unset development/test runs (WS-5 Task6).
type MemoryBatchRunRepo struct {
	mu   sync.RWMutex
	data map[string]*domain.BatchRun
}

func NewMemoryBatchRunRepo() *MemoryBatchRunRepo {
	return &MemoryBatchRunRepo{data: make(map[string]*domain.BatchRun)}
}

func (r *MemoryBatchRunRepo) Create(_ context.Context, run *domain.BatchRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	cp := *run
	cp.ProcessedCustomerIDs = append([]string(nil), run.ProcessedCustomerIDs...)
	r.data[run.ID] = &cp
	return nil
}

func (r *MemoryBatchRunRepo) Get(_ context.Context, id string) (*domain.BatchRun, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	run, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "batch_run", ID: id}
	}
	cp := *run
	cp.ProcessedCustomerIDs = append([]string(nil), run.ProcessedCustomerIDs...)
	return &cp, nil
}

func (r *MemoryBatchRunRepo) GetLatestRunning(_ context.Context, jobType string) (*domain.BatchRun, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var latest *domain.BatchRun
	for _, run := range r.data {
		if run.JobType != jobType || run.Status != domain.BatchRunStatusRunning {
			continue
		}
		if latest == nil || run.StartedAt.After(latest.StartedAt) {
			latest = run
		}
	}
	if latest == nil {
		return nil, nil
	}
	cp := *latest
	cp.ProcessedCustomerIDs = append([]string(nil), latest.ProcessedCustomerIDs...)
	return &cp, nil
}

func (r *MemoryBatchRunRepo) AppendProcessedCustomer(_ context.Context, id string, customerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "batch_run", ID: id}
	}
	run.ProcessedCustomerIDs = append(run.ProcessedCustomerIDs, customerID)
	return nil
}

func (r *MemoryBatchRunRepo) Complete(_ context.Context, id string) error {
	return r.setStatus(id, domain.BatchRunStatusCompleted)
}

func (r *MemoryBatchRunRepo) Fail(_ context.Context, id string) error {
	return r.setStatus(id, domain.BatchRunStatusFailed)
}

func (r *MemoryBatchRunRepo) setStatus(id string, status domain.BatchRunStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "batch_run", ID: id}
	}
	run.Status = status
	now := time.Now()
	run.CompletedAt = &now
	return nil
}
