package screening

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/metrics"
	"github.com/ksuk/merlon/api/internal/store"
)

// TestScreeningPipeline_ImportThenRescreenThenFreshness_E2E exercises the
// full WS-7 pipeline end-to-end at the package layer: import a sanctions
// list, run a CDD-tier-driven rescreening batch against it (persisting a
// hit to screening_results), and confirm the resulting list freshness is
// reported both by ComputeListFreshness and the
// merlon_screening_list_stale_days metric.
//
// This runs entirely against the in-memory ListStore/FailureTracker/
// ScreeningResultRepository and a fake screening engine. A real deployment
// wires the Rust engine over gRPC and (per Task 2's design note) a
// persistent list store; exercising that full stack requires `docker
// compose up` with Postgres and the Rust engine, which this sandboxed test
// environment cannot do — see rules_demo_test.go for the same tradeoff
// made elsewhere in this codebase.
func TestScreeningPipeline_ImportThenRescreenThenFreshness_E2E(t *testing.T) {
	ctx := context.Background()

	listStore := NewMemoryListStore()
	failureTracker := NewMemoryFailureTracker()

	adapters := map[string]ListAdapter{
		"mof_japan": &fakeAdapter{data: rawList("mof_japan", "MOF-001")},
	}
	importResult, err := RunImportJob(ctx, adapters, listStore, failureTracker)
	if err != nil {
		t.Fatalf("RunImportJob: %v", err)
	}
	if len(importResult.Outcomes) != 1 || !importResult.Outcomes[0].Imported {
		t.Fatalf("import outcomes = %+v, want single imported outcome", importResult.Outcomes)
	}

	highTierCustomer := domain.Customer{ID: "cust-high", RiskTier: riskTier(domain.RiskTierHigh)}
	customers := newFakeCustomerRepo(highTierCustomer)
	results := store.NewMemoryScreeningResultRepo()
	engine := &fakeScreeningEngine{
		matches: map[string][]domain.ScreenMatch{
			"cust-high": {{
				ListID: "mof_japan", EntryID: "MOF-001", MatchedName: "Kim Jong Un",
				Similarity: 0.97, ListType: "sanctions", Source: "test",
			}},
		},
	}

	batchResult, err := RunRescreeningBatch(ctx, SchedulerDeps{
		Customers: customers,
		Screening: engine,
		Results:   results,
		ListIDs:   []string{"mof_japan"},
	}, TriggerListUpdated)
	if err != nil {
		t.Fatalf("RunRescreeningBatch: %v", err)
	}
	if len(batchResult.Outcomes) != 1 || !batchResult.Outcomes[0].Screened {
		t.Fatalf("batch outcomes = %+v, want single screened outcome", batchResult.Outcomes)
	}

	persisted, err := results.ListByCustomer(ctx, "cust-high", 10, 0)
	if err != nil {
		t.Fatalf("ListByCustomer: %v", err)
	}
	if len(persisted) != 1 || persisted[0].EntryID != "MOF-001" || persisted[0].Status != domain.ScreeningResultStatusNew {
		t.Fatalf("persisted screening results = %+v, want single NEW hit against MOF-001", persisted)
	}

	data, err := listStore.GetList(ctx, "mof_japan")
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	lastSuccess, err := failureTracker.LastSuccessAt(ctx, "mof_japan")
	if err != nil {
		t.Fatalf("LastSuccessAt: %v", err)
	}

	freshness := ComputeListFreshness([]ListImportStatus{
		{ListID: "mof_japan", ListType: data.ListType, LastSuccessAt: lastSuccess},
	})
	if len(freshness) != 1 || freshness[0].StaleDays != 0 || freshness[0].NeedsOperationalAlert {
		t.Fatalf("freshness = %+v, want a single fresh (0-day) entry", freshness)
	}

	RecordListFreshnessMetrics(freshness)
	if got := testutil.ToFloat64(metrics.ScreeningListStaleDays.WithLabelValues("sanctions")); got != 0 {
		t.Errorf("merlon_screening_list_stale_days{list_type=sanctions} = %v, want 0", got)
	}
}
