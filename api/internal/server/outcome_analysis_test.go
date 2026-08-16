package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/coverage"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/outcome"
	"github.com/ksuk/merlon/api/internal/store"
)

func TestBacktestOutcomesEndpointFiltersDetailAndExposesAnalysis(t *testing.T) {
	jobs := store.NewMemoryBacktestJobRepo()
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	job := &domain.BacktestJob{ID: "job-outcome", Status: domain.BacktestJobCompleted, From: now.Add(-time.Hour), To: now, BaselineRuleSetID: "base", CandidateRuleSetID: "candidate", SnapshotAt: now}
	if err := jobs.Create(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	analysis := &domain.BacktestOutcomeAnalysis{MatcherVersion: outcome.MatcherVersion, SnapshotAt: now, Baseline: domain.OutcomeSummary{TP: 1, Denominator: 1, Rate: 1}}
	if err := jobs.SaveBacktestOutcomeAnalysis(context.Background(), job.ID, analysis, []domain.BacktestOutcomeDetail{{ID: "detail-1", JobID: job.ID, Variant: domain.OutcomeVariantBaseline, CandidateID: "candidate-1", CustomerID: "cust-1", ScenarioID: "scenario-a", Label: "TP", Investigated: true, MatcherVersion: outcome.MatcherVersion, SnapshotAt: now, CreatedAt: now}}); err != nil {
		t.Fatal(err)
	}
	s := New(":0", Deps{BacktestJobs: jobs})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/backtests/"+job.ID+"/outcomes?variant=baseline&scenario_id=scenario-a", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Analysis domain.BacktestOutcomeAnalysis `json:"outcome_analysis"`
		Data     []domain.BacktestOutcomeDetail `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Analysis.MatcherVersion != outcome.MatcherVersion || len(response.Data) != 1 || response.Data[0].Label != "TP" {
		t.Fatalf("response=%#v", response)
	}
}

func TestCoverageAnalysisEndpointsQueueAndListMatters(t *testing.T) {
	repo := store.NewMemoryCoverageAnalysisRepo()
	clock := func() time.Time { return time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC) }
	svc := coverage.NewService(coverage.Dependencies{Repository: repo, Clock: clock})
	s := New(":0", Deps{CoverageAnalyses: svc})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/coverage-analyses", strings.NewReader(`{"scenario_ids":["scenario-a"],"from":"2026-08-01T00:00:00Z","to":"2026-08-15T00:00:00Z"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var analysis domain.CoverageAnalysis
	if err := json.NewDecoder(rec.Body).Decode(&analysis); err != nil {
		t.Fatal(err)
	}
	if analysis.Kind != domain.CoverageAnalysisKind || analysis.MatcherVersion != outcome.MatcherVersion || analysis.Status != domain.CoverageAnalysisQueued {
		t.Fatalf("analysis=%#v", analysis)
	}
	get := httptest.NewRequest(http.MethodGet, "/api/v1/coverage-analyses/"+analysis.ID, nil)
	getRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(getRec, get)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	list := httptest.NewRequest(http.MethodGet, "/api/v1/coverage-analyses", nil)
	listRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(listRec, list)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	matters := httptest.NewRequest(http.MethodGet, "/api/v1/coverage-analyses/"+analysis.ID+"/matters", nil)
	mattersRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(mattersRec, matters)
	if mattersRec.Code != http.StatusOK {
		t.Fatalf("matters status=%d body=%s", mattersRec.Code, mattersRec.Body.String())
	}
}
