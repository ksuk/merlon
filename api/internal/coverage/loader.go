package coverage

import (
	"context"
	"fmt"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/outcome"
)

// LoaderDependencies describes the durable source rows used by the default
// coverage worker. The loader deliberately reads through repositories and
// applies the analysis snapshot before invoking BuildKnownMatterUnion.
type LoaderDependencies struct {
	Customers domain.CustomerRepository
	Alerts    domain.AlertRepository
	Cases     domain.CaseRepository
	Reports   domain.ReportRepository
}

func NewLoader(deps LoaderDependencies) func(context.Context, *domain.CoverageAnalysis) ([]outcome.Detection, []outcome.Reference, error) {
	return func(ctx context.Context, analysis *domain.CoverageAnalysis) ([]outcome.Detection, []outcome.Reference, error) {
		if analysis == nil {
			return nil, nil, fmt.Errorf("coverage analysis is required")
		}
		if deps.Customers == nil || deps.Alerts == nil || deps.Cases == nil || deps.Reports == nil {
			return nil, nil, fmt.Errorf("coverage source repositories are not configured")
		}
		customerIDs, err := coverageCustomerIDs(ctx, deps.Customers, analysis.CustomerIDs)
		if err != nil {
			return nil, nil, err
		}
		snapshot := analysis.SnapshotAt
		candidateAlerts := make([]domain.Alert, 0)
		allCases := make([]domain.Case, 0)
		allReports := make([]domain.STRReport, 0)
		candidates := make([]outcome.Detection, 0)
		for _, customerID := range customerIDs {
			alerts, err := listCustomerAlerts(ctx, deps.Alerts, customerID)
			if err != nil {
				return nil, nil, err
			}
			cases, err := deps.Cases.ListByCustomer(ctx, customerID)
			if err != nil {
				return nil, nil, err
			}
			reports, err := deps.Reports.List(ctx, domain.ReportListFilter{CustomerID: customerID, Status: domain.ReportStatusSubmitted})
			if err != nil {
				return nil, nil, err
			}
			allCases = append(allCases, filterCasesAtSnapshot(cases, snapshot)...)
			allReports = append(allReports, filterReportsAtSnapshot(reports, snapshot)...)
			scores, err := deps.Customers.ListScoreHistory(ctx, customerID, 1000)
			if err != nil {
				return nil, nil, err
			}
			for _, alert := range alerts {
				if afterSnapshot(alert.DetectedAt, snapshot) || !matchesScenario(alert.ScenarioID, analysis.ScenarioIDs) {
					continue
				}
				tier, known := outcome.TierAt(scores, alert.DetectedAt)
				candidates = append(candidates, outcome.Detection{ID: alert.ID, CustomerID: alert.CustomerID, ScenarioID: alert.ScenarioID, TransactionIDs: append([]string(nil), alert.TransactionIDs...), DetectedAt: alert.DetectedAt, ScoreTier: tier, ScoreTierKnown: known})
				candidateAlerts = append(candidateAlerts, alert)
			}
		}
		return candidates, BuildKnownMatterUnion(candidateAlerts, allCases, allReports), nil
	}
}

func coverageCustomerIDs(ctx context.Context, repo domain.CustomerRepository, requested []string) ([]string, error) {
	if len(requested) > 0 {
		return append([]string(nil), requested...), nil
	}
	ids := make([]string, 0)
	var after *domain.Cursor
	for {
		page, err := repo.ListByCursor(ctx, 500, after)
		if err != nil {
			return nil, err
		}
		for _, customer := range page {
			ids = append(ids, customer.ID)
		}
		if len(page) < 500 {
			break
		}
		last := page[len(page)-1]
		after = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return ids, nil
}

func listCustomerAlerts(ctx context.Context, repo domain.AlertRepository, customerID string) ([]domain.Alert, error) {
	alerts := make([]domain.Alert, 0)
	var after *domain.Cursor
	for {
		page, err := repo.ListByCustomerCursor(ctx, customerID, 500, after)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, page...)
		if len(page) < 500 {
			break
		}
		last := page[len(page)-1]
		after = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return alerts, nil
}

func filterCasesAtSnapshot(items []domain.Case, snapshot time.Time) []domain.Case {
	result := make([]domain.Case, 0, len(items))
	for _, item := range items {
		if !afterSnapshot(item.UpdatedAt, snapshot) {
			result = append(result, item)
		}
	}
	return result
}

func filterReportsAtSnapshot(items []domain.STRReport, snapshot time.Time) []domain.STRReport {
	result := make([]domain.STRReport, 0, len(items))
	for _, item := range items {
		if item.SubmittedAt != nil && !afterSnapshot(*item.SubmittedAt, snapshot) {
			result = append(result, item)
		}
	}
	return result
}

func matchesScenario(scenario string, allowed []string) bool {
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

func afterSnapshot(value, snapshot time.Time) bool {
	return !snapshot.IsZero() && !value.IsZero() && value.After(snapshot)
}
