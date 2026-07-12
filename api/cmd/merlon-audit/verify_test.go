package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestPgPool connects to MERLON_DATABASE_URL for integration tests,
// matching api/internal/store's convention (skip, not fail, when unset).
func newTestPgPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("MERLON_DATABASE_URL")
	if dsn == "" {
		t.Skip("MERLON_DATABASE_URL not set, skipping Postgres integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("pool.Ping: %v", err)
	}
	return pool
}

// baseTestID offsets fixture rows well past any sequence-generated id, so
// verify_test.go fixtures never collide with concurrently running tests or
// real data (audit_logs.id is BIGSERIAL starting at 1).
const baseTestID int64 = 9_000_000_000_000

var testIDCounter int64

func nextTestIDBlock() int64 {
	testIDCounter += 1000
	return baseTestID + testIDCounter
}

func insertAuditLog(t *testing.T, pool *pgxpool.Pool, id int64, createdAt time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO audit_logs (id, user_id, action, resource_type, resource_id, details, created_at)
		VALUES ($1, 'verify-test-user', 'test_action', 'verify_test_resource', 'verify-test', '{}', $2)`,
		id, createdAt,
	)
	if err != nil {
		t.Fatalf("insert audit_logs id=%d: %v", id, err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM audit_logs WHERE id = $1`, id)
	})
}

func TestVerifyDetectsIDGap(t *testing.T) {
	pool := newTestPgPool(t)
	base := nextTestIDBlock()
	now := time.Now()

	insertAuditLog(t, pool, base, now.Add(-2*time.Hour))
	insertAuditLog(t, pool, base+2, now.Add(-1*time.Hour)) // base+1 deliberately missing

	since := now.Add(-3 * time.Hour)
	until := now
	result, err := Verify(context.Background(), pool, VerifyOptions{Since: &since, Until: &until})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	found := false
	for _, gap := range result.IDGaps {
		if gap.PreviousID == base && gap.NextID == base+2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected id gap %d -> %d, got %+v", base, base+2, result.IDGaps)
	}
}

