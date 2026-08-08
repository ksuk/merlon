package screening

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

// recordingWorkflow captures what RunRescreeningBatch persists, so these tests
// assert on the durable record rather than on the transient batch outcome.
type recordingWorkflow struct {
	runs    []domain.ScreeningRun
	results []domain.ScreeningResultRecord
	err     error
}

func (w *recordingWorkflow) persist(_ context.Context, run *domain.ScreeningRun, results []domain.ScreeningResultRecord) error {
	if w.err != nil {
		return w.err
	}
	w.runs = append(w.runs, *run)
	w.results = append(w.results, results...)
	return nil
}

func newSuppressionDeps(t *testing.T, customerID string, matches []domain.ScreenMatch) (SchedulerDeps, *store.MemoryScreeningResultRepo, *recordingWorkflow) {
	t.Helper()
	results := store.NewMemoryScreeningResultRepo()
	workflow := &recordingWorkflow{}
	deps := SchedulerDeps{
		Customers:        newFakeCustomerRepo(domain.Customer{ID: customerID, RiskTier: riskTier(domain.RiskTierHigh)}),
		Screening:        &fakeScreeningEngine{matches: map[string][]domain.ScreenMatch{customerID: matches}},
		Results:          results,
		PersistWorkflow:  workflow.persist,
		TargetCustomerID: customerID,
	}
	return deps, results, workflow
}

func seedFalsePositive(t *testing.T, repo *store.MemoryScreeningResultRepo, id, customerID, entryID string) {
	t.Helper()
	record := &domain.ScreeningResultRecord{
		ID: id, CustomerID: customerID, ListID: "ofac_sdn", ListType: "sanctions", EntryID: entryID,
		MatchedName: "Example Person", Status: domain.ScreeningResultStatusFalsePositive,
		FalsePositiveReason: "different date of birth", ReviewedBy: "analyst-1",
		// Dated in the past so the immediate-rescreen exclusion in
		// screenOneForBatch (screened_at after batch start) does not fire.
		ScreenedAt: time.Now().Add(-48 * time.Hour), CreatedAt: time.Now().Add(-48 * time.Hour),
	}
	if err := repo.Create(context.Background(), record); err != nil {
		t.Fatal(err)
	}
}

func TestRunRescreeningBatch_SuppressesRepeatFalsePositiveForSameCustomer(t *testing.T) {
	customerID := "cust-suppress"
	match := domain.ScreenMatch{ListID: "ofac_sdn", ListType: "sanctions", EntryID: "OFAC-123", MatchedName: "Example Person", Similarity: 0.97, Source: "ofac"}
	deps, results, workflow := newSuppressionDeps(t, customerID, []domain.ScreenMatch{match})
	seedFalsePositive(t, results, "prior-fp-1", customerID, "OFAC-123")

	if _, err := RunRescreeningBatch(context.Background(), deps, TriggerAPIRequest); err != nil {
		t.Fatal(err)
	}

	if len(workflow.results) != 1 {
		t.Fatalf("persisted %d results, want 1", len(workflow.results))
	}
	got := workflow.results[0]
	if !got.Suppressed {
		t.Fatal("repeat hit on an entry this customer already cleared was not suppressed")
	}
	if got.SuppressionReason != "prior_false_positive:prior-fp-1" {
		t.Fatalf("suppression_reason = %q, want the prior determination's id", got.SuppressionReason)
	}
	// The earlier judgement travels with the hit so a reviewer never has to
	// go looking for why it was hidden.
	evidence, ok := got.MatchEvidence["prior_false_positive"].(map[string]any)
	if !ok {
		t.Fatalf("match_evidence = %v, want a prior_false_positive block", got.MatchEvidence)
	}
	if evidence["reason"] != "different date of birth" || evidence["reviewed_by"] != "analyst-1" {
		t.Fatalf("prior_false_positive evidence = %v, want the original reason and reviewer", evidence)
	}
	// Suppression hides a hit from the default queue; it never drops it.
	if got.Status != domain.ScreeningResultStatusNew {
		t.Fatalf("status = %q, want the suppressed hit still reviewable as NEW", got.Status)
	}
}

func TestRunRescreeningBatch_DoesNotSuppressAnotherCustomersFalsePositive(t *testing.T) {
	customerID := "cust-b"
	match := domain.ScreenMatch{ListID: "ofac_sdn", ListType: "sanctions", EntryID: "OFAC-123", MatchedName: "Example Person", Similarity: 0.97}
	deps, results, workflow := newSuppressionDeps(t, customerID, []domain.ScreenMatch{match})
	// The same list entry, cleared for a different customer. That decision
	// says nothing about this customer, whose name may match for an entirely
	// different reason.
	seedFalsePositive(t, results, "prior-fp-other", "cust-a", "OFAC-123")

	if _, err := RunRescreeningBatch(context.Background(), deps, TriggerAPIRequest); err != nil {
		t.Fatal(err)
	}
	if len(workflow.results) != 1 {
		t.Fatalf("persisted %d results, want 1", len(workflow.results))
	}
	if workflow.results[0].Suppressed {
		t.Fatal("another customer's false positive suppressed this customer's hit")
	}
}

