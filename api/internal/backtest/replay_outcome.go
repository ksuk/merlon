package backtest

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/outcome"
)

type ReplayOutcomeDependencies struct {
	Customers      domain.CustomerRepository
	Alerts         domain.AlertRepository
	Cases          domain.CaseRepository
	Reports        domain.ReportRepository
	AlertDecisions domain.AlertDecisionRepository
	Clock          func() time.Time
}

// NewReplayOutcomeBuilder labels replay detections against durable alert,
// decision, case, STR, and score history as of the job snapshot.
func NewReplayOutcomeBuilder(deps ReplayOutcomeDependencies) func(context.Context, *domain.BacktestJob, map[domain.OutcomeVariant][]outcome.Detection) (*domain.BacktestOutcomeAnalysis, []domain.BacktestOutcomeDetail, error) {
	return func(ctx context.Context, job *domain.BacktestJob, detections map[domain.OutcomeVariant][]outcome.Detection) (*domain.BacktestOutcomeAnalysis, []domain.BacktestOutcomeDetail, error) {
		if job == nil {
			return nil, nil, fmt.Errorf("backtest job is required")
		}
		if deps.Customers == nil || deps.Alerts == nil || deps.Cases == nil || deps.Reports == nil || deps.AlertDecisions == nil {
			return nil, nil, fmt.Errorf("backtest outcome history repositories are not configured")
		}
		snapshot := job.SnapshotAt
		if snapshot.IsZero() {
			snapshot = time.Now().UTC()
		}
		customerIDs := replayCustomerIDs(detections)
		references := make([]outcome.Reference, 0)
		scoresByCustomer := make(map[string][]domain.ScoreRecord, len(customerIDs))
		for _, customerID := range customerIDs {
			scores, err := deps.Customers.ListScoreHistory(ctx, customerID, 1000)
			if err != nil {
				return nil, nil, err
			}
			scoresByCustomer[customerID] = scores
			cases, err := deps.Cases.ListByCustomer(ctx, customerID)
			if err != nil {
				return nil, nil, err
			}
			reports, err := deps.Reports.List(ctx, domain.ReportListFilter{CustomerID: customerID})
			if err != nil {
				return nil, nil, err
			}
			alerts, err := listReplayAlerts(ctx, deps.Alerts, customerID)
			if err != nil {
				return nil, nil, err
			}
			for _, alert := range alerts {
				if (!snapshot.IsZero() && alert.DetectedAt.After(snapshot)) || !replayScenarioSelected(alert.ScenarioID, job.ScenarioIDs) {
					continue
				}
				decisions, err := deps.AlertDecisions.ListDecisions(ctx, alert.ID)
				if err != nil {
					return nil, nil, err
				}
				historicalAlert := alert
				if alert.UpdatedAt.After(snapshot) {
					// A later mutable status is not historical evidence. Append-only
					// decisions below may still reconstruct the status as of snapshot.
					historicalAlert.Status = ""
				}
				state := outcome.HistoricalStateAt(historicalAlert, decisions, cases, reports, scores, snapshot)
				references = append(references, outcome.Reference{
					Detection: outcome.Detection{ID: "alert:" + alert.ID, CustomerID: alert.CustomerID, ScenarioID: alert.ScenarioID, TransactionIDs: append([]string(nil), alert.TransactionIDs...), DetectedAt: alert.DetectedAt},
					State:     state, Provenance: map[string]string{"source": "alert", "alert_id": alert.ID},
				})
			}
		}
		variants := make(map[domain.OutcomeVariant]outcome.Result, 2)
		for _, variant := range []domain.OutcomeVariant{domain.OutcomeVariantBaseline, domain.OutcomeVariantCandidate} {
			items := append([]outcome.Detection(nil), detections[variant]...)
			for index := range items {
				items[index].ScoreTier, items[index].ScoreTierKnown = outcome.TierAt(scoresByCustomer[items[index].CustomerID], items[index].DetectedAt)
			}
			variants[variant] = outcome.MatchAlerts(items, references, outcome.Options{Mode: outcome.ModeBacktest, SnapshotAt: snapshot})
		}
		deltaResult, changeKinds := replayOutcomeDelta(variants[domain.OutcomeVariantBaseline], variants[domain.OutcomeVariantCandidate])
		variants[domain.OutcomeVariantDelta] = deltaResult
		generatedAt := time.Now().UTC()
		if deps.Clock != nil {
			generatedAt = deps.Clock().UTC()
		}
		analysis, details, err := BuildOutcomeAnalysis(job.ID, variants, generatedAt)
		if err != nil {
			return nil, nil, err
		}
		analysis.Delta = subtractOutcomeSummary(analysis.Candidate, analysis.Baseline)
		analysis.CustomerPeriod = buildCustomerPeriodOutcomes(job, variants)
		for index := range details {
			if details[index].Variant == domain.OutcomeVariantDelta {
				details[index].ChangeKind = changeKinds[details[index].CandidateID]
			}
		}
		return analysis, details, nil
	}
}

