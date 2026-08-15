package coverage

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/outcome"
	"github.com/ksuk/merlon/api/internal/store"
)

func TestAnalyzePersistsCoveredNotCoveredAndUnevaluableMatters(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	repo := store.NewMemoryCoverageAnalysisRepo()
	svc := NewService(Dependencies{Repository: repo, Clock: func() time.Time { return now }})
	analysis, err := svc.Create(context.Background(), &domain.CoverageAnalysis{ID: "coverage-1", SnapshotAt: now})
	if err != nil {
		t.Fatal(err)
	}
	matters := []outcome.Reference{
		{Detection: outcome.Detection{ID: "matter-covered", CustomerID: "cust-1", ScenarioID: "scenario-a", TransactionIDs: []string{"tx-1"}, DetectedAt: now, ScoreTier: domain.RiskTierHigh, ScoreTierKnown: true}, Provenance: map[string]string{"source": "str"}},
		{Detection: outcome.Detection{ID: "matter-not-covered", CustomerID: "cust-2", ScenarioID: "scenario-a", TransactionIDs: []string{"tx-2"}, DetectedAt: now, ScoreTier: domain.RiskTierMedium, ScoreTierKnown: true}, Provenance: map[string]string{"source": "case"}},
		{Detection: outcome.Detection{ID: "matter-unevaluable", CustomerID: "cust-3", ScenarioID: "scenario-b", TransactionIDs: []string{"tx-3"}, DetectedAt: now}, Provenance: map[string]string{"source": "alert"}},
	}
	candidates := []outcome.Detection{{ID: "alert-1", CustomerID: "cust-1", ScenarioID: "scenario-other", TransactionIDs: []string{"tx-1"}, DetectedAt: now, ScoreTier: domain.RiskTierHigh, ScoreTierKnown: true}}
	completed, err := svc.Analyze(context.Background(), analysis, candidates, matters)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != domain.CoverageAnalysisCompleted || completed.Summary.KnownMatter != 3 || completed.Summary.Covered != 1 || completed.Summary.NotCovered != 1 || completed.Summary.Unevaluable != 1 || completed.Summary.Denominator != 2 {
		t.Fatalf("analysis = %#v", completed)
	}
	rows, err := svc.Matters(context.Background(), domain.CoverageMatterFilter{AnalysisID: analysis.ID, Limit: 10})
	if err != nil || len(rows) != 3 {
		t.Fatalf("matter rows = %#v, err=%v", rows, err)
	}
	if rows[0].MatcherVersion != outcome.MatcherVersion || rows[0].SnapshotAt.IsZero() {
		t.Fatalf("matter provenance = %#v", rows[0])
	}
}

func TestRunOnceClaimsQueuedAnalysisAndCompletesIt(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	repo := store.NewMemoryCoverageAnalysisRepo()
	svc := NewService(Dependencies{Repository: repo, Clock: func() time.Time { return now }, Load: func(_ context.Context, analysis *domain.CoverageAnalysis) ([]outcome.Detection, []outcome.Reference, error) {
		return []outcome.Detection{{ID: "candidate", CustomerID: "cust-1", TransactionIDs: []string{"tx"}, DetectedAt: analysis.SnapshotAt, ScoreTier: domain.RiskTierHigh, ScoreTierKnown: true}}, []outcome.Reference{{Detection: outcome.Detection{ID: "matter", CustomerID: "cust-1", TransactionIDs: []string{"tx"}, DetectedAt: analysis.SnapshotAt, ScoreTier: domain.RiskTierHigh, ScoreTierKnown: true}}}, nil
	}})
	if _, err := svc.Create(context.Background(), &domain.CoverageAnalysis{ID: "queued-1", SnapshotAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	completed, err := svc.Get(context.Background(), "queued-1")
	if err != nil || completed.Status != domain.CoverageAnalysisCompleted || completed.Summary.Covered != 1 {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
}
