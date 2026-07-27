package store

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/domain"
)

func TestMemoryUserRepo_CreateIfEmptyConcurrent(t *testing.T) {
	repo := NewMemoryUserRepo()
	assertOnlyOneUserCreated(t, repo)
}

func TestPgUserRepo_CreateIfEmptyConcurrent(t *testing.T) {
	pool, applicationName := newIsolatedUserTestPool(t, "repeatable read")
	repo := NewPgUserRepo(pool)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	var defaultIsolation string
	if err := pool.QueryRow(ctx, `SHOW default_transaction_isolation`).Scan(&defaultIsolation); err != nil {
		t.Fatalf("show default transaction isolation: %v", err)
	}
	if defaultIsolation != "repeatable read" {
		t.Fatalf("default transaction isolation = %q, want repeatable read", defaultIsolation)
	}

	// Hold the repository's lock before either call begins. Waiting until both
	// calls show up as advisory-lock waiters proves that both transactions have
	// started under the configured default before the winner is allowed to
	// commit.
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin blocker transaction: %v", err)
	}
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(ctx, initialAdministratorLockSQL); err != nil {
		t.Fatalf("acquire blocker lock: %v", err)
	}

	results := make(chan createIfEmptyResult, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		i := i
		go func() {
			<-start
			created, err := repo.CreateIfEmpty(ctx, testAdministrator(i))
			results <- createIfEmptyResult{created: created, err: err}
		}()
	}
	close(start)

	if err := waitForAdvisoryWaiters(ctx, pool, applicationName, 2); err != nil {
		t.Fatalf("wait for concurrent CreateIfEmpty calls: %v", err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatalf("release blocker lock: %v", err)
	}

	createdCount := 0
	for i := 0; i < 2; i++ {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatalf("CreateIfEmpty: %v", result.err)
			}
			if result.created {
				createdCount++
			}
		case <-ctx.Done():
			t.Fatalf("collect CreateIfEmpty result: %v", ctx.Err())
		}
	}
	if createdCount != 1 {
		t.Errorf("created count = %d, want 1", createdCount)
	}

	count, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Errorf("user count = %d, want 1", count)
	}
}

type createIfEmptyResult struct {
	created bool
	err     error
}

func assertOnlyOneUserCreated(t *testing.T, repo domain.UserRepository) {
	t.Helper()

	ctx := context.Background()
	start := make(chan struct{})
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			created, err := repo.CreateIfEmpty(ctx, testAdministrator(i))
			results <- created
			errs <- err
		}()
	}

	close(start)
	wg.Wait()
	close(results)
	close(errs)

	createdCount := 0
	for created := range results {
		if created {
			createdCount++
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("CreateIfEmpty: %v", err)
		}
	}
	if createdCount != 1 {
		t.Errorf("created count = %d, want 1", createdCount)
	}

	count, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Errorf("user count = %d, want 1", count)
	}
}

func testAdministrator(i int) *domain.User {
	now := time.Now()
	return &domain.User{
		ID:           newTestUUID(),
		Email:        "first-admin-" + string(rune('a'+i)) + "@example.com",
		PasswordHash: "test-password-hash",
		Role:         domain.RoleAdmin,
		Active:       true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func waitForAdvisoryWaiters(ctx context.Context, pool *pgxpool.Pool, applicationName string, want int) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		var count int
		err := pool.QueryRow(ctx, `SELECT COUNT(*)
			FROM pg_stat_activity
			WHERE datname = current_database()
			  AND application_name = $1
			  AND wait_event_type = 'Lock'
			  AND wait_event = 'advisory'`, applicationName).Scan(&count)
		if err != nil {
			return err
		}
		if count >= want {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("found %d advisory waiters, want %d: %w", count, want, ctx.Err())
		case <-ticker.C:
		}
	}
}

// newIsolatedUserTestPool gives this test its own schema so proving that an
// empty users table admits exactly one creator never deletes or depends on
// rows belonging to another integration test.
func newIsolatedUserTestPool(t *testing.T, defaultIsolation string) (*pgxpool.Pool, string) {
	t.Helper()

	dsn := os.Getenv("MERLON_DATABASE_URL")
	if dsn == "" {
		t.Skip("MERLON_DATABASE_URL not set, skipping Postgres integration test")
	}

	adminPool := newTestPgPool(t)
	ctx := context.Background()
	schema := "merlon_users_test_" + newTestUUID()
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(context.Background(), `DROP SCHEMA `+quotedSchema+` CASCADE`); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse database config: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.ConnConfig.RuntimeParams["default_transaction_isolation"] = defaultIsolation
	applicationName := "merlon-users-test-" + newTestUUID()
	cfg.ConnConfig.RuntimeParams["application_name"] = applicationName
	if cfg.MaxConns < 4 {
		cfg.MaxConns = 4
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create isolated pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping isolated pool: %v", err)
	}

	if _, err := pool.Exec(ctx, `CREATE TABLE users (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL,
		active BOOLEAN NOT NULL,
		created_at TIMESTAMPTZ NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL
	)`); err != nil {
		t.Fatalf("create users table: %v", err)
	}

	return pool, applicationName
}
