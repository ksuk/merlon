package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

type MemoryBacktestJobRepo struct {
	mu        sync.Mutex
	data      map[string]*domain.BacktestJob
	snapshots map[string][]string
}

func NewMemoryBacktestJobRepo() *MemoryBacktestJobRepo {
	return &MemoryBacktestJobRepo{data: make(map[string]*domain.BacktestJob), snapshots: make(map[string][]string)}
}

func cloneBacktestJob(j *domain.BacktestJob) *domain.BacktestJob {
	if j == nil {
		return nil
	}
	cp := *j
	cp.CustomerIDs = append([]string(nil), j.CustomerIDs...)
	cp.ScenarioIDs = append([]string(nil), j.ScenarioIDs...)
	cp.BaselineRuleDefinition = append([]byte(nil), j.BaselineRuleDefinition...)
	cp.CandidateRuleDefinition = append([]byte(nil), j.CandidateRuleDefinition...)
	if j.CustomerFilter != nil {
		f := *j.CustomerFilter
		cp.CustomerFilter = &f
	}
	if j.ConfigDigests != nil {
		cp.ConfigDigests = map[string]string{}
		for k, v := range j.ConfigDigests {
			cp.ConfigDigests[k] = v
		}
	}
	cp.Baseline = cloneBacktestResult(j.Baseline)
	cp.Candidate = cloneBacktestResult(j.Candidate)
	cp.Delta = cloneBacktestResult(j.Delta)
	return &cp
}

func cloneBacktestResult(in *domain.BacktestResult) *domain.BacktestResult {
	if in == nil {
		return nil
	}
	out := *in
	out.ScenarioResults = make([]domain.BacktestScenarioResult, len(in.ScenarioResults))
	for i, scenario := range in.ScenarioResults {
		out.ScenarioResults[i] = scenario
		out.ScenarioResults[i].AffectedCustomerIDs = append([]string(nil), scenario.AffectedCustomerIDs...)
		out.ScenarioResults[i].AddedCustomerIDs = append([]string(nil), scenario.AddedCustomerIDs...)
		out.ScenarioResults[i].RemovedCustomerIDs = append([]string(nil), scenario.RemovedCustomerIDs...)
	}
	return &out
}

func (r *MemoryBacktestJobRepo) Create(_ context.Context, job *domain.BacktestJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	if job.Status == "" {
		job.Status = domain.BacktestJobQueued
	}
	r.data[job.ID] = cloneBacktestJob(job)
	return nil
}
func (r *MemoryBacktestJobRepo) Get(_ context.Context, id string) (*domain.BacktestJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "backtest_job", ID: id}
	}
	return cloneBacktestJob(j), nil
}
func (r *MemoryBacktestJobRepo) List(_ context.Context, limit, offset int) ([]domain.BacktestJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := make([]*domain.BacktestJob, 0, len(r.data))
	for _, j := range r.data {
		all = append(all, j)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	if offset > len(all) {
		offset = len(all)
	}
	end := len(all)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	out := make([]domain.BacktestJob, 0, end-offset)
	for _, j := range all[offset:end] {
		out = append(out, *cloneBacktestJob(j))
	}
	return out, nil
}
func (r *MemoryBacktestJobRepo) Cancel(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "backtest_job", ID: id}
	}
	if j.Status == domain.BacktestJobCompleted || j.Status == domain.BacktestJobFailed {
		return nil
	}
	j.Status = domain.BacktestJobCancelled
	j.UpdatedAt = time.Now().UTC()
	return nil
}
func (r *MemoryBacktestJobRepo) ClaimNext(_ context.Context) (*domain.BacktestJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var chosen *domain.BacktestJob
	for _, j := range r.data {
		if j.Status == domain.BacktestJobQueued && (chosen == nil || j.CreatedAt.Before(chosen.CreatedAt)) {
			chosen = j
		}
	}
	if chosen == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	chosen.Status = domain.BacktestJobRunning
	chosen.StartedAt = &now
	if chosen.SnapshotAt.IsZero() {
		chosen.SnapshotAt = now
	}
	chosen.UpdatedAt = now
	return cloneBacktestJob(chosen), nil
}
func (r *MemoryBacktestJobRepo) UpdateProgress(_ context.Context, id string, processed, total int, eta *int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "backtest_job", ID: id}
	}
	if j.Status != domain.BacktestJobRunning {
		return nil
	}
	j.ProcessedCustomers = processed
	j.TotalCustomers = total
	j.ETASeconds = eta
	if total > 0 {
		j.Progress = float64(processed) / float64(total)
	}
	j.UpdatedAt = time.Now().UTC()
	return nil
}
func (r *MemoryBacktestJobRepo) Complete(_ context.Context, id string, baseline, candidate, delta *domain.BacktestResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "backtest_job", ID: id}
	}
	if j.Status != domain.BacktestJobRunning {
		return nil
	}
	now := time.Now().UTC()
	j.Status = domain.BacktestJobCompleted
	j.Progress = 1
	j.Baseline, j.Candidate, j.Delta = cloneBacktestResult(baseline), cloneBacktestResult(candidate), cloneBacktestResult(delta)
	j.CompletedAt = &now
	j.UpdatedAt = now
	return nil
}
func (r *MemoryBacktestJobRepo) Fail(_ context.Context, id, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "backtest_job", ID: id}
	}
	if j.Status != domain.BacktestJobRunning {
		return nil
	}
	j.Status = domain.BacktestJobFailed
	j.Error = reason
	j.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *MemoryBacktestJobRepo) SaveCustomerSnapshot(_ context.Context, jobID string, customerIDs []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.data[jobID]; !ok {
		return &domain.ErrNotFound{Entity: "backtest_job", ID: jobID}
	}
	r.snapshots[jobID] = append([]string(nil), customerIDs...)
	return nil
}

func (r *MemoryBacktestJobRepo) GetCustomerSnapshot(_ context.Context, jobID string) ([]string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.data[jobID]; !ok {
		return nil, false, &domain.ErrNotFound{Entity: "backtest_job", ID: jobID}
	}
	ids, found := r.snapshots[jobID]
	return append([]string(nil), ids...), found, nil
}
