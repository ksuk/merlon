package store

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// sourceFixture is one importer state expressed in the terms each store
// records it. The point of this file is that both stores must classify the
// same underlying facts identically: before ClassifyScreeningSource existed,
// the memory store could not produce unreadable at all, so the same
// deployment state was reported differently depending on which store backed
// the API.
type sourceFixture struct {
	name string
	// snapshot is what the memory store holds.
	snapshot domain.ScreeningSourceSnapshot
	// importedAt/successAt/failures are the PostgreSQL row equivalents.
	importedAt *time.Time
	successAt  *time.Time
	failures   int
	want       domain.ScreeningSourceState
}

func sourceFixtures(now time.Time) []sourceFixture {
	hourAgo := now.Add(-time.Hour)
	weekAgo := now.Add(-7 * 24 * time.Hour)
	return []sourceFixture{
		{
			name:     "never imported",
			snapshot: domain.ScreeningSourceSnapshot{SnapshotMissing: true},
			want:     domain.ScreeningSourceNeverImported,
		},
		{
			name:       "fresh import",
			snapshot:   domain.ScreeningSourceSnapshot{LastSuccessAt: &hourAgo},
			importedAt: &hourAgo, successAt: &hourAgo,
			want: domain.ScreeningSourceReady,
		},
		{
			name:       "import older than the window",
			snapshot:   domain.ScreeningSourceSnapshot{LastSuccessAt: &weekAgo},
			importedAt: &weekAgo, successAt: &weekAgo,
			want: domain.ScreeningSourceStale,
		},
		{
			name:       "recent import with failures since",
			snapshot:   domain.ScreeningSourceSnapshot{LastSuccessAt: &hourAgo, ConsecutiveFailures: 3},
			importedAt: &hourAgo, successAt: &hourAgo, failures: 3,
			want: domain.ScreeningSourceFailed,
		},
		{
			name:      "successful import whose body is gone",
			snapshot:  domain.ScreeningSourceSnapshot{LastSuccessAt: &hourAgo, SnapshotMissing: true},
			successAt: &hourAgo,
			want:      domain.ScreeningSourceUnreadable,
		},
	}
}

func TestScreeningSourceClassificationAgreesAcrossStores(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	fixtures := sourceFixtures(now)
	threshold := func(string) time.Duration { return 72 * time.Hour }

	memory := NewMemoryWave3Repo()
	ids := make([]string, 0, len(fixtures))
	for i, fixture := range fixtures {
		id := "fixture-source-" + string(rune('a'+i))
		ids = append(ids, id)
		snapshot := fixture.snapshot
		snapshot.ListID = id
		memory.SetScreeningSourceSnapshot(snapshot)
	}

	memoryStates, err := memory.ListScreeningSources(ctx, ids, threshold)
	if err != nil {
		t.Fatal(err)
	}
	if len(memoryStates) != len(fixtures) {
		t.Fatalf("memory returned %d rows for %d configured sources", len(memoryStates), len(fixtures))
	}
	for i, fixture := range fixtures {
		if memoryStates[i].OperationalState != fixture.want {
			t.Errorf("memory %s: state = %q, want %q", fixture.name, memoryStates[i].OperationalState, fixture.want)
		}
	}

	// The PostgreSQL half only runs where a database is configured; the
	// memory assertions above still hold on their own.
	pool := newTestPgPool(t)
	for i, fixture := range fixtures {
		id := ids[i]
		if fixture.importedAt != nil {
			if _, err := pool.Exec(ctx, `INSERT INTO screening_list_snapshots(list_id,list_type,name,source,entries,imported_at) VALUES($1,'sanctions',$1,'test','[]'::jsonb,$2) ON CONFLICT(list_id) DO UPDATE SET imported_at=EXCLUDED.imported_at`, id, *fixture.importedAt); err != nil {
				t.Fatal(err)
			}
		}
		if fixture.successAt != nil || fixture.failures > 0 {
			if _, err := pool.Exec(ctx, `INSERT INTO screening_list_failures(list_id,consecutive_failures,last_success_at) VALUES($1,$2,$3) ON CONFLICT(list_id) DO UPDATE SET consecutive_failures=EXCLUDED.consecutive_failures,last_success_at=EXCLUDED.last_success_at`, id, fixture.failures, fixture.successAt); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM screening_list_snapshots WHERE list_id = ANY($1)`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM screening_list_failures WHERE list_id = ANY($1)`, ids)
	})

	pgStates, err := NewPgWave3Repo(pool).ListScreeningSources(ctx, ids, threshold)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]domain.ScreeningSourceState{}
	for _, status := range pgStates {
		byID[status.ListID] = status.OperationalState
	}
	for i, fixture := range fixtures {
		got := byID[ids[i]]
		if got != fixture.want {
			t.Errorf("postgres %s: state = %q, want %q", fixture.name, got, fixture.want)
		}
		if got != memoryStates[i].OperationalState {
			t.Errorf("%s: postgres says %q, memory says %q for the same facts", fixture.name, got, memoryStates[i].OperationalState)
		}
	}
}

// A per-source window is the whole reason ListScreeningSources takes a
// function: one source may be stale at an age another is still fresh at.
func TestScreeningSourceThresholdIsPerSource(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	twoDaysAgo := now.Add(-48 * time.Hour)

	memory := NewMemoryWave3Repo()
	memory.SetScreeningSourceSnapshot(domain.ScreeningSourceSnapshot{ListID: "daily_feed", LastSuccessAt: &twoDaysAgo})
	memory.SetScreeningSourceSnapshot(domain.ScreeningSourceSnapshot{ListID: "monthly_feed", LastSuccessAt: &twoDaysAgo})

	statuses, err := memory.ListScreeningSources(ctx, []string{"daily_feed", "monthly_feed"}, func(id string) time.Duration {
		if id == "daily_feed" {
			return 24 * time.Hour
		}
		return 30 * 24 * time.Hour
	})
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].OperationalState != domain.ScreeningSourceStale {
		t.Errorf("daily_feed at 48h = %q, want stale against its 24h window", statuses[0].OperationalState)
	}
	if statuses[1].OperationalState != domain.ScreeningSourceReady {
		t.Errorf("monthly_feed at 48h = %q, want ready against its 30d window", statuses[1].OperationalState)
	}
	if statuses[0].FreshnessThresholdSeconds == statuses[1].FreshnessThresholdSeconds {
		t.Error("both sources reported the same threshold; the per-source window was not applied")
	}
}
