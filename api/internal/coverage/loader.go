package coverage

import (
	"context"
	"fmt"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/outcome"
	"github.com/ksuk/merlon/api/internal/transactionhistory"
)

// LoaderDependencies describes the durable source rows used by the default
// coverage worker. The loader deliberately reads through repositories and
// applies the analysis snapshot before invoking BuildKnownMatterUnion.
type LoaderDependencies struct {
	Customers      domain.CustomerRepository
	Alerts         domain.AlertRepository
	Cases          domain.CaseRepository
	Reports        domain.ReportRepository
	AlertDecisions domain.AlertDecisionRepository
	Transactions   domain.TransactionRepository
	Engine         engine.BacktestEngine
}

func NewLoader(deps LoaderDependencies) func(context.Context, *domain.CoverageAnalysis) ([]outcome.Detection, []outcome.Reference, error) {
	return func(ctx context.Context, analysis *domain.CoverageAnalysis) ([]outcome.Detection, []outcome.Reference, error) {
		if analysis == nil {
			return nil, nil, fmt.Errorf("coverage analysis is required")
		}
		if deps.Customers == nil || deps.Alerts == nil || deps.Cases == nil || deps.Reports == nil || deps.AlertDecisions == nil || deps.Transactions == nil || deps.Engine == nil {
			return nil, nil, fmt.Errorf("coverage source repositories are not configured")
		}
		customerIDs, err := coverageCustomerIDs(ctx, deps.Customers, analysis.CustomerIDs)
		if err != nil {
			return nil, nil, err
		}
		snapshot := analysis.SnapshotAt
		allSnapshotAlerts := make([]domain.Alert, 0)
		allCases := make([]domain.Case, 0)
		allReports := make([]domain.STRReport, 0)
		scoresByCustomer := make(map[string][]domain.ScoreRecord, len(customerIDs))
		selectedCustomers := make([]domain.Customer, 0, len(customerIDs))
		transactions := make([]domain.Transaction, 0)
		for _, customerID := range customerIDs {
			customer, err := deps.Customers.Get(ctx, customerID)
			if err != nil {
				return nil, nil, err
			}
			selectedCustomers = append(selectedCustomers, *customer)
			customerTransactions, err := transactionhistory.ListCustomerTransactionsAsOf(ctx, deps.Transactions, customerID, transactionhistory.Query{From: analysis.From, To: analysis.To, CreatedThrough: snapshot})
			if err != nil {
				return nil, nil, err
			}
			transactions = append(transactions, customerTransactions...)
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
			scoresByCustomer[customerID] = scores
			for _, alert := range alerts {
				if afterSnapshot(alert.DetectedAt, snapshot) {
					continue
				}
				decisions, err := deps.AlertDecisions.ListDecisions(ctx, alert.ID)
				if err != nil {
					return nil, nil, err
				}
				if alert.UpdatedAt.After(snapshot) {
					alert.Status = ""
				}
				alert.Status = outcome.HistoricalStateAt(alert, decisions, nil, nil, nil, snapshot).AlertStatus
				allSnapshotAlerts = append(allSnapshotAlerts, alert)
			}
		}
		if analysis.RuleSetID != "" && analysis.RuleSetID != "active" {
			return nil, nil, fmt.Errorf("coverage replay rule set %q is not available from the pinned active engine", analysis.RuleSetID)
		}
		detailed, ok := deps.Engine.(engine.DetailedBacktestEngine)
		if !ok {
			return nil, nil, fmt.Errorf("coverage replay engine does not expose detailed detections")
		}
		_, candidates, err := detailed.RunBacktestDetailed(ctx, selectedCustomers, transactions, analysis.ScenarioIDs, "coverage:"+analysis.ID)
		if err != nil {
			return nil, nil, err
		}
		for index := range candidates {
			candidates[index].ScoreTier, candidates[index].ScoreTierKnown = outcome.TierAt(scoresByCustomer[candidates[index].CustomerID], candidates[index].DetectedAt)
		}
		matters := BuildKnownMatterUnion(allSnapshotAlerts, allCases, allReports)
		for index := range matters {
			tier, known := outcome.TierAt(scoresByCustomer[matters[index].CustomerID], matters[index].DetectedAt)
			matters[index].ScoreTier = tier
			matters[index].ScoreTierKnown = known
			matters[index].State.ScoreTier = tier
			matters[index].State.ScoreTierKnown = known
		}
		return candidates, matters, nil
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
