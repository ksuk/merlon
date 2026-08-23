package backtest

import (
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/outcome"
)

func TestBuildOutcomeAnalysisKeepsUnlabeledOutOfDenominator(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	result := outcome.MatchAlerts([]outcome.Detection{{ID: "candidate", CustomerID: "cust-1", ScenarioID: "scenario", TransactionIDs: []string{"tx"}, DetectedAt: now, ScoreTier: domain.RiskTierHigh, ScoreTierKnown: true}}, []outcome.Reference{{Detection: outcome.Detection{ID: "reference", CustomerID: "cust-1", ScenarioID: "scenario", TransactionIDs: []string{"tx"}, DetectedAt: now}, State: outcome.HistoricalState{ScoreTier: domain.RiskTierHigh, ScoreTierKnown: true, AlertStatus: domain.AlertStatusClosedTruePositive}}}, outcome.Options{Mode: outcome.ModeBacktest, SnapshotAt: now.Add(time.Hour)})
	analysis, details, err := BuildOutcomeAnalysis("job-1", map[domain.OutcomeVariant]outcome.Result{domain.OutcomeVariantCandidate: result}, now)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Candidate.TP != 1 || analysis.Candidate.Denominator != 1 || analysis.Candidate.Rate != 1 || len(details) != 1 || details[0].MatchedAlertID != "reference" {
		t.Fatalf("analysis=%#v details=%#v", analysis, details)
	}
}
