package domain

import (
	"context"
	"sort"
)

// BacktestDeltaKind says what a candidate rule set would change for one
// customer under one scenario. It is the answer an operator actually needs:
// "which customers start alerting if we ship this" is a different question
// from "which customers alert at all".
type BacktestDeltaKind string

const (
	// BacktestDeltaAdded: the candidate rule set alerts on this customer and
	// the baseline did not.
	BacktestDeltaAdded BacktestDeltaKind = "added"
	// BacktestDeltaRemoved: the baseline alerted and the candidate does not.
	BacktestDeltaRemoved BacktestDeltaKind = "removed"
	// BacktestDeltaUnchanged: both alert on this customer.
	BacktestDeltaUnchanged BacktestDeltaKind = "unchanged"
	// BacktestDeltaMixed can only appear in an aggregate across scenarios,
	// where a customer is added by one scenario and removed by another.
	BacktestDeltaMixed BacktestDeltaKind = "mixed"
)

// BacktestAffectedCustomer is one durable (job, scenario, customer) row. The
// rows exist so a completed job's population can be paged from the database
// instead of rebuilt in memory on every request: a job covering 50,000
// customers otherwise concatenated, sorted and de-duplicated the whole
// population to serve a 50-row page.
type BacktestAffectedCustomer struct {
	JobID      string            `json:"job_id"`
	ScenarioID string            `json:"scenario_id"`
	CustomerID string            `json:"customer_id"`
	DeltaKind  BacktestDeltaKind `json:"delta_kind"`
}

// BacktestAffectedCustomerFilter selects a keyset page. AfterCustomerID is the
// exclusive lower bound; the rows are ordered by customer_id so the cursor is
// the customer id itself and needs no encoding.
type BacktestAffectedCustomerFilter struct {
	JobID           string
	ScenarioID      string
	AfterCustomerID string
}

// BacktestAffectedCustomerRepository serves the durable rows written when a
// job completes.
type BacktestAffectedCustomerRepository interface {
	ListBacktestAffectedCustomers(ctx context.Context, filter BacktestAffectedCustomerFilter, limit int) ([]BacktestAffectedCustomer, error)
	CountBacktestAffectedCustomers(ctx context.Context, filter BacktestAffectedCustomerFilter) (int, error)
}

// BacktestAffectedCustomersFrom derives the durable rows from a completed
// job's results. Both stores call this, so the memory and PostgreSQL rows
// cannot disagree about what a result means.
//
// The population is the union of the candidate's affected customers and the
// delta's added and removed sets: a removed customer alerted only under the
// baseline, so it appears in no candidate scenario result and would otherwise
// be invisible in exactly the review that cares most about it.
func BacktestAffectedCustomersFrom(jobID string, candidate, delta *BacktestResult) []BacktestAffectedCustomer {
	kinds := map[string]map[string]BacktestDeltaKind{}
	set := func(scenarioID, customerID string, kind BacktestDeltaKind) {
		if scenarioID == "" || customerID == "" {
			return
		}
		if kinds[scenarioID] == nil {
			kinds[scenarioID] = map[string]BacktestDeltaKind{}
		}
		current, seen := kinds[scenarioID][customerID]
		// added wins over any other classification for the same pair. A pair
		// claimed both added and removed is contradictory input; treating it
		// as a new alert is the Fail-Alert reading.
		if seen && current == BacktestDeltaAdded {
			return
		}
		kinds[scenarioID][customerID] = kind
	}

	if candidate != nil {
		for _, scenario := range candidate.ScenarioResults {
			for _, id := range scenario.AffectedCustomerIDs {
				set(scenario.ScenarioID, id, BacktestDeltaUnchanged)
			}
		}
	}
	if delta != nil {
		for _, scenario := range delta.ScenarioResults {
			for _, id := range scenario.RemovedCustomerIDs {
				set(scenario.ScenarioID, id, BacktestDeltaRemoved)
			}
			for _, id := range scenario.AddedCustomerIDs {
				set(scenario.ScenarioID, id, BacktestDeltaAdded)
			}
		}
	}

	out := make([]BacktestAffectedCustomer, 0)
	for scenarioID, customers := range kinds {
		for customerID, kind := range customers {
			out = append(out, BacktestAffectedCustomer{JobID: jobID, ScenarioID: scenarioID, CustomerID: customerID, DeltaKind: kind})
		}
	}
	// Deterministic order so an identical job produces an identical row set,
	// which is what the backtest determinism guarantee rests on.
	sort.Slice(out, func(i, j int) bool {
		if out[i].CustomerID != out[j].CustomerID {
			return out[i].CustomerID < out[j].CustomerID
		}
		return out[i].ScenarioID < out[j].ScenarioID
	})
	return out
}

// AggregateBacktestDeltaKinds collapses per-scenario rows into one verdict per
// customer, which is what a page of customer ids can carry. A customer added
// by one scenario and removed by another is reported mixed rather than being
// silently assigned whichever scenario sorted first.
func AggregateBacktestDeltaKinds(rows []BacktestAffectedCustomer) map[string]BacktestDeltaKind {
	out := map[string]BacktestDeltaKind{}
	for _, row := range rows {
		current, seen := out[row.CustomerID]
		switch {
		case !seen:
			out[row.CustomerID] = row.DeltaKind
		case current == row.DeltaKind:
		case current == BacktestDeltaMixed:
		case current == BacktestDeltaUnchanged:
			out[row.CustomerID] = row.DeltaKind
		case row.DeltaKind == BacktestDeltaUnchanged:
		default:
			// added in one scenario, removed in another
			out[row.CustomerID] = BacktestDeltaMixed
		}
	}
	return out
}
