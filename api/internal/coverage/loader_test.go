package coverage

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
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
	loader := NewLoader(LoaderDependencies{Customers: customers, Alerts: alerts, Cases: cases, Reports: reports})
	candidates, matters, err := loader(ctx, &domain.CoverageAnalysis{SnapshotAt: now, ScenarioIDs: []string{"scenario-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != "alert-in" || !candidates[0].ScoreTierKnown || candidates[0].ScoreTier != domain.RiskTierHigh {
		t.Fatalf("candidates = %#v", candidates)
	}
	if len(matters) != 0 {
		t.Fatalf("matters = %#v, want no known matter", matters)
	}
}
