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
	// affected holds the completed job's per-scenario outcome rows, ordered by
	// (customer_id, scenario_id) the way the PostgreSQL index returns them.
	affected       map[string][]domain.BacktestAffectedCustomer
	outcomeDetails map[string][]domain.BacktestOutcomeDetail
}

func NewMemoryBacktestJobRepo() *MemoryBacktestJobRepo {
	return &MemoryBacktestJobRepo{
		data:      make(map[string]*domain.BacktestJob),
		snapshots: make(map[string][]string),
		affected:  make(map[string][]domain.BacktestAffectedCustomer), outcomeDetails: make(map[string][]domain.BacktestOutcomeDetail),
	}
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
	cp.OutcomeAnalysis = cloneBacktestOutcomeAnalysis(j.OutcomeAnalysis)
	return &cp
}

func cloneBacktestOutcomeAnalysis(in *domain.BacktestOutcomeAnalysis) *domain.BacktestOutcomeAnalysis {
	if in == nil {
		return nil
	}
	out := *in
	out.Assumptions = append([]string(nil), in.Assumptions...)
	if in.ByScenario != nil {
		out.ByScenario = make(map[string]domain.OutcomeSummary, len(in.ByScenario))
		for key, value := range in.ByScenario {
			out.ByScenario[key] = value
		}
	}
	return &out
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
	// Derived through the same domain function the PostgreSQL store uses, so
	// the two cannot disagree about what a result means.
	r.affected[id] = domain.BacktestAffectedCustomersFrom(id, candidate, delta)
	return nil
}

func (r *MemoryBacktestJobRepo) ListBacktestAffectedCustomers(_ context.Context, filter domain.BacktestAffectedCustomerFilter, limit int) ([]domain.BacktestAffectedCustomer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []domain.BacktestAffectedCustomer{}
	// r.affected is already ordered by (customer_id, scenario_id).
	for _, row := range r.affected[filter.JobID] {
		if filter.ScenarioID != "" && row.ScenarioID != filter.ScenarioID {
			continue
		}
		if filter.AfterCustomerID != "" && row.CustomerID <= filter.AfterCustomerID {
			continue
		}
		out = append(out, row)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *MemoryBacktestJobRepo) CountBacktestAffectedCustomers(_ context.Context, filter domain.BacktestAffectedCustomerFilter) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	for _, row := range r.affected[filter.JobID] {
		if filter.ScenarioID != "" && row.ScenarioID != filter.ScenarioID {
			continue
		}
		seen[row.CustomerID] = true
	}
	return len(seen), nil
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

func (r *MemoryBacktestJobRepo) SaveBacktestOutcomeAnalysis(_ context.Context, jobID string, analysis *domain.BacktestOutcomeAnalysis, details []domain.BacktestOutcomeDetail) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.data[jobID]
	if !ok {
		return &domain.ErrNotFound{Entity: "backtest_job", ID: jobID}
	}
	job.OutcomeAnalysis = cloneBacktestOutcomeAnalysis(analysis)
	job.UpdatedAt = time.Now().UTC()
	copyDetails := make([]domain.BacktestOutcomeDetail, len(details))
	for i, detail := range details {
		copyDetails[i] = cloneBacktestOutcomeDetail(detail)
	}
	sort.Slice(copyDetails, func(i, j int) bool {
		if copyDetails[i].Variant != copyDetails[j].Variant {
			return copyDetails[i].Variant < copyDetails[j].Variant
		}
		if copyDetails[i].ScenarioID != copyDetails[j].ScenarioID {
			return copyDetails[i].ScenarioID < copyDetails[j].ScenarioID
		}
		return copyDetails[i].ID < copyDetails[j].ID
	})
	r.outcomeDetails[jobID] = copyDetails
	return nil
}

func (r *MemoryBacktestJobRepo) GetBacktestOutcomeAnalysis(_ context.Context, jobID string) (*domain.BacktestOutcomeAnalysis, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.data[jobID]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "backtest_job", ID: jobID}
	}
	return cloneBacktestOutcomeAnalysis(job.OutcomeAnalysis), nil
}

func (r *MemoryBacktestJobRepo) ListBacktestOutcomeDetails(_ context.Context, filter domain.BacktestOutcomeFilter) ([]domain.BacktestOutcomeDetail, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.data[filter.JobID]; !ok {
		return nil, &domain.ErrNotFound{Entity: "backtest_job", ID: filter.JobID}
	}
	items := make([]domain.BacktestOutcomeDetail, 0)
	for _, detail := range r.outcomeDetails[filter.JobID] {
		if (filter.Variant != "" && detail.Variant != filter.Variant) || (filter.ScenarioID != "" && detail.ScenarioID != filter.ScenarioID) || (filter.Label != "" && detail.Label != filter.Label) {
			continue
		}
		if filter.Cursor != nil && (detail.CreatedAt.Before(filter.Cursor.CreatedAt) || (detail.CreatedAt.Equal(filter.Cursor.CreatedAt) && detail.ID <= filter.Cursor.ID)) {
			continue
		}
		items = append(items, cloneBacktestOutcomeDetail(detail))
		if filter.Limit > 0 && len(items) >= filter.Limit {
			break
		}
	}
	return items, nil
}

func cloneBacktestOutcomeDetail(in domain.BacktestOutcomeDetail) domain.BacktestOutcomeDetail {
	out := in
	out.Assumptions = append([]string(nil), in.Assumptions...)
	if in.Provenance != nil {
		out.Provenance = make(map[string]string, len(in.Provenance))
		for key, value := range in.Provenance {
			out.Provenance[key] = value
		}
	}
	return out
}
