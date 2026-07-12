package screening

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/ksuk/merlon/api/internal/metrics"
)

func TestComputeListFreshness_ReportsDaysSinceLastSuccess(t *testing.T) {
	lists := []ListImportStatus{
		{ListID: "ofac_sdn", ListType: "sanctions", LastSuccessAt: time.Now().Add(-1 * 24 * time.Hour)},
	}

	got := ComputeListFreshness(lists)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].StaleDays != 1 {
		t.Errorf("StaleDays = %d, want 1", got[0].StaleDays)
	}
	if got[0].NeedsOperationalAlert {
		t.Error("NeedsOperationalAlert = true, want false at 1 day stale")
	}
}

func TestComputeListFreshness_TriggersOperationalAlertAt3Days(t *testing.T) {
	lists := []ListImportStatus{
		{ListID: "eu_sanctions", ListType: "sanctions", LastSuccessAt: time.Now().Add(-3*24*time.Hour - time.Hour)},
	}

	got := ComputeListFreshness(lists)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].StaleDays < staleFailureThreshold {
		t.Fatalf("StaleDays = %d, want >= %d", got[0].StaleDays, staleFailureThreshold)
	}
	if !got[0].NeedsOperationalAlert {
		t.Error("NeedsOperationalAlert = false, want true at the 3-day threshold (the screening workflow default)")
	}
}

func TestMetric_ScreeningListStaleDays_RegisteredPerList(t *testing.T) {
	RecordListFreshnessMetrics([]ListFreshness{
		{ListID: "ofac_sdn", ListType: "sanctions", StaleDays: 0},
		{ListID: "pep_provider", ListType: "pep", StaleDays: 5, NeedsOperationalAlert: true},
	})

	if got := testutil.ToFloat64(metrics.ScreeningListStaleDays.WithLabelValues("sanctions")); got != 0 {
		t.Errorf("sanctions stale_days = %v, want 0", got)
	}
	if got := testutil.ToFloat64(metrics.ScreeningListStaleDays.WithLabelValues("pep")); got != 5 {
		t.Errorf("pep stale_days = %v, want 5", got)
	}
}