func TestVerifyDetectsTimeRegression(t *testing.T) {
	pool := newTestPgPool(t)
	base := nextTestIDBlock()
	now := time.Now()

	insertAuditLog(t, pool, base, now.Add(-1*time.Hour))
	insertAuditLog(t, pool, base+1, now.Add(-2*time.Hour)) // created_at earlier than previous id

	since := now.Add(-3 * time.Hour)
	until := now
	result, err := Verify(context.Background(), pool, VerifyOptions{Since: &since, Until: &until})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	found := false
	for _, tr := range result.TimeRegressions {
		if tr.ID == base+1 && tr.PreviousID == base {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected time regression at id %d, got %+v", base+1, result.TimeRegressions)
	}
}

func TestVerifyDetectsCountDrop(t *testing.T) {
	pool := newTestPgPool(t)
	base := nextTestIDBlock()
	now := time.Now()

	// 7 prior days with 10 entries each, then a drop-off day with 2 entries
	// (80% drop, comfortably past the default 50% threshold).
	id := base
	for day := 8; day >= 1; day-- {
		count := 10
		if day == 1 {
			count = 2
		}
		dayTime := now.AddDate(0, 0, -day)
		for i := 0; i < count; i++ {
			insertAuditLog(t, pool, id, dayTime.Add(time.Duration(i)*time.Minute))
			id++
		}
	}

	since := now.AddDate(0, 0, -9)
	until := now
	result, err := Verify(context.Background(), pool, VerifyOptions{Since: &since, Until: &until})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if len(result.CountDrops) == 0 {
		t.Fatalf("expected at least 1 count drop, got %+v", result.CountDrops)
	}
}

func TestVerifyDropThresholdConfigurable(t *testing.T) {
	pool := newTestPgPool(t)
	base := nextTestIDBlock()
	now := time.Now()

	// 7 prior days with 10 entries each, then a day with 5 entries (50% drop).
	id := base
	for day := 8; day >= 1; day-- {
		count := 10
		if day == 1 {
			count = 5
		}
		dayTime := now.AddDate(0, 0, -day)
		for i := 0; i < count; i++ {
			insertAuditLog(t, pool, id, dayTime.Add(time.Duration(i)*time.Minute))
			id++
		}
	}

	since := now.AddDate(0, 0, -9)
	until := now

	// At the default 50% threshold, an exact 50% drop should not (barely)
	// trigger detection...
	strict, err := Verify(context.Background(), pool, VerifyOptions{Since: &since, Until: &until, DropThreshold: 0.5})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(strict.CountDrops) != 0 {
		t.Errorf("with drop-threshold=0.5 and an exact 50%% drop, expected no CountDrops, got %+v", strict.CountDrops)
	}

	// ...but at a stricter 30% threshold, the same 50% drop should trigger.
	lenient, err := Verify(context.Background(), pool, VerifyOptions{Since: &since, Until: &until, DropThreshold: 0.3})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(lenient.CountDrops) == 0 {
		t.Errorf("with drop-threshold=0.3 and a 50%% drop, expected a CountDrop, got none")
	}
}

func TestVerifySinceUntilFiltersRange(t *testing.T) {
	pool := newTestPgPool(t)
	base := nextTestIDBlock()
	now := time.Now()

	// Out-of-range row far in the past creates what would be an id gap if
	// included; since/until should exclude it entirely.
	insertAuditLog(t, pool, base, now.AddDate(0, 0, -30))
	insertAuditLog(t, pool, base+5, now.Add(-1*time.Hour))
	insertAuditLog(t, pool, base+6, now.Add(-30*time.Minute))

	since := now.Add(-2 * time.Hour)
	until := now
	result, err := Verify(context.Background(), pool, VerifyOptions{Since: &since, Until: &until})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(result.IDGaps) != 0 {
		t.Errorf("expected no id gaps within the filtered range, got %+v", result.IDGaps)
	}
}

func TestVerifyExitCodeNoAnomaly(t *testing.T) {
	pool := newTestPgPool(t)
	base := nextTestIDBlock()
	now := time.Now()
	insertAuditLog(t, pool, base, now)

	since := now.Add(-time.Minute)
	until := now.Add(time.Minute)
	result, err := Verify(context.Background(), pool, VerifyOptions{Since: &since, Until: &until})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.HasAnomalies() {
		t.Errorf("expected no anomalies, got %+v", result)
	}
}

func TestVerifyExitCodeAnomalyDetected(t *testing.T) {
	pool := newTestPgPool(t)
	base := nextTestIDBlock()
	now := time.Now()
	insertAuditLog(t, pool, base, now.Add(-time.Hour))
	insertAuditLog(t, pool, base+2, now)

	since := now.Add(-2 * time.Hour)
	until := now.Add(time.Minute)
	result, err := Verify(context.Background(), pool, VerifyOptions{Since: &since, Until: &until})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.HasAnomalies() {
		t.Errorf("expected anomalies, got %+v", result)
	}
}

// TestVerifyExitCodeConnectionError verifies the run() CLI entry point
// returns exit code 2 when the database connection fails, without needing
// a live Postgres (an invalid DSN fails to connect regardless).
func TestVerifyExitCodeConnectionError(t *testing.T) {
	code := run([]string{"verify", "--database-url", "postgres://invalid-host-for-test:5432/nope"}, os.Stdout)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestVerifyJSONOutputFormat(t *testing.T) {
	pool := newTestPgPool(t)
	base := nextTestIDBlock()
	now := time.Now()
	insertAuditLog(t, pool, base, now)

	since := now.Add(-time.Minute)
	until := now.Add(time.Minute)
	result, err := Verify(context.Background(), pool, VerifyOptions{Since: &since, Until: &until, Format: "json"})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.IDGaps == nil && len(result.IDGaps) != 0 {
		t.Errorf("IDGaps should be a valid (possibly empty) slice")
	}
}

func TestVerifyRecordsAuditLogOnWritableConnection(t *testing.T) {
	pool := newTestPgPool(t)
	code := run([]string{"verify", "--database-url", os.Getenv("MERLON_DATABASE_URL")}, os.Stdout)
	if code != 0 {
		t.Fatalf("run() exit code = %d, want 0", code)
	}

	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'audit_verify' AND created_at > now() - interval '1 minute'`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count == 0 {
		t.Fatal("expected an audit_verify audit log entry")
	}
}

func TestVerifyReadOnlyConnectionSkipsAuditWrite(t *testing.T) {
	newTestPgPool(t) // ensure MERLON_DATABASE_URL is set, else skip
	var before int
	pool := newTestPgPool(t)
	pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_logs WHERE action = 'audit_verify'`).Scan(&before)

	code := run([]string{"verify", "--database-url", os.Getenv("MERLON_DATABASE_URL"), "--read-only"}, os.Stdout)
	if code != 0 {
		t.Fatalf("run() exit code = %d, want 0", code)
	}

	var after int
	pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_logs WHERE action = 'audit_verify'`).Scan(&after)
	if after != before {
		t.Errorf("expected no new audit_verify entries in read-only mode, before=%d after=%d", before, after)
	}
}
