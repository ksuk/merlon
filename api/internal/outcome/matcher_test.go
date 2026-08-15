package outcome

import (
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestMatcherUsesJaccardBoundaryAndOneToOneAssignment(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	known := []Reference{
		{Detection: Detection{ID: "known-a", CustomerID: "cust-1", ScenarioID: "scenario-a", TransactionIDs: []string{"t-1", "t-2"}, DetectedAt: now}, State: HistoricalState{ScoreTier: domain.RiskTierHigh, ScoreTierKnown: true, AlertStatus: domain.AlertStatusClosedTruePositive}},
		{Detection: Detection{ID: "known-b", CustomerID: "cust-1", ScenarioID: "scenario-a", TransactionIDs: []string{"t-2", "t-3"}, DetectedAt: now.Add(time.Minute)}, State: HistoricalState{ScoreTier: domain.RiskTierHigh, ScoreTierKnown: true, AlertStatus: domain.AlertStatusClosedFalsePositive}},
	}
	candidates := []Detection{
		{ID: "candidate-1", CustomerID: "cust-1", ScenarioID: "scenario-a", TransactionIDs: []string{"t-1", "t-2", "t-3"}, DetectedAt: now, ScoreTier: domain.RiskTierHigh, ScoreTierKnown: true},
		{ID: "candidate-2", CustomerID: "cust-1", ScenarioID: "scenario-a", TransactionIDs: []string{"t-2"}, DetectedAt: now.Add(time.Minute), ScoreTier: domain.RiskTierHigh, ScoreTierKnown: true},
	}
	result := MatchAlerts(candidates, known, Options{Mode: ModeBacktest, SnapshotAt: now.Add(time.Hour)})
	if len(result.Matches) != 2 {
		t.Fatalf("matches = %#v, want one-to-one matches", result.Matches)
	}
	if result.Matches[0].CandidateID != "candidate-1" || result.Matches[0].ReferenceID != "known-a" || result.Matches[0].Score < 0.66 {
		t.Fatalf("first match = %#v", result.Matches[0])
	}
	if result.Matches[1].CandidateID != "candidate-2" || result.Matches[1].ReferenceID != "known-b" {
		t.Fatalf("second match = %#v", result.Matches[1])
	}
	if result.Evaluations[0].Label != LabelTP || result.Evaluations[1].Label != LabelFP {
		t.Fatalf("labels = %#v", result.Evaluations)
	}
	if result.Denominator != 2 {
		t.Fatalf("denominator = %d, want 2", result.Denominator)
	}
}

func TestMatcherFallsBackToIntervalOverlapAndScenarioUnion(t *testing.T) {
	from := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)
	known := []Reference{{Detection: Detection{ID: "known", CustomerID: "cust-1", ScenarioID: "other-scenario", WindowFrom: &from, WindowTo: &to, DetectedAt: from.Add(5 * time.Minute)}, State: HistoricalState{ScoreTier: domain.RiskTierMedium, ScoreTierKnown: true, CaseStatus: domain.CaseStatusEscalated}}}
	candidate := []Detection{{ID: "candidate", CustomerID: "cust-1", ScenarioID: "candidate-scenario", WindowFrom: ptrTime(from.Add(5 * time.Minute)), WindowTo: ptrTime(to.Add(5 * time.Minute)), DetectedAt: from.Add(5 * time.Minute), ScoreTier: domain.RiskTierMedium, ScoreTierKnown: true}}
	backtest := MatchAlerts(candidate, known, Options{Mode: ModeBacktest, SnapshotAt: to.Add(time.Hour)})
	if len(backtest.Matches) != 0 {
		t.Fatalf("backtest scenario mismatch should not match: %#v", backtest.Matches)
	}
	coverage := MatchAlerts(candidate, known, Options{Mode: ModeCoverage, SnapshotAt: to.Add(time.Hour)})
	if len(coverage.Matches) != 1 || coverage.Matches[0].Metric != MetricInterval || coverage.Matches[0].Score != 0.5 {
		t.Fatalf("coverage interval match = %#v", coverage.Matches)
	}
	if coverage.Evaluations[0].Label != LabelTP {
		t.Fatalf("coverage label = %q, want TP", coverage.Evaluations[0].Label)
	}
}

