package coverage

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/outcome"
	"github.com/ksuk/merlon/api/internal/store"
)

func TestNewLoaderAppliesSnapshotAndBuildsCandidateScoreTier(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	customers := store.NewMemoryCustomerRepo()
	if err := customers.Create(ctx, &domain.Customer{ID: "cust-1", CreatedAt: now.Add(-time.Hour), UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := customers.SaveScoreRecord(ctx, &domain.ScoreRecord{ID: "score-1", CustomerID: "cust-1", Tier: domain.RiskTierHigh, ScoredAt: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	alerts := store.NewMemoryAlertRepo()
	if err := alerts.Create(ctx, &domain.Alert{ID: "alert-in", CustomerID: "cust-1", ScenarioID: "scenario-a", TransactionIDs: []string{"tx-1"}, DetectedAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := alerts.Create(ctx, &domain.Alert{ID: "alert-out", CustomerID: "cust-1", ScenarioID: "scenario-a", DetectedAt: now.Add(time.Minute), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	cases := store.NewMemoryCaseRepo()
	reports := store.NewMemorySTRReportRepo()
	loader := NewLoader(LoaderDependencies{Customers: customers, Alerts: alerts, Cases: cases, Reports: reports, AlertDecisions: store.NewMemoryAlertDecisionRepo(), Transactions: store.NewMemoryTransactionRepo(), Engine: &engine.MockBacktestEngine{Detections: []outcome.Detection{{ID: "replay-in", CustomerID: "cust-1", ScenarioID: "scenario-a", DetectedAt: now.Add(-time.Minute)}}}})
	candidates, matters, err := loader(ctx, &domain.CoverageAnalysis{From: now.Add(-24 * time.Hour), To: now, SnapshotAt: now, ScenarioIDs: []string{"scenario-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != "replay-in" || !candidates[0].ScoreTierKnown || candidates[0].ScoreTier != domain.RiskTierHigh {
		t.Fatalf("candidates = %#v", candidates)
	}
	if len(matters) != 0 {
		t.Fatalf("matters = %#v, want no known matter", matters)
	}
}

func TestLoaderUnionThroughAnalyzeProducesNonZeroCoverage(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	customers := store.NewMemoryCustomerRepo()
	if err := customers.Create(ctx, &domain.Customer{ID: "cust-coverage", CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := customers.SaveScoreRecord(ctx, &domain.ScoreRecord{ID: "score-coverage", CustomerID: "cust-coverage", Tier: domain.RiskTierHigh, ScoredAt: now.Add(-2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	alerts := store.NewMemoryAlertRepo()
	alert := &domain.Alert{ID: "alert-covered", CustomerID: "cust-coverage", ScenarioID: "scenario-a", Status: domain.AlertStatusClosedTruePositive, TransactionIDs: []string{"tx-coverage"}, DetectedAt: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}
	if err := alerts.Create(ctx, alert); err != nil {
		t.Fatal(err)
	}
	repo := store.NewMemoryCoverageAnalysisRepo()
	loader := NewLoader(LoaderDependencies{Customers: customers, Alerts: alerts, Cases: store.NewMemoryCaseRepo(), Reports: store.NewMemorySTRReportRepo(), AlertDecisions: store.NewMemoryAlertDecisionRepo(), Transactions: store.NewMemoryTransactionRepo(), Engine: &engine.MockBacktestEngine{Detections: []outcome.Detection{{ID: "replay-covered", CustomerID: "cust-coverage", ScenarioID: "scenario-a", TransactionIDs: []string{"tx-coverage"}, DetectedAt: now.Add(-time.Hour)}}}})
	svc := NewService(Dependencies{Repository: repo, Clock: func() time.Time { return now }, Load: loader})
	if _, err := svc.Create(ctx, &domain.CoverageAnalysis{ID: "coverage-e2e", From: now.Add(-24 * time.Hour), To: now, SnapshotAt: now, CustomerIDs: []string{"cust-coverage"}, ScenarioIDs: []string{"scenario-a"}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	completed, err := svc.Get(ctx, "coverage-e2e")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Summary.KnownMatter != 1 || completed.Summary.Covered != 1 || completed.Summary.Denominator != 1 || completed.Summary.Rate != 1 {
		t.Fatalf("coverage summary = %#v", completed.Summary)
	}
}
