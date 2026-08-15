// Package coverage runs the known-matter coverage analysis as a durable job.
// Loading source rows is injected so the same matcher can be used by the API,
// a worker, and deterministic unit tests without a second matching algorithm.
package coverage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/outcome"
)

type Dependencies struct {
	Repository domain.CoverageAnalysisRepository
	Clock      func() time.Time
	Load       func(context.Context, *domain.CoverageAnalysis) ([]outcome.Detection, []outcome.Reference, error)
}

type Service struct {
	repository domain.CoverageAnalysisRepository
	clock      func() time.Time
	load       func(context.Context, *domain.CoverageAnalysis) ([]outcome.Detection, []outcome.Reference, error)
	runMu      sync.Mutex
}

func NewService(deps Dependencies) *Service {
	clock := deps.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Service{repository: deps.Repository, clock: clock, load: deps.Load}
}

func (s *Service) Repository() domain.CoverageAnalysisRepository {
	if s == nil {
		return nil
	}
	return s.repository
}

func (s *Service) Create(ctx context.Context, analysis *domain.CoverageAnalysis) (*domain.CoverageAnalysis, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("coverage analysis repository is not configured")
	}
	if analysis == nil {
		return nil, fmt.Errorf("coverage analysis is required")
	}
	if analysis.ID == "" {
		analysis.ID = newID()
	}
	if analysis.Kind != "" && analysis.Kind != domain.CoverageAnalysisKind {
		return nil, fmt.Errorf("unsupported coverage analysis kind: %s", analysis.Kind)
	}
	if analysis.Kind == "" {
		analysis.Kind = domain.CoverageAnalysisKind
	}
	if analysis.MatcherVersion == "" {
		analysis.MatcherVersion = outcome.MatcherVersion
	}
	if analysis.SnapshotAt.IsZero() {
		analysis.SnapshotAt = s.clock().UTC()
	}
	if len(analysis.Assumptions) == 0 {
		analysis.Assumptions = []string{"known matter is derived from durable case/STR/alert records", "coverage reports internal known matter only"}
	}
	if err := s.repository.CreateCoverageAnalysis(ctx, analysis); err != nil {
		return nil, err
	}
	return analysis, nil
}

func (s *Service) Get(ctx context.Context, id string) (*domain.CoverageAnalysis, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("coverage analysis repository is not configured")
	}
	return s.repository.GetCoverageAnalysis(ctx, id)
}

func (s *Service) List(ctx context.Context, filter domain.CoverageAnalysisFilter) ([]domain.CoverageAnalysis, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("coverage analysis repository is not configured")
	}
	return s.repository.ListCoverageAnalyses(ctx, filter)
}

func (s *Service) Matters(ctx context.Context, filter domain.CoverageMatterFilter) ([]domain.CoverageMatterResult, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("coverage analysis repository is not configured")
	}
	return s.repository.ListCoverageMatterResults(ctx, filter)
}

// Analyze executes one loaded job. The input detections are candidate alerts
// produced by the comparison; known matters are passed as references so each
// matter receives an explicit covered/not-covered result.
func (s *Service) Analyze(ctx context.Context, analysis *domain.CoverageAnalysis, candidates []outcome.Detection, matters []outcome.Reference) (*domain.CoverageAnalysis, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("coverage analysis repository is not configured")
	}
	if analysis == nil {
		return nil, fmt.Errorf("coverage analysis is required")
	}
	started, err := s.repository.StartCoverageAnalysis(ctx, analysis.ID)
	if err != nil {
		return nil, err
	}
	if started.Status == domain.CoverageAnalysisCompleted {
		return started, nil
	}
	if started.Status != domain.CoverageAnalysisRunning {
		return nil, fmt.Errorf("coverage analysis is not runnable: %s", started.Status)
	}
	result := outcome.MatchAlerts(mattersAsDetections(matters), referencesFromCandidates(candidates), outcome.Options{Mode: outcome.ModeCoverage, SnapshotAt: analysis.SnapshotAt})
	matterByID := make(map[string]outcome.Reference, len(matters))
	for _, matter := range matters {
		matterByID[matter.ID] = matter
	}
	rows := make([]domain.CoverageMatterResult, 0, len(result.Evaluations))
	byScenario := map[string]domain.CoverageSummary{}
	summary := domain.CoverageSummary{KnownMatter: len(result.Evaluations), Denominator: result.Denominator}
	for _, evaluation := range result.Evaluations {
		matter := matterByID[evaluation.CandidateID]
		covered := evaluation.Match != nil && evaluation.Label != outcome.LabelUnevaluable
		unevaluable := evaluation.Label == outcome.LabelUnevaluable
		if covered {
			summary.Covered++
		} else if !unevaluable {
			summary.NotCovered++
		}
		if unevaluable {
			summary.Unevaluable++
		}
		scenarioIDs := nonEmptyScenarioIDs(matter.ScenarioID)
		row := domain.CoverageMatterResult{ID: newID(), AnalysisID: analysis.ID, MatterID: matter.ID, CustomerID: matter.CustomerID, ScenarioIDs: scenarioIDs, Source: matter.Provenance["source"], Label: string(evaluation.Label), Covered: covered, Unevaluable: unevaluable, MatcherVersion: result.MatcherVersion, Assumptions: append([]string(nil), result.Assumptions...), SnapshotAt: result.SnapshotAt, Provenance: cloneMap(matter.Provenance), CreatedAt: s.clock().UTC()}
		if evaluation.Match != nil {
			row.MatchedAlertID = evaluation.ReferenceID
			row.ScenarioIDs = nonEmptyScenarioIDs(evaluation.Match.ScenarioID)
		}
		rows = append(rows, row)
	}
	summary.Denominator = summary.KnownMatter - summary.Unevaluable
	summary.Rate = safeRate(summary.Covered, summary.Denominator)
	for _, scenario := range coverageScenarios(analysis.ScenarioIDs, candidates) {
		filtered := make([]outcome.Detection, 0, len(candidates))
		for _, candidate := range candidates {
			if candidate.ScenarioID == scenario {
				filtered = append(filtered, candidate)
			}
		}
		scenarioResult := outcome.MatchAlerts(mattersAsDetections(matters), referencesFromCandidates(filtered), outcome.Options{Mode: outcome.ModeCoverage, SnapshotAt: analysis.SnapshotAt})
		byScenario[scenario] = coverageSummary(scenarioResult)
	}
	if err := s.repository.SaveCoverageMatterResults(ctx, analysis.ID, rows); err != nil {
		_ = s.repository.FailCoverageAnalysis(ctx, analysis.ID, err.Error())
		return nil, err
	}
	if err := s.repository.CompleteCoverageAnalysis(ctx, analysis.ID, summary, byScenario); err != nil {
		_ = s.repository.FailCoverageAnalysis(ctx, analysis.ID, err.Error())
		return nil, err
	}
	completed, err := s.repository.GetCoverageAnalysis(ctx, analysis.ID)
	if err != nil {
		return nil, err
	}
	return completed, nil
}

