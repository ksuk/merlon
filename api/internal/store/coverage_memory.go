package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

type MemoryCoverageAnalysisRepo struct {
	mu      sync.Mutex
	data    map[string]*domain.CoverageAnalysis
	matters map[string][]domain.CoverageMatterResult
}

func NewMemoryCoverageAnalysisRepo() *MemoryCoverageAnalysisRepo {
	return &MemoryCoverageAnalysisRepo{data: map[string]*domain.CoverageAnalysis{}, matters: map[string][]domain.CoverageMatterResult{}}
}

func cloneCoverageAnalysis(in *domain.CoverageAnalysis) *domain.CoverageAnalysis {
	if in == nil {
		return nil
	}
	out := *in
	out.ScenarioIDs = append([]string(nil), in.ScenarioIDs...)
	out.CustomerIDs = append([]string(nil), in.CustomerIDs...)
	out.Assumptions = append([]string(nil), in.Assumptions...)
	if in.ByScenario != nil {
		out.ByScenario = make(map[string]domain.CoverageSummary, len(in.ByScenario))
		for key, value := range in.ByScenario {
			out.ByScenario[key] = value
		}
	}
	return &out
}

func cloneCoverageMatter(in domain.CoverageMatterResult) domain.CoverageMatterResult {
	out := in
	out.ScenarioIDs = append([]string(nil), in.ScenarioIDs...)
	out.Assumptions = append([]string(nil), in.Assumptions...)
	if in.Provenance != nil {
		out.Provenance = make(map[string]string, len(in.Provenance))
		for key, value := range in.Provenance {
			out.Provenance[key] = value
		}
	}
	return out
}

func (r *MemoryCoverageAnalysisRepo) CreateCoverageAnalysis(_ context.Context, analysis *domain.CoverageAnalysis) error {
	if analysis == nil || analysis.ID == "" {
		return &domain.ErrConflict{Entity: "coverage_analysis", Reason: "id is required"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.data[analysis.ID]; exists {
		return &domain.ErrConflict{Entity: "coverage_analysis", ID: analysis.ID, Reason: "analysis already exists"}
	}
	now := time.Now().UTC()
	if analysis.CreatedAt.IsZero() {
		analysis.CreatedAt = now
	}
	if analysis.UpdatedAt.IsZero() {
		analysis.UpdatedAt = analysis.CreatedAt
	}
	if analysis.Kind == "" {
		analysis.Kind = domain.CoverageAnalysisKind
	}
	if analysis.Status == "" {
		analysis.Status = domain.CoverageAnalysisQueued
	}
	r.data[analysis.ID] = cloneCoverageAnalysis(analysis)
	return nil
}

func (r *MemoryCoverageAnalysisRepo) GetCoverageAnalysis(_ context.Context, id string) (*domain.CoverageAnalysis, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	analysis, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "coverage_analysis", ID: id}
	}
	return cloneCoverageAnalysis(analysis), nil
}

func (r *MemoryCoverageAnalysisRepo) ListCoverageAnalyses(_ context.Context, filter domain.CoverageAnalysisFilter) ([]domain.CoverageAnalysis, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*domain.CoverageAnalysis, 0, len(r.data))
	for _, analysis := range r.data {
		if filter.Status != "" && analysis.Status != filter.Status {
			continue
		}
		items = append(items, analysis)
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].ID > items[j].ID
	})
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	if filter.Offset > len(items) {
		filter.Offset = len(items)
	}
	end := len(items)
	if filter.Limit > 0 && filter.Offset+filter.Limit < end {
		end = filter.Offset + filter.Limit
	}
	out := make([]domain.CoverageAnalysis, 0, end-filter.Offset)
	for _, analysis := range items[filter.Offset:end] {
		out = append(out, *cloneCoverageAnalysis(analysis))
	}
	return out, nil
}

func (r *MemoryCoverageAnalysisRepo) StartCoverageAnalysis(_ context.Context, id string) (*domain.CoverageAnalysis, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	analysis, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "coverage_analysis", ID: id}
	}
	if analysis.Status == domain.CoverageAnalysisCompleted || analysis.Status == domain.CoverageAnalysisFailed || analysis.Status == domain.CoverageAnalysisRunning {
		return cloneCoverageAnalysis(analysis), nil
	}
	now := time.Now().UTC()
	analysis.Status, analysis.StartedAt, analysis.UpdatedAt = domain.CoverageAnalysisRunning, &now, now
	return cloneCoverageAnalysis(analysis), nil
}

func (r *MemoryCoverageAnalysisRepo) CompleteCoverageAnalysis(_ context.Context, id string, summary domain.CoverageSummary, byScenario map[string]domain.CoverageSummary) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	analysis, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "coverage_analysis", ID: id}
	}
	now := time.Now().UTC()
	analysis.Status, analysis.Summary, analysis.ByScenario, analysis.CompletedAt, analysis.UpdatedAt = domain.CoverageAnalysisCompleted, summary, byScenario, &now, now
	return nil
}

func (r *MemoryCoverageAnalysisRepo) FailCoverageAnalysis(_ context.Context, id, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	analysis, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "coverage_analysis", ID: id}
	}
	analysis.Status, analysis.Error, analysis.UpdatedAt = domain.CoverageAnalysisFailed, reason, time.Now().UTC()
	return nil
}

func (r *MemoryCoverageAnalysisRepo) SaveCoverageMatterResults(_ context.Context, id string, results []domain.CoverageMatterResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.data[id]; !ok {
		return &domain.ErrNotFound{Entity: "coverage_analysis", ID: id}
	}
	copyResults := make([]domain.CoverageMatterResult, len(results))
	for i, result := range results {
		copyResults[i] = cloneCoverageMatter(result)
	}
	sort.Slice(copyResults, func(i, j int) bool {
		if copyResults[i].CreatedAt.Equal(copyResults[j].CreatedAt) {
			return copyResults[i].ID < copyResults[j].ID
		}
		return copyResults[i].CreatedAt.Before(copyResults[j].CreatedAt)
	})
	r.matters[id] = copyResults
	return nil
}

func (r *MemoryCoverageAnalysisRepo) ListCoverageMatterResults(_ context.Context, filter domain.CoverageMatterFilter) ([]domain.CoverageMatterResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.data[filter.AnalysisID]; !ok {
		return nil, &domain.ErrNotFound{Entity: "coverage_analysis", ID: filter.AnalysisID}
	}
	out := make([]domain.CoverageMatterResult, 0)
	for _, result := range r.matters[filter.AnalysisID] {
		if filter.ScenarioID != "" && !containsString(result.ScenarioIDs, filter.ScenarioID) || filter.Label != "" && result.Label != filter.Label {
			continue
		}
		if filter.Cursor != nil && (result.CreatedAt.Before(filter.Cursor.CreatedAt) || result.CreatedAt.Equal(filter.Cursor.CreatedAt) && result.ID <= filter.Cursor.ID) {
			continue
		}
		out = append(out, cloneCoverageMatter(result))
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

var _ domain.CoverageAnalysisRepository = (*MemoryCoverageAnalysisRepo)(nil)