func buildCustomerPeriodOutcomes(job *domain.BacktestJob, variants map[domain.OutcomeVariant]outcome.Result) []domain.CustomerPeriodOutcome {
	type bucketKey struct{ customerID, scenarioID string }
	buckets := map[bucketKey]*domain.CustomerPeriodOutcome{}
	for _, variant := range []domain.OutcomeVariant{domain.OutcomeVariantBaseline, domain.OutcomeVariantCandidate} {
		for _, evaluation := range variants[variant].Evaluations {
			key := bucketKey{evaluation.CustomerID, evaluation.ScenarioID}
			bucket := buckets[key]
			if bucket == nil {
				bucket = &domain.CustomerPeriodOutcome{CustomerID: key.customerID, ScenarioID: key.scenarioID, From: job.From, To: job.To}
				buckets[key] = bucket
			}
			if variant == domain.OutcomeVariantBaseline {
				addEvaluationSummary(&bucket.Baseline, evaluation)
			} else {
				addEvaluationSummary(&bucket.Candidate, evaluation)
			}
		}
	}
	result := make([]domain.CustomerPeriodOutcome, 0, len(buckets))
	for _, bucket := range buckets {
		bucket.Delta = subtractOutcomeSummary(bucket.Candidate, bucket.Baseline)
		result = append(result, *bucket)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CustomerID != result[j].CustomerID {
			return result[i].CustomerID < result[j].CustomerID
		}
		return result[i].ScenarioID < result[j].ScenarioID
	})
	return result
}

func addEvaluationSummary(summary *domain.OutcomeSummary, evaluation outcome.Evaluation) {
	if evaluation.Match != nil {
		summary.Investigated++
	}
	if evaluation.Denominator {
		summary.Denominator++
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
	if summary.Denominator > 0 {
		summary.Rate = float64(summary.TP) / float64(summary.Denominator)
	}
}

func replayOutcomeDelta(baseline, candidate outcome.Result) (outcome.Result, map[string]string) {
	delta := outcome.Result{MatcherVersion: candidate.MatcherVersion, Mode: candidate.Mode, SnapshotAt: candidate.SnapshotAt, Assumptions: append([]string(nil), candidate.Assumptions...)}
	changeKinds := map[string]string{}
	baselineByID := make(map[string]outcome.Evaluation, len(baseline.Evaluations))
	candidateByID := make(map[string]outcome.Evaluation, len(candidate.Evaluations))
	for _, evaluation := range baseline.Evaluations {
		baselineByID[evaluation.CandidateID] = evaluation
	}
	for _, evaluation := range candidate.Evaluations {
		candidateByID[evaluation.CandidateID] = evaluation
		if previous, exists := baselineByID[evaluation.CandidateID]; !exists {
			delta.Evaluations = append(delta.Evaluations, evaluation)
			changeKinds[evaluation.CandidateID] = "added"
		} else if previous.Label != evaluation.Label || previous.ReferenceID != evaluation.ReferenceID {
			delta.Evaluations = append(delta.Evaluations, evaluation)
			changeKinds[evaluation.CandidateID] = "changed"
		}
	}
	for _, evaluation := range baseline.Evaluations {
		if _, exists := candidateByID[evaluation.CandidateID]; !exists {
			delta.Evaluations = append(delta.Evaluations, evaluation)
			changeKinds[evaluation.CandidateID] = "removed"
		}
	}
	sort.Slice(delta.Evaluations, func(i, j int) bool { return delta.Evaluations[i].CandidateID < delta.Evaluations[j].CandidateID })
	return delta, changeKinds
}

func replayCustomerIDs(variants map[domain.OutcomeVariant][]outcome.Detection) []string {
	seen := map[string]struct{}{}
	for _, items := range variants {
		for _, item := range items {
			if item.CustomerID != "" {
				seen[item.CustomerID] = struct{}{}
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func listReplayAlerts(ctx context.Context, repo domain.AlertRepository, customerID string) ([]domain.Alert, error) {
	var result []domain.Alert
	var after *domain.Cursor
	for {
		page, err := repo.ListByCustomerCursor(ctx, customerID, 500, after)
		if err != nil {
			return nil, err
		}
		result = append(result, page...)
		if len(page) < 500 {
			return result, nil
		}
		last := page[len(page)-1]
		after = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}

func replayScenarioSelected(scenario string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, item := range allowed {
		if item == scenario {
			return true
		}
	}
	return false
}

func subtractOutcomeSummary(candidate, baseline domain.OutcomeSummary) domain.OutcomeSummary {
	return domain.OutcomeSummary{TP: candidate.TP - baseline.TP, FP: candidate.FP - baseline.FP,
		Unlabeled: candidate.Unlabeled - baseline.Unlabeled, Unevaluable: candidate.Unevaluable - baseline.Unevaluable,
		Investigated: candidate.Investigated - baseline.Investigated, Rate: candidate.Rate - baseline.Rate,
		Denominator: candidate.Denominator - baseline.Denominator}
}
