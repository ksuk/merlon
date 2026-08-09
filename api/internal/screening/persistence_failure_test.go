package screening

import (
	"context"
	"errors"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
)

// selectivelyFailingWorkflow fails persistence for exactly one customer, which
// is the shape of a real partial outage: a row-level constraint or a lock
// timeout hits one customer, not the batch.
type selectivelyFailingWorkflow struct {
	failFor   string
	persisted []string
	attempts  int
}

func (w *selectivelyFailingWorkflow) persist(_ context.Context, run *domain.ScreeningRun, _ []domain.ScreeningResultRecord) error {
	w.attempts++
	if run.CustomerID == w.failFor {
		return errors.New("persist screening run: deadlock detected")
	}
	w.persisted = append(w.persisted, run.CustomerID)
	return nil
}

func TestRunRescreeningBatch_OneCustomerPersistenceFailureDoesNotAbortTheBatch(t *testing.T) {
	customers := newFakeCustomerRepo(
		domain.Customer{ID: "cust-1", RiskTier: riskTier(domain.RiskTierHigh)},
		domain.Customer{ID: "cust-2", RiskTier: riskTier(domain.RiskTierHigh)},
		domain.Customer{ID: "cust-3", RiskTier: riskTier(domain.RiskTierHigh)},
	)
	workflow := &selectivelyFailingWorkflow{failFor: "cust-2"}
	deps := SchedulerDeps{
		Customers:       customers,
		Screening:       &fakeScreeningEngine{},
		PersistWorkflow: workflow.persist,
	}

	result, err := RunRescreeningBatch(context.Background(), deps, TriggerListUpdated)
	if err != nil {
		t.Fatalf("batch returned a fatal error for one customer's failure: %v", err)
	}
	if len(result.Outcomes) != 3 {
		t.Fatalf("outcomes = %d, want one per customer", len(result.Outcomes))
	}

	byCustomer := map[string]CustomerScreenOutcome{}
	for _, outcome := range result.Outcomes {
		byCustomer[outcome.CustomerID] = outcome
	}
	if outcome := byCustomer["cust-2"]; outcome.Err == nil || outcome.Screened {
		t.Fatalf("cust-2 outcome = %+v, want the failure attributed to that customer", outcome)
	}
	for _, id := range []string{"cust-1", "cust-3"} {
		if outcome := byCustomer[id]; outcome.Err != nil || !outcome.Screened {
			t.Fatalf("%s outcome = %+v, want screened despite the other customer's failure", id, outcome)
		}
	}
	if workflow.attempts != 3 {
		t.Fatalf("persistence attempts = %d, want 3: the batch must not stop at the first failure", workflow.attempts)
	}
	if len(workflow.persisted) != 2 {
		t.Fatalf("persisted customers = %v, want the two that succeeded", workflow.persisted)
	}
}
