package retention

import (
	"context"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/store"
)

// fakeTarget simulates a per-category purge against an in-memory list of
// record timestamps: records at or before cutoff are "logically deleted",
// and (for simplicity in this fixture) also counted as physically deleted
// in the same pass.
type fakeTarget struct {
	recordTimes []time.Time
}

func (f *fakeTarget) purge(_ context.Context, cutoff time.Time) (logicallyDeleted, physicallyDeleted int, err error) {
	for _, ts := range f.recordTimes {
		if !ts.After(cutoff) {
			logicallyDeleted++
			physicallyDeleted++
		}
	}
	return logicallyDeleted, physicallyDeleted, nil
}

// TestPurgeJobSkipsWithinRetentionPeriod verifies records newer than the
// category's cutoff (now - retention_days) are not purged (audit.md RET-003).
func TestPurgeJobSkipsWithinRetentionPeriod(t *testing.T) {
	now := time.Now()
	target := &fakeTarget{recordTimes: []time.Time{now.AddDate(0, 0, -10)}} // well within 2555-day retention

	job := &PurgeJob{
		Retention: store.NewMemoryRetentionRepo(),
		Audit:     store.NewMemoryAuditRepo(),
		Targets:   map[string]PurgeFunc{"customer_data": target.purge},
	}

	results, err := job.Run(context.Background(), now)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var got *PurgeResult
	for i := range results {
		if results[i].Category == "customer_data" {
			got = &results[i]
		}
	}
	if got == nil {
		t.Fatal("expected a customer_data result")
	}
	if got.LogicallyDeleted != 0 || got.PhysicallyDeleted != 0 {
		t.Errorf("got %+v, want 0 deletions (record within retention period)", got)
	}
}

// TestPurgeJobPurgesRecordsPastRetention verifies records older than the
// cutoff are purged.
func TestPurgeJobPurgesRecordsPastRetention(t *testing.T) {
	now := time.Now()
	target := &fakeTarget{recordTimes: []time.Time{now.AddDate(0, 0, -3000)}} // past the 2555-day retention

	job := &PurgeJob{
		Retention: store.NewMemoryRetentionRepo(),
		Audit:     store.NewMemoryAuditRepo(),
		Targets:   map[string]PurgeFunc{"customer_data": target.purge},
	}

	results, err := job.Run(context.Background(), now)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var got *PurgeResult
	for i := range results {
		if results[i].Category == "customer_data" {
			got = &results[i]
		}
	}
	if got == nil {
		t.Fatal("expected a customer_data result")
	}
	if got.LogicallyDeleted != 1 || got.PhysicallyDeleted != 1 {
		t.Errorf("got %+v, want 1 deletion (record past retention period)", got)
	}
}

// TestPurgeJobLogsExecutionHistory verifies the purge run itself is recorded
// in the audit log (audit.md §6 自動パージ: パージの設定と実行履歴は監査ロ
// グに記録する).
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

	entries, err := audit.List(context.Background(), "retention_policy", "", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Action == "purge_execution" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a purge_execution audit entry, got %+v", entries)
	}
}

// TestPurgeJobSkipsCategoriesWithoutTarget verifies categories with no
// PurgeFunc registered (the framework's extension point for tables not yet
// wired, e.g. pending WS-11) are silently skipped rather than erroring.
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