func TestMatcherPreservesUnlabeledAndMarksMissingHistoricalScoreUnevaluable(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	candidates := []Detection{
		{ID: "candidate-unlabeled", CustomerID: "cust-1", ScenarioID: "scenario", TransactionIDs: []string{"t-1"}, DetectedAt: now},
		{ID: "candidate-unknown-score", CustomerID: "cust-2", ScenarioID: "scenario", TransactionIDs: []string{"t-2"}, DetectedAt: now},
	}
	known := []Reference{
		{Detection: Detection{ID: "known-unlabeled", CustomerID: "cust-1", ScenarioID: "scenario", TransactionIDs: []string{"t-1"}, DetectedAt: now}, State: HistoricalState{ScoreTier: domain.RiskTierLow, ScoreTierKnown: true}},
		{Detection: Detection{ID: "known-positive", CustomerID: "cust-2", ScenarioID: "scenario", TransactionIDs: []string{"t-2"}, DetectedAt: now}, State: HistoricalState{ScoreTier: domain.RiskTierLow, ScoreTierKnown: true, STRFiled: true}},
	}
	result := MatchAlerts(candidates, known, Options{Mode: ModeBacktest, SnapshotAt: now.Add(time.Hour), ResolveScoreTier: func(customerID string, _ time.Time) (domain.RiskTier, bool) {
		if customerID == "cust-1" {
			return domain.RiskTierLow, true
		}
		return "", false
	}})
	labels := make(map[string]Label, len(result.Evaluations))
	for _, evaluation := range result.Evaluations {
		labels[evaluation.CandidateID] = evaluation.Label
	}
	if labels["candidate-unlabeled"] != LabelUnlabeled || labels["candidate-unknown-score"] != LabelUnevaluable {
		t.Fatalf("evaluations = %#v", result.Evaluations)
	}
	if result.Denominator != 0 {
		t.Fatalf("denominator = %d, unlabeled/unevaluable must be excluded", result.Denominator)
	}
	if result.Evaluations[0].Provenance.MatcherVersion != MatcherVersion || len(result.Evaluations[0].Provenance.Assumptions) == 0 || result.Evaluations[0].Provenance.SnapshotAt.IsZero() {
		t.Fatalf("provenance = %#v", result.Evaluations[0].Provenance)
	}
}

func TestTierAtUsesHistoricalScoreNotCurrentTier(t *testing.T) {
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	records := []domain.ScoreRecord{{ID: "score-old", Tier: domain.RiskTierLow, ScoredAt: first}, {ID: "score-new", Tier: domain.RiskTierHigh, ScoredAt: first.Add(24 * time.Hour)}}
	tier, ok := TierAt(records, first.Add(12*time.Hour))
	if !ok || tier != domain.RiskTierLow {
		t.Fatalf("tier at historical event = %q, %t", tier, ok)
	}
	if _, ok := TierAt(records, first.Add(-time.Hour)); ok {
		t.Fatal("score before first history should be unevaluable")
	}
}

func TestHistoricalStateAtReadsAppendOnlyDecisionAndSTRAsOfSnapshot(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	snapshot := start.Add(48 * time.Hour)
	decisions := []domain.AlertDecisionEvent{
		{ID: "decision-old", AlertID: "alert-1", ToStatus: domain.AlertStatusInvestigating, CreatedAt: start.Add(time.Hour)},
		{ID: "decision-future", AlertID: "alert-1", ToStatus: domain.AlertStatusClosedFalsePositive, CreatedAt: snapshot.Add(time.Hour)},
	}
	reports := []domain.STRReport{{ID: "str-1", AlertID: "alert-1", Status: domain.ReportStatusSubmitted, SubmittedAt: ptrTime(start.Add(2 * time.Hour))}}
	scores := []domain.ScoreRecord{{ID: "score-1", Tier: domain.RiskTierMedium, ScoredAt: start}}
	state := HistoricalStateAt(domain.Alert{ID: "alert-1", Status: domain.AlertStatusOpen}, decisions, nil, reports, scores, snapshot)
	if state.Decision == nil || state.Decision.ID != "decision-old" || !state.STRFiled || state.ScoreTier != domain.RiskTierMedium || !state.ScoreTierKnown {
		t.Fatalf("historical state = %#v", state)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
