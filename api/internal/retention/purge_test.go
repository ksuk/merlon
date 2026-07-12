package retention

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

type failingAuditRepo struct{ err error }

func (r *failingAuditRepo) Create(context.Context, *domain.AuditEntry) error { return r.err }
func (r *failingAuditRepo) List(context.Context, domain.AuditListFilter) ([]domain.AuditEntry, error) {
	return nil, nil
}

// fakeTarget models the retention lifecycle: an expired record is marked on
// the first pass and only physically removed after the grace period.
type fakeTarget struct {
	recordTimes []time.Time
	markedAt    map[int]time.Time
	deleted     map[int]bool
}

func (f *fakeTarget) purge(_ context.Context, cutoff, now time.Time) (logicallyDeleted, physicallyDeleted int, err error) {
	if f.markedAt == nil {
		f.markedAt = make(map[int]time.Time)
	}
	if f.deleted == nil {
		f.deleted = make(map[int]bool)
	}
	for i, ts := range f.recordTimes {
		if ts.After(cutoff) || f.deleted[i] {
			continue
		}
		marked, ok := f.markedAt[i]
		if !ok {
			f.markedAt[i] = now
			logicallyDeleted++
			continue
		}
		if !now.Before(marked.Add(PhysicalDeletionGracePeriod)) {
			f.deleted[i] = true
			physicallyDeleted++
		}
	}
	return logicallyDeleted, physicallyDeleted, nil
}

func newPurgeJob(target *fakeTarget) *PurgeJob {
	return &PurgeJob{
		Retention: store.NewMemoryRetentionRepo(),
		Audit:     store.NewMemoryAuditRepo(),
		Targets:   map[string]PurgeFunc{"customer_data": target.purge},
	}
}

func findPurgeResult(t *testing.T, results []PurgeResult) PurgeResult {
	t.Helper()
	for _, result := range results {
		if result.Category == "customer_data" {
			return result
		}
	}
	t.Fatal("expected a customer_data result")
	return PurgeResult{}
}

// TestPurgeJobSkipsWithinRetentionPeriod verifies records newer than the
// category's cutoff are not marked or deleted.
func TestPurgeJobSkipsWithinRetentionPeriod(t *testing.T) {
	now := time.Now()
	target := &fakeTarget{recordTimes: []time.Time{now.AddDate(0, 0, -10)}}

	results, err := newPurgeJob(target).Run(context.Background(), now)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := findPurgeResult(t, results)
	if got.LogicallyDeleted != 0 || got.PhysicallyDeleted != 0 {
		t.Errorf("got %+v, want 0 deletions (record within retention period)", got)
	}
}

// TestPurgeJobMarksExpiredRecordsBeforePhysicalDeletion fixes the 30-day
// grace period: expiration makes data unavailable before it is irreversible.
func TestPurgeJobMarksExpiredRecordsBeforePhysicalDeletion(t *testing.T) {
	now := time.Now()
	target := &fakeTarget{recordTimes: []time.Time{now.AddDate(0, 0, -3000)}}

	results, err := newPurgeJob(target).Run(context.Background(), now)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := findPurgeResult(t, results)
	if got.LogicallyDeleted != 1 || got.PhysicallyDeleted != 0 {
		t.Errorf("got %+v, want one mark and no physical deletion", got)
	}
}

func TestPurgeJobPhysicallyDeletesAfterGracePeriod(t *testing.T) {
	now := time.Now()
	target := &fakeTarget{recordTimes: []time.Time{now.AddDate(0, 0, -3000)}}
	job := newPurgeJob(target)
	if _, err := job.Run(context.Background(), now); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	results, err := job.Run(context.Background(), now.Add(PhysicalDeletionGracePeriod))
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	got := findPurgeResult(t, results)
	if got.LogicallyDeleted != 0 || got.PhysicallyDeleted != 1 {
		t.Errorf("got %+v, want one physical deletion after grace period", got)
	}
}

// TestPurgeJobLogsExecutionHistory verifies the purge run itself is recorded
// in the audit log.
func TestPurgeJobLogsExecutionHistory(t *testing.T) {
	now := time.Now()
	audit := store.NewMemoryAuditRepo()
	target := &fakeTarget{recordTimes: []time.Time{now.AddDate(0, 0, -3000)}}
	job := &PurgeJob{
		Retention: store.NewMemoryRetentionRepo(),
		Audit:     audit,
		Targets:   map[string]PurgeFunc{"customer_data": target.purge},
	}

	if _, err := job.Run(context.Background(), now); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries, err := audit.List(context.Background(), domain.AuditListFilter{ResourceType: "retention_policy", Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, e := range entries {
		if e.Action == "purge_execution" {
			return
		}
	}
	t.Fatalf("expected a purge_execution audit entry, got %+v", entries)
}

func TestPurgeJobDoesNotMutateWhenStartAuditFails(t *testing.T) {
	wantErr := errors.New("audit unavailable")
	called := false
	job := &PurgeJob{
		Retention: store.NewMemoryRetentionRepo(),
		Audit:     &failingAuditRepo{err: wantErr},
		Targets: map[string]PurgeFunc{"customer_data": func(context.Context, time.Time, time.Time) (int, int, error) {
			called = true
			return 1, 1, nil
		}},
	}

	if _, err := job.Run(context.Background(), time.Now()); !errors.Is(err, wantErr) {
		t.Fatalf("Run error = %v, want %v", err, wantErr)
	}
	if called {
		t.Fatal("purge target called before audit availability was established")
	}
}

func TestPurgeJobRequiresAuditRepository(t *testing.T) {
	called := false
	job := &PurgeJob{
		Retention: store.NewMemoryRetentionRepo(),
		Targets: map[string]PurgeFunc{"customer_data": func(context.Context, time.Time, time.Time) (int, int, error) {
			called = true
			return 1, 1, nil
		}},
	}

	if _, err := job.Run(context.Background(), time.Now()); err == nil {
		t.Fatal("Run should reject a missing audit repository")
	}
	if called {
		t.Fatal("purge target called without an audit repository")
	}
}

func TestPurgeJobSkipsCategoriesWithoutTarget(t *testing.T) {
	job := &PurgeJob{
		Retention: store.NewMemoryRetentionRepo(),
		Audit:     store.NewMemoryAuditRepo(),
		Targets:   map[string]PurgeFunc{},
	}

	results, err := job.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0 (no targets registered)", len(results))
	}
}

var _ domain.RetentionRepository = (*store.MemoryRetentionRepo)(nil)
