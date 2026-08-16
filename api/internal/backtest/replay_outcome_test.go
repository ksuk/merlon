package backtest

import (
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/outcome"
)

func TestReplayOutcomeDeltaEmitsSymmetricDifferenceChangeKinds(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	base := outcome.Result{MatcherVersion: outcome.MatcherVersion, SnapshotAt: now, Evaluations: []outcome.Evaluation{
		{CandidateID: "same", Label: outcome.LabelTP}, {CandidateID: "removed", Label: outcome.LabelFP}, {CandidateID: "changed", Label: outcome.LabelFP, ReferenceID: "old"},
	}}
	candidate := outcome.Result{MatcherVersion: outcome.MatcherVersion, SnapshotAt: now, Evaluations: []outcome.Evaluation{
		{CandidateID: "same", Label: outcome.LabelTP}, {CandidateID: "added", Label: outcome.LabelTP}, {CandidateID: "changed", Label: outcome.LabelTP, ReferenceID: "new"},
	}}
	delta, kinds := replayOutcomeDelta(base, candidate)
	if len(delta.Evaluations) != 3 || kinds["added"] != "added" || kinds["removed"] != "removed" || kinds["changed"] != "changed" {
		t.Fatalf("delta=%#v kinds=%#v", delta.Evaluations, kinds)
	}
}

func TestBuildCustomerPeriodOutcomesUsesSignedCandidateDelta(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	job := &domain.BacktestJob{From: from, To: from.Add(24 * time.Hour)}
	variants := map[domain.OutcomeVariant]outcome.Result{
		domain.OutcomeVariantBaseline:  {Evaluations: []outcome.Evaluation{{CandidateID: "base", CustomerID: "customer", ScenarioID: "scenario", Label: outcome.LabelFP, Denominator: true}}},
		domain.OutcomeVariantCandidate: {Evaluations: []outcome.Evaluation{{CandidateID: "candidate", CustomerID: "customer", ScenarioID: "scenario", Label: outcome.LabelTP, Denominator: true}}},
	}
	rows := buildCustomerPeriodOutcomes(job, variants)
	if len(rows) != 1 || rows[0].Baseline.FP != 1 || rows[0].Candidate.TP != 1 || rows[0].Delta.TP != 1 || rows[0].Delta.FP != -1 {
		t.Fatalf("customer-period rows = %#v", rows)
	}
}
