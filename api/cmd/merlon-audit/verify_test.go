package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestPgPool creates an isolated database so integrity checks never inspect
// audit rows left by another package, prior test run, or concurrently running
// integration test.
func newTestPgPool(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("MERLON_DATABASE_URL")
	if dsn == "" {
		t.Skip("MERLON_DATABASE_URL not set, skipping Postgres integration test")
	}
	ctx := context.Background()
	adminConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)

	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	name := "merlon_audit_test_" + hex.EncodeToString(random)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, err := pgx.ConnectConfig(context.Background(), adminConfig)
		if err != nil {
			return
		}
		defer cleanup.Close(context.Background())
		_, _ = cleanup.Exec(context.Background(), `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, name)
		_, _ = cleanup.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize())
	})

	testDSN := databaseDSN(dsn, name)
	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("pool.Ping: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE audit_logs (
		id BIGSERIAL PRIMARY KEY,
		user_id VARCHAR(255),
		action VARCHAR(100) NOT NULL,
		resource_type VARCHAR(100) NOT NULL,
		resource_id VARCHAR(255),
		details JSONB DEFAULT '{}',
		ip_address INET,
		user_agent TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, testDSN
}

func databaseDSN(dsn, name string) string {
	parsed, err := url.Parse(dsn)
	if err == nil && parsed.Scheme != "" {
		parsed.Path = "/" + name
		return parsed.String()
	}
	return fmt.Sprintf("%s dbname=%s", dsn, name)
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
	pool, _ := newTestPgPool(t)
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
	pool, _ := newTestPgPool(t)
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
	pool, _ := newTestPgPool(t)
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
	pool, _ := newTestPgPool(t)
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
	pool, _ := newTestPgPool(t)
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
	pool, _ := newTestPgPool(t)
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
	pool, _ := newTestPgPool(t)
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
	pool, _ := newTestPgPool(t)
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
	pool, dsn := newTestPgPool(t)
	code := run([]string{"verify", "--database-url", dsn}, os.Stdout)
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
	pool, dsn := newTestPgPool(t)
	var before int
	pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_logs WHERE action = 'audit_verify'`).Scan(&before)

	code := run([]string{"verify", "--database-url", dsn, "--read-only"}, os.Stdout)
	if code != 0 {
		t.Fatalf("run() exit code = %d, want 0", code)
	}

	var after int
	pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_logs WHERE action = 'audit_verify'`).Scan(&after)
	if after != before {
		t.Errorf("expected no new audit_verify entries in read-only mode, before=%d after=%d", before, after)
	}
}