func TestRunRescreeningBatch_DoesNotSuppressDifferentEntry(t *testing.T) {
	customerID := "cust-c"
	match := domain.ScreenMatch{ListID: "ofac_sdn", ListType: "sanctions", EntryID: "OFAC-999", MatchedName: "Example Person"}
	deps, results, workflow := newSuppressionDeps(t, customerID, []domain.ScreenMatch{match})
	seedFalsePositive(t, results, "prior-fp-2", customerID, "OFAC-123")

	if _, err := RunRescreeningBatch(context.Background(), deps, TriggerAPIRequest); err != nil {
		t.Fatal(err)
	}
	if workflow.results[0].Suppressed {
		t.Fatal("a false positive on OFAC-123 suppressed an unrelated hit on OFAC-999")
	}
}

// failingFalsePositiveRepo makes only the suppression lookup fail, leaving the
// rest of the result repository behaviour intact.
type failingFalsePositiveRepo struct {
	*store.MemoryScreeningResultRepo
}

func (r failingFalsePositiveRepo) ListPastFalsePositives(context.Context, string) ([]domain.ScreeningResultRecord, error) {
	return nil, errors.New("suppression history unavailable")
}

func TestRunRescreeningBatch_UnreadableHistoryLeavesHitVisible(t *testing.T) {
	customerID := "cust-d"
	match := domain.ScreenMatch{ListID: "ofac_sdn", ListType: "sanctions", EntryID: "OFAC-123", MatchedName: "Example Person"}
	deps, results, workflow := newSuppressionDeps(t, customerID, []domain.ScreenMatch{match})
	deps.Results = failingFalsePositiveRepo{MemoryScreeningResultRepo: results}

	if _, err := RunRescreeningBatch(context.Background(), deps, TriggerAPIRequest); err != nil {
		t.Fatal(err)
	}
	// Fail-Alert: an unreadable history means review the hit again, never hide it.
	if workflow.results[0].Suppressed {
		t.Fatal("hit was suppressed even though the suppression history could not be read")
	}
}

func TestRunRescreeningBatch_MarksRunDegradedWhenSourcesUnready(t *testing.T) {
	customerID := "cust-degraded"
	match := domain.ScreenMatch{ListID: "ofac_sdn", ListType: "sanctions", EntryID: "OFAC-123", MatchedName: "Example Person"}
	deps, _, workflow := newSuppressionDeps(t, customerID, []domain.ScreenMatch{match})
	deps.Readiness = func(context.Context) domain.ScreeningDegradation {
		return domain.ScreeningDegradation{Degraded: true, Sources: []string{"un_sc"}}
	}

	result, err := RunRescreeningBatch(context.Background(), deps, TriggerAPIRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Degradation.Degraded {
		t.Fatal("batch result did not report the degradation")
	}
	if len(workflow.runs) != 1 || !workflow.runs[0].Degraded {
		t.Fatalf("persisted run = %+v, want degraded", workflow.runs)
	}
	if got := workflow.runs[0].DegradedSources; len(got) != 1 || got[0] != "un_sc" {
		t.Fatalf("run degraded_sources = %v, want [un_sc]", got)
	}
	// The result carries its own copy: results are listed and exported
	// without the run that produced them.
	if !workflow.results[0].Degraded || len(workflow.results[0].DegradedSources) != 1 {
		t.Fatalf("persisted result = %+v, want the same degradation stamped on the hit", workflow.results[0])
	}
}

func TestRunRescreeningBatch_MarksFailedRunDegraded(t *testing.T) {
	customerID := "cust-degraded-failure"
	deps, _, workflow := newSuppressionDeps(t, customerID, nil)
	deps.Screening = nil // forces the failure path
	deps.Readiness = func(context.Context) domain.ScreeningDegradation {
		return domain.ScreeningDegradation{Degraded: true, Sources: []string{"mof_japan"}}
	}

	result, err := RunRescreeningBatch(context.Background(), deps, TriggerAPIRequest)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcomes[0].Err == nil {
		t.Fatal("expected the missing engine to be reported as a customer-level error")
	}
	if len(workflow.runs) != 1 || workflow.runs[0].Status != domain.ScreeningRunFailed {
		t.Fatalf("persisted runs = %+v, want one failed run", workflow.runs)
	}
	if !workflow.runs[0].Degraded {
		t.Fatal("failed run lost the degradation verdict; the failure and the stale list are separate facts")
	}
}

func TestRunRescreeningBatch_NoReadinessAssessorMakesNoDegradationClaim(t *testing.T) {
	customerID := "cust-unknown-readiness"
	match := domain.ScreenMatch{ListID: "ofac_sdn", ListType: "sanctions", EntryID: "OFAC-1", MatchedName: "Example Person"}
	deps, _, workflow := newSuppressionDeps(t, customerID, []domain.ScreenMatch{match})

	if _, err := RunRescreeningBatch(context.Background(), deps, TriggerAPIRequest); err != nil {
		t.Fatal(err)
	}
	if workflow.runs[0].Degraded || len(workflow.runs[0].DegradedSources) != 0 {
		t.Fatalf("run = %+v, want no degradation claim when readiness cannot be assessed", workflow.runs[0])
	}
}
