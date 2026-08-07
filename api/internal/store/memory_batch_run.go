package store

import (
	"context"
	"sort"
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
	if key, ok := run.Parameters["idempotency_key"].(string); ok && key != "" {
		for _, existing := range r.data {
			if existing.Operation == run.Operation {
				if existingKey, ok := existing.Parameters["idempotency_key"].(string); ok && existingKey == key {
					return &domain.ErrConflict{Entity: "batch_run", ID: run.ID, Reason: "idempotency key already used"}
				}
			}
		}
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	cp := *run
	cp.ProcessedCustomerIDs = append([]string(nil), run.ProcessedCustomerIDs...)
	if cp.Operation == "" {
		cp.Operation = cp.JobType
	}
	if cp.UpdatedAt.IsZero() {
		cp.UpdatedAt = cp.StartedAt
	}
	cp.Parameters = copyAnyMap(cp.Parameters)
	cp.ConfigDigests = copyStringMap(cp.ConfigDigests)
	cp.ResultCounts = copyIntMap(cp.ResultCounts)
	cp.CustomerOutcomes = cloneBatchCustomerOutcomes(cp.CustomerOutcomes)
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
	cp.Parameters = copyAnyMap(run.Parameters)
	cp.ConfigDigests = copyStringMap(run.ConfigDigests)
	cp.ResultCounts = copyIntMap(run.ResultCounts)
	cp.CustomerOutcomes = cloneBatchCustomerOutcomes(run.CustomerOutcomes)
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
	cp.CustomerOutcomes = cloneBatchCustomerOutcomes(latest.CustomerOutcomes)
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

func (r *MemoryBatchRunRepo) AppendProcessedCustomerIfAbsent(_ context.Context, id string, customerID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.data[id]
	if !ok {
		return false, &domain.ErrNotFound{Entity: "batch_run", ID: id}
	}
	canonical := domain.CanonicalIdentifier(customerID)
	for _, existing := range run.ProcessedCustomerIDs {
		if domain.CanonicalIdentifier(existing) == canonical {
			return false, nil
		}
	}
	run.ProcessedCustomerIDs = append(run.ProcessedCustomerIDs, customerID)
	return true, nil
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
	run.UpdatedAt = now
	return nil
}

func copyIntMap(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (r *MemoryBatchRunRepo) ListBatchRuns(_ context.Context, filter domain.BatchRunFilter, limit int) ([]domain.BatchRun, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := []domain.BatchRun{}
	for _, run := range r.data {
		if filter.Operation != "" && run.Operation != filter.Operation {
			continue
		}
		if filter.Status != "" && run.Status != filter.Status {
			continue
		}
		if filter.Cursor != nil && !(run.StartedAt.Before(filter.Cursor.CreatedAt) || (run.StartedAt.Equal(filter.Cursor.CreatedAt) && run.ID < filter.Cursor.ID)) {
			continue
		}
		cp := *run
		cp.ProcessedCustomerIDs = append([]string(nil), run.ProcessedCustomerIDs...)
		cp.Parameters = copyAnyMap(run.Parameters)
		cp.ConfigDigests = copyStringMap(run.ConfigDigests)
		cp.ResultCounts = copyIntMap(run.ResultCounts)
		cp.CustomerOutcomes = cloneBatchCustomerOutcomes(run.CustomerOutcomes)
		all = append(all, cp)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].StartedAt.After(all[j].StartedAt) || (all[i].StartedAt.Equal(all[j].StartedAt) && all[i].ID > all[j].ID)
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}
func (r *MemoryBatchRunRepo) UpdateBatchRun(_ context.Context, id string, status domain.BatchRunStatus, resultCounts map[string]int, failure string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "batch_run", ID: id}
	}
	// A terminal run may have its counts refreshed by the worker that was
	// still finishing when it was cancelled, but its verdict is settled: a
	// completed run must never later read as failed, or a cancelled one as
	// completed, because the audit trail already records the first answer.
	if isTerminalBatchRunStatus(run.Status) && run.Status != status {
		return &domain.ErrConflict{Entity: "batch_run", ID: id, Reason: "run is already " + string(run.Status)}
	}
	run.Status = status
	run.ResultCounts = copyIntMap(resultCounts)
	run.Error = failure
	now := time.Now().UTC()
	run.UpdatedAt = now
	if status != domain.BatchRunStatusRunning {
		run.CompletedAt = &now
	}
	return nil
}

// isTerminalBatchRunStatus reports whether a run has reached a state it can
// never leave. cancelled is terminal alongside completed/failed/partial: an
// operator stopped it, and restarting is a new run, not a continuation.
func isTerminalBatchRunStatus(status domain.BatchRunStatus) bool {
	switch status {
	case domain.BatchRunStatusCompleted, domain.BatchRunStatusFailed,
		domain.BatchRunStatusPartial, domain.BatchRunStatusCancelled:
		return true
	}
	return false
}

func (r *MemoryBatchRunRepo) RecordBatchRunOutcome(_ context.Context, runID string, outcome domain.BatchRunCustomerOutcome) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.data[runID]
	if !ok {
		return &domain.ErrNotFound{Entity: "batch_run", ID: runID}
	}
	if run.CustomerOutcomes == nil {
		run.CustomerOutcomes = make(map[string]domain.BatchRunCustomerOutcome)
	}
	key := domain.CanonicalIdentifier(outcome.CustomerID)
	previous := run.CustomerOutcomes[key]
	if outcome.Attempt <= previous.Attempt {
		outcome.Attempt = previous.Attempt + 1
	}
	outcome.CustomerID = key
	outcome.AlertIDs = append([]string(nil), outcome.AlertIDs...)
	if outcome.UpdatedAt.IsZero() {
		outcome.UpdatedAt = time.Now().UTC()
	}
	run.CustomerOutcomes[key] = outcome
	run.UpdatedAt = outcome.UpdatedAt
	return nil
}

func cloneBatchCustomerOutcomes(input map[string]domain.BatchRunCustomerOutcome) map[string]domain.BatchRunCustomerOutcome {
	if input == nil {
		return nil
	}
	output := make(map[string]domain.BatchRunCustomerOutcome, len(input))
	for key, value := range input {
		value.AlertIDs = append([]string(nil), value.AlertIDs...)
		output[key] = value
	}
	return output
}

func (r *MemoryBatchRunRepo) FindBatchRunByIdempotency(_ context.Context, operation, key string) (*domain.BatchRun, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if key == "" {
		return nil, nil
	}
	for _, run := range r.data {
		if run.Operation == operation {
			if v, ok := run.Parameters["idempotency_key"].(string); ok && v == key {
				cp := *run
				return &cp, nil
			}
		}
	}
	return nil, nil
}