func (s *Service) RunOnce(ctx context.Context) error {
	if s == nil || s.repository == nil || s.load == nil {
		return fmt.Errorf("coverage analysis worker is not configured")
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	items, err := s.repository.ListCoverageAnalyses(ctx, domain.CoverageAnalysisFilter{Status: domain.CoverageAnalysisQueued, Limit: 1})
	if err != nil || len(items) == 0 {
		return err
	}
	started, err := s.repository.StartCoverageAnalysis(ctx, items[0].ID)
	if err != nil {
		return err
	}
	if started.Status != domain.CoverageAnalysisRunning {
		return nil
	}
	candidates, matters, err := s.load(ctx, started)
	if err != nil {
		_ = s.repository.FailCoverageAnalysis(ctx, started.ID, err.Error())
		return err
	}
	_, err = s.Analyze(ctx, started, candidates, matters)
	return err
}

// Run polls the durable coverage queue. A single process-level mutex keeps
// concurrent ticks from reprocessing the same row; the repository's status
// transition remains the durable recovery boundary across restarts.
func (s *Service) Run(ctx context.Context, poll time.Duration) error {
	if poll <= 0 {
		poll = time.Second
	}
	if s == nil || s.repository == nil || s.load == nil {
		return fmt.Errorf("coverage analysis worker is not configured")
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
				// The job is durably failed by RunOnce when loading fails. Keep
				// polling so a later queued job is not stranded by one bad row.
			}
		}
	}
}

func mattersAsDetections(items []outcome.Reference) []outcome.Detection {
	result := make([]outcome.Detection, 0, len(items))
	for _, item := range items {
		result = append(result, item.Detection)
	}
	return result
}

func referencesFromCandidates(items []outcome.Detection) []outcome.Reference {
	result := make([]outcome.Reference, 0, len(items))
	for _, item := range items {
		result = append(result, outcome.Reference{Detection: item, State: outcome.HistoricalState{AlertStatus: domain.AlertStatusClosedTruePositive, ScoreTier: item.ScoreTier, ScoreTierKnown: item.ScoreTierKnown}, Provenance: map[string]string{"source": "candidate_alert"}})
	}
	return result
}

func nonEmptyScenarioIDs(scenario string) []string {
	if scenario == "" {
		return nil
	}
	return []string{scenario}
}

func safeRate(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func coverageSummary(result outcome.Result) domain.CoverageSummary {
	summary := domain.CoverageSummary{KnownMatter: len(result.Evaluations), Denominator: result.Denominator}
	for _, evaluation := range result.Evaluations {
		switch evaluation.Label {
		case outcome.LabelUnevaluable:
			summary.Unevaluable++
		case outcome.LabelTP:
			if evaluation.Match != nil {
				summary.Covered++
			}
		default:
			if evaluation.Match == nil {
				summary.NotCovered++
			}
		}
	}
	summary.Denominator = summary.KnownMatter - summary.Unevaluable
	summary.Rate = safeRate(summary.Covered, summary.Denominator)
	return summary
}

func coverageScenarios(requested []string, candidates []outcome.Detection) []string {
	seen := map[string]struct{}{}
	for _, scenario := range requested {
		if scenario != "" {
			seen[scenario] = struct{}{}
		}
	}
	if len(seen) == 0 {
		for _, candidate := range candidates {
			if candidate.ScenarioID != "" {
				seen[candidate.ScenarioID] = struct{}{}
			}
		}
	}
	scenarios := make([]string, 0, len(seen))
	for scenario := range seen {
		scenarios = append(scenarios, scenario)
	}
	sort.Strings(scenarios)
	return scenarios
}

func cloneMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func newID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("%032x", time.Now().UnixNano())
}
