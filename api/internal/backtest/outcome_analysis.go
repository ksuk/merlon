package backtest

import (
	"fmt"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/outcome"
)

// BuildOutcomeAnalysis converts matcher results for baseline/candidate/delta
// into the additive durable BacktestJob contract. It is pure so a worker can
// retry persistence without rerunning matching.
func BuildOutcomeAnalysis(jobID string, variants map[domain.OutcomeVariant]outcome.Result, generatedAt time.Time) (*domain.BacktestOutcomeAnalysis, []domain.BacktestOutcomeDetail, error) {
	if jobID == "" {
		return nil, nil, fmt.Errorf("job ID is required")
	}
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	analysis := &domain.BacktestOutcomeAnalysis{MatcherVersion: outcome.MatcherVersion, Assumptions: []string{}, ByScenario: map[string]domain.OutcomeSummary{}, GeneratedAt: generatedAt.UTC()}
	var details []domain.BacktestOutcomeDetail
	for _, variant := range []domain.OutcomeVariant{domain.OutcomeVariantBaseline, domain.OutcomeVariantCandidate, domain.OutcomeVariantDelta} {
		result, ok := variants[variant]
		if !ok {
			continue
		}
		if analysis.SnapshotAt.IsZero() {
			analysis.SnapshotAt = result.SnapshotAt
		}
		analysis.MatcherVersion = result.MatcherVersion
		if len(analysis.Assumptions) == 0 {
			analysis.Assumptions = append([]string(nil), result.Assumptions...)
		}
		summary := summarize(result)
		switch variant {
		case domain.OutcomeVariantBaseline:
			analysis.Baseline = summary
		case domain.OutcomeVariantCandidate:
			analysis.Candidate = summary
		case domain.OutcomeVariantDelta:
			analysis.Delta = summary
		}
		for _, evaluation := range result.Evaluations {
			scenarioKey := string(variant) + "/" + evaluation.ScenarioID
			bucket := analysis.ByScenario[scenarioKey]
			bucket.Denominator += boolInt(evaluation.Denominator)
			if evaluation.Match != nil {
				bucket.Investigated++
			}
			switch evaluation.Label {
			case outcome.LabelTP:
				bucket.TP++
			case outcome.LabelFP:
				bucket.FP++
			case outcome.LabelUnlabeled:
				bucket.Unlabeled++
			case outcome.LabelUnevaluable:
				bucket.Unevaluable++
			}
			if bucket.Denominator > 0 {
				bucket.Rate = float64(bucket.TP) / float64(bucket.Denominator)
			}
			analysis.ByScenario[scenarioKey] = bucket
			detail := domain.BacktestOutcomeDetail{ID: fmt.Sprintf("%s:%s:%s", jobID, variant, evaluation.CandidateID), JobID: jobID, Variant: variant, CandidateID: evaluation.CandidateID, ReferenceID: evaluation.ReferenceID, CustomerID: evaluation.CustomerID, ScenarioID: evaluation.ScenarioID, Label: string(evaluation.Label), Investigated: evaluation.Match != nil, MatcherVersion: evaluation.Provenance.MatcherVersion, Assumptions: append([]string(nil), evaluation.Provenance.Assumptions...), SnapshotAt: evaluation.Provenance.SnapshotAt, Provenance: cloneStringMap(evaluation.Provenance.Source), CreatedAt: generatedAt.UTC()}
			if evaluation.Match != nil {
				detail.Metric, detail.Score = evaluation.Match.Metric, evaluation.Match.Score
				detail.MatchedAlertID = evaluation.ReferenceID
				if evaluation.Provenance.Source != nil {
					detail.MatchedCaseID = evaluation.Provenance.Source["case_id"]
				}
			}
			details = append(details, detail)
		}
	}
	if analysis.SnapshotAt.IsZero() {
		analysis.SnapshotAt = generatedAt.UTC()
	}
	return analysis, details, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func summarize(result outcome.Result) domain.OutcomeSummary {
	summary := domain.OutcomeSummary{Denominator: result.Denominator}
	for _, evaluation := range result.Evaluations {
		if evaluation.Match != nil {
			summary.Investigated++
		}
		switch evaluation.Label {
		case outcome.LabelTP:
			summary.TP++
		case outcome.LabelFP:
			summary.FP++
		case outcome.LabelUnlabeled:
			summary.Unlabeled++
		case outcome.LabelUnevaluable:
			summary.Unevaluable++
		}
	}
	if summary.Denominator > 0 {
		summary.Rate = float64(summary.TP) / float64(summary.Denominator)
	}
	return summary
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
