package domain

import (
	"testing"
	"time"
)

func at(now time.Time, d time.Duration) *time.Time {
	value := now.Add(d)
	return &value
}

func TestClassifyScreeningSourceCoversEveryState(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	threshold := 72 * time.Hour

	tests := []struct {
		name     string
		snapshot ScreeningSourceSnapshot
		want     ScreeningSourceState
	}{
		{
			name:     "never imported",
			snapshot: ScreeningSourceSnapshot{ListID: "ofac_sdn", SnapshotMissing: true},
			want:     ScreeningSourceNeverImported,
		},
		{
			name:     "ready",
			snapshot: ScreeningSourceSnapshot{ListID: "ofac_sdn", LastSuccessAt: at(now, -time.Hour)},
			want:     ScreeningSourceReady,
		},
		{
			name:     "stale past the threshold",
			snapshot: ScreeningSourceSnapshot{ListID: "ofac_sdn", LastSuccessAt: at(now, -73*time.Hour)},
			want:     ScreeningSourceStale,
		},
		{
			name:     "unreadable body",
			snapshot: ScreeningSourceSnapshot{ListID: "ofac_sdn", LastSuccessAt: at(now, -time.Hour), SnapshotUnreadable: true},
			want:     ScreeningSourceUnreadable,
		},
		{
			name:     "successful import whose body is gone",
			snapshot: ScreeningSourceSnapshot{ListID: "ofac_sdn", LastSuccessAt: at(now, -time.Hour), SnapshotMissing: true},
			want:     ScreeningSourceUnreadable,
		},
		{
			name:     "failed import",
			snapshot: ScreeningSourceSnapshot{ListID: "ofac_sdn", LastSuccessAt: at(now, -time.Hour), ConsecutiveFailures: 2},
			want:     ScreeningSourceFailed,
		},
		{
			name:     "status tracker unreadable",
			snapshot: ScreeningSourceSnapshot{ListID: "ofac_sdn", StatusUnavailable: true},
			want:     ScreeningSourceUnavailable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyScreeningSource(tc.snapshot, threshold, now)
			if got.OperationalState != tc.want {
				t.Fatalf("state = %q, want %q", got.OperationalState, tc.want)
			}
			if got.ListID != tc.snapshot.ListID || !got.Configured {
				t.Fatalf("status = %+v, want the configured source echoed back", got)
			}
			if got.FreshnessThresholdSeconds != int64(threshold.Seconds()) {
				t.Fatalf("threshold = %d, want %d", got.FreshnessThresholdSeconds, int64(threshold.Seconds()))
			}
			if got.OperationalState == ScreeningSourceReady && !got.IsReady() {
				t.Fatal("IsReady disagreed with the state it was derived from")
			}
		})
	}
}

// The threshold decides stale-vs-ready, so the boundary itself is pinned:
// exactly at the window the source is still ready, one second past it is not.
func TestClassifyScreeningSourceFreshnessBoundary(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	threshold := 24 * time.Hour

	tests := []struct {
		age  time.Duration
		want ScreeningSourceState
	}{
		{23*time.Hour + 59*time.Minute + 59*time.Second, ScreeningSourceReady},
		{24 * time.Hour, ScreeningSourceReady},
		{24*time.Hour + time.Second, ScreeningSourceStale},
	}
	for _, tc := range tests {
		got := ClassifyScreeningSource(ScreeningSourceSnapshot{ListID: "un_sc", LastSuccessAt: at(now, -tc.age)}, threshold, now)
		if got.OperationalState != tc.want {
			t.Errorf("age %s: state = %q, want %q", tc.age, got.OperationalState, tc.want)
		}
	}
}

func TestClassifyScreeningSourceClampsFutureImports(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	got := ClassifyScreeningSource(ScreeningSourceSnapshot{ListID: "un_sc", LastSuccessAt: at(now, time.Hour)}, 72*time.Hour, now)
	if got.AgeSeconds == nil || *got.AgeSeconds != 0 {
		t.Fatalf("age = %v, want 0: clock skew must not present as an impossibly fresh source", got.AgeSeconds)
	}
	if got.OperationalState != ScreeningSourceReady {
		t.Fatalf("state = %q, want ready", got.OperationalState)
	}
}

// Each unready state carries a diagnostic, because "unavailable" with an empty
// explanation gives an operator nothing to act on.
func TestClassifyScreeningSourceAlwaysExplainsAnUnreadyState(t *testing.T) {
	now := time.Now().UTC()
	for _, snapshot := range []ScreeningSourceSnapshot{
		{ListID: "a", StatusUnavailable: true},
		{ListID: "b", SnapshotUnreadable: true},
		{ListID: "c", LastSuccessAt: &now, SnapshotMissing: true},
		{ListID: "d", ConsecutiveFailures: 1},
	} {
		got := ClassifyScreeningSource(snapshot, time.Hour, now)
		if got.Diagnostic == "" {
			t.Errorf("%s: state %q has no diagnostic", snapshot.ListID, got.OperationalState)
		}
	}
}

// A caller-supplied diagnostic is preserved rather than overwritten by the
// generic one: the importer's own message is the more specific of the two.
func TestClassifyScreeningSourceKeepsSpecificDiagnostic(t *testing.T) {
	now := time.Now().UTC()
	got := ClassifyScreeningSource(ScreeningSourceSnapshot{ListID: "a", ConsecutiveFailures: 3, Diagnostic: "http 503 from provider"}, time.Hour, now)
	if got.Diagnostic != "http 503 from provider" {
		t.Fatalf("diagnostic = %q, want the importer's own message", got.Diagnostic)
	}
}
