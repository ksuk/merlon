package store

import (
	"context"
	"errors"
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

func TestMemoryRefreshTokenRepo_CreateSessionWithAuditIsAtomic(t *testing.T) {
	audit := NewMemoryAuditRepo()
	repo := NewMemoryRefreshTokenRepoWithAudit(audit)
	now := time.Now()
	existing := &domain.RefreshToken{
		ID: "existing", UserID: "user-1", TokenHash: "existing-hash", TokenFamily: "existing-family",
		SessionRole: domain.RoleAdmin, ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	if _, err := repo.CreateSession(context.Background(), existing, 1); err != nil {
		t.Fatalf("CreateSession existing: %v", err)
	}
	audit.SetCreateFailure(errors.New("audit unavailable"))
	candidate := &domain.RefreshToken{
		ID: "candidate", UserID: "user-1", TokenHash: "candidate-hash", TokenFamily: "candidate-family",
		SessionRole: domain.RoleAdmin, ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(time.Second),
	}
	entry := &domain.AuditEntry{UserID: "user-1", Action: "login_success", ResourceType: "auth", ResourceID: "user-1", CreatedAt: now.Add(time.Second)}
	if _, err := repo.CreateSessionWithAudit(context.Background(), candidate, 1, entry); err == nil {
		t.Fatal("CreateSessionWithAudit succeeded when audit failed")
	}
	active, err := repo.IsFamilyActive(context.Background(), existing.TokenFamily)
	if err != nil {
		t.Fatalf("IsFamilyActive existing: %v", err)
	}
	if !active {
		t.Fatal("existing session was evicted despite audit rollback")
	}
	if _, err := repo.GetByHash(context.Background(), candidate.TokenHash); err == nil {
		t.Fatal("candidate session persisted despite audit rollback")
	}
}

func TestMemoryRefreshTokenRepo_RevokeUserSessionsWithAuditIsAtomic(t *testing.T) {
	audit := NewMemoryAuditRepo()
	repo := NewMemoryRefreshTokenRepoWithAudit(audit)
	now := time.Now()
	token := &domain.RefreshToken{
		ID: "token-1", UserID: "user-1", TokenHash: "hash-1", TokenFamily: "family-1",
		SessionRole: domain.RoleAdmin, ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	if _, err := repo.CreateSession(context.Background(), token, 5); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	audit.SetCreateFailure(errors.New("audit unavailable"))
	entry := &domain.AuditEntry{UserID: "admin-1", Action: "user_wide_session_revocation", ResourceType: "auth", ResourceID: "user-1", CreatedAt: now}
	if _, err := repo.RevokeUserSessionsWithAudit(context.Background(), "user-1", entry); err == nil {
		t.Fatal("RevokeUserSessionsWithAudit succeeded when audit failed")
	}
	active, err := repo.IsFamilyActive(context.Background(), token.TokenFamily)
	if err != nil {
		t.Fatalf("IsFamilyActive: %v", err)
	}
	if !active {
		t.Fatal("session was revoked despite audit rollback")
	}
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
	if _, err := pool.Exec(ctx, `CREATE TABLE refresh_tokens (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash TEXT NOT NULL UNIQUE,
		token_family TEXT NOT NULL,
		session_role TEXT NOT NULL DEFAULT '',
		expires_at TIMESTAMPTZ NOT NULL,
		revoked_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL
	)`); err != nil {
		t.Fatalf("create refresh_tokens table: %v", err)
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
		t.Fatalf("create audit_logs table: %v", err)
	}

	return pool, applicationName
}

func TestPgRefreshTokenRepo_PreservesSessionRoleAcrossRotation(t *testing.T) {
	pool, _ := newIsolatedUserTestPool(t, "read committed")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	user := testAdministrator(0)
	if err := NewPgUserRepo(pool).Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo := NewPgRefreshTokenRepo(pool)
	now := time.Now().UTC()
	initial := &domain.RefreshToken{
		ID: newTestUUID(), UserID: user.ID, TokenHash: "role-initial", TokenFamily: "role-family",
		SessionRole: domain.RoleAdmin, ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	if _, err := repo.CreateSession(ctx, initial, 5); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	replacement := &domain.RefreshToken{ID: newTestUUID(), TokenHash: "role-replacement", ExpiresAt: now.Add(2 * time.Hour), CreatedAt: now.Add(time.Microsecond)}
	if _, err := repo.Rotate(ctx, initial.TokenHash, replacement); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	stored, err := repo.GetByHash(ctx, replacement.TokenHash)
	if err != nil {
		t.Fatalf("GetByHash replacement: %v", err)
	}
	if stored.SessionRole != domain.RoleAdmin {
		t.Fatalf("session role = %q, want %q", stored.SessionRole, domain.RoleAdmin)
	}
}

func TestPgRefreshTokenRepo_CreateSessionWithAuditRollsBackBothChanges(t *testing.T) {
	pool, _ := newIsolatedUserTestPool(t, "read committed")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	user := testAdministrator(0)
	if err := NewPgUserRepo(pool).Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo := NewPgRefreshTokenRepo(pool)
	now := time.Now().UTC()
	existing := &domain.RefreshToken{ID: newTestUUID(), UserID: user.ID, TokenHash: "audit-existing", TokenFamily: "audit-existing-family", SessionRole: domain.RoleAdmin, ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	if _, err := repo.CreateSession(ctx, existing, 1); err != nil {
		t.Fatalf("CreateSession existing: %v", err)
	}
	candidate := &domain.RefreshToken{ID: newTestUUID(), UserID: user.ID, TokenHash: "audit-candidate", TokenFamily: "audit-candidate-family", SessionRole: domain.RoleAdmin, ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(time.Second)}
	badAudit := &domain.AuditEntry{UserID: user.ID, Action: "login_success", ResourceType: "auth", ResourceID: user.ID, IPAddress: "not-an-ip", CreatedAt: now.Add(time.Second)}
	if _, err := repo.CreateSessionWithAudit(ctx, candidate, 1, badAudit); err == nil {
		t.Fatal("CreateSessionWithAudit succeeded with invalid audit entry")
	}
	active, err := repo.IsFamilyActive(ctx, existing.TokenFamily)
	if err != nil {
		t.Fatalf("IsFamilyActive existing: %v", err)
	}
	if !active {
		t.Fatal("existing family was evicted despite transaction rollback")
	}
	if _, err := repo.GetByHash(ctx, candidate.TokenHash); err == nil {
		t.Fatal("candidate token persisted despite transaction rollback")
	}
}

func TestPgRefreshTokenRepo_RevokeUserSessionsWithAuditRollsBackBothChanges(t *testing.T) {
	pool, _ := newIsolatedUserTestPool(t, "read committed")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	user := testAdministrator(0)
	if err := NewPgUserRepo(pool).Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo := NewPgRefreshTokenRepo(pool)
	now := time.Now().UTC()
	token := &domain.RefreshToken{ID: newTestUUID(), UserID: user.ID, TokenHash: "revoke-audit-token", TokenFamily: "revoke-audit-family", SessionRole: domain.RoleAdmin, ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	if _, err := repo.CreateSession(ctx, token, 5); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	badAudit := &domain.AuditEntry{UserID: user.ID, Action: "user_wide_session_revocation", ResourceType: "auth", ResourceID: user.ID, IPAddress: "not-an-ip", CreatedAt: now}
	if _, err := repo.RevokeUserSessionsWithAudit(ctx, user.ID, badAudit); err == nil {
		t.Fatal("RevokeUserSessionsWithAudit succeeded with invalid audit entry")
	}
	active, err := repo.IsFamilyActive(ctx, token.TokenFamily)
	if err != nil {
		t.Fatalf("IsFamilyActive: %v", err)
	}
	if !active {
		t.Fatal("family was revoked despite transaction rollback")
	}
}

func TestPgRefreshTokenRepo_ConcurrentRotationRevokesFamily(t *testing.T) {
	pool, _ := newIsolatedUserTestPool(t, "read committed")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	user := testAdministrator(0)
	if err := NewPgUserRepo(pool).Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo := NewPgRefreshTokenRepo(pool)
	now := time.Now().UTC()
	initial := &domain.RefreshToken{ID: newTestUUID(), UserID: user.ID, TokenHash: "initial-hash", TokenFamily: "family-a", ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	if _, err := repo.CreateSession(ctx, initial, 5); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			replacement := &domain.RefreshToken{ID: newTestUUID(), TokenHash: fmt.Sprintf("replacement-%d", i), ExpiresAt: now.Add(2 * time.Hour), CreatedAt: now.Add(time.Duration(i+1) * time.Microsecond)}
			_, err := repo.Rotate(ctx, initial.TokenHash, replacement)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	reuses := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domain.ErrRefreshTokenReuse):
			reuses++
		default:
			t.Fatalf("Rotate: %v", err)
		}
	}
	if successes != 1 || reuses != 1 {
		t.Fatalf("rotation outcomes successes/reuses = %d/%d, want 1/1", successes, reuses)
	}
	active, err := repo.IsFamilyActive(ctx, initial.TokenFamily)
	if err != nil {
		t.Fatalf("IsFamilyActive: %v", err)
	}
	if active {
		t.Fatal("family remains active after concurrent token reuse")
	}
}

func TestPgRefreshTokenRepo_ConcurrentSessionLimitIsAtomic(t *testing.T) {
	pool, _ := newIsolatedUserTestPool(t, "read committed")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	user := testAdministrator(0)
	if err := NewPgUserRepo(pool).Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo := NewPgRefreshTokenRepo(pool)
	now := time.Now().UTC()
	for i := 0; i < 4; i++ {
		token := &domain.RefreshToken{ID: newTestUUID(), UserID: user.ID, TokenHash: fmt.Sprintf("seed-%d", i), TokenFamily: fmt.Sprintf("seed-family-%d", i), ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(time.Duration(i) * time.Microsecond)}
		if _, err := repo.CreateSession(ctx, token, 5); err != nil {
			t.Fatalf("seed CreateSession: %v", err)
		}
	}

	start := make(chan struct{})
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			token := &domain.RefreshToken{ID: newTestUUID(), UserID: user.ID, TokenHash: fmt.Sprintf("concurrent-%d", i), TokenFamily: fmt.Sprintf("concurrent-family-%d", i), ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(time.Duration(i+10) * time.Microsecond)}
			_, err := repo.CreateSession(ctx, token, 5)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent CreateSession: %v", err)
		}
	}

	count, err := repo.CountActiveByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("CountActiveByUser: %v", err)
	}
	if count != 5 {
		t.Fatalf("active sessions = %d, want 5", count)
	}
}

func TestPgRefreshTokenRepo_RevocationIsSharedAndFutureSessionSurvives(t *testing.T) {
	pool, _ := newIsolatedUserTestPool(t, "read committed")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	user := testAdministrator(0)
	if err := NewPgUserRepo(pool).Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	first := NewPgRefreshTokenRepo(pool)
	second := NewPgRefreshTokenRepo(pool)
	now := time.Now().UTC()
	old := &domain.RefreshToken{ID: newTestUUID(), UserID: user.ID, TokenHash: "old", TokenFamily: "old-family", ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	if _, err := first.CreateSession(ctx, old, 5); err != nil {
		t.Fatalf("CreateSession(old): %v", err)
	}
	families, err := first.RevokeUserSessions(ctx, user.ID)
	if err != nil {
		t.Fatalf("RevokeUserSessions: %v", err)
	}
	if len(families) != 1 || families[0] != old.TokenFamily {
		t.Fatalf("revoked families = %v, want [%s]", families, old.TokenFamily)
	}
	active, err := second.IsFamilyActive(ctx, old.TokenFamily)
	if err != nil {
		t.Fatalf("IsFamilyActive(old): %v", err)
	}
	if active {
		t.Fatal("revocation was not visible through second repository instance")
	}

	future := &domain.RefreshToken{ID: newTestUUID(), UserID: user.ID, TokenHash: "future", TokenFamily: "future-family", ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(time.Microsecond)}
	if _, err := second.CreateSession(ctx, future, 5); err != nil {
		t.Fatalf("CreateSession(future): %v", err)
	}
	active, err = first.IsFamilyActive(ctx, future.TokenFamily)
	if err != nil {
		t.Fatalf("IsFamilyActive(future): %v", err)
	}
	if !active {
		t.Fatal("future session was revoked by earlier user-wide revocation")
	}
}

func TestPgRefreshTokenRepo_RotationCannotResurrectRevokedFamily(t *testing.T) {
	pool, _ := newIsolatedUserTestPool(t, "read committed")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	user := testAdministrator(0)
	if err := NewPgUserRepo(pool).Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo := NewPgRefreshTokenRepo(pool)
	now := time.Now().UTC()
	initial := &domain.RefreshToken{ID: newTestUUID(), UserID: user.ID, TokenHash: "race-initial", TokenFamily: "race-family", ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	if _, err := repo.CreateSession(ctx, initial, 5); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		replacement := &domain.RefreshToken{ID: newTestUUID(), TokenHash: "race-replacement", ExpiresAt: now.Add(2 * time.Hour), CreatedAt: now.Add(time.Microsecond)}
		_, err := repo.Rotate(ctx, initial.TokenHash, replacement)
		if err != nil && !errors.Is(err, domain.ErrRefreshTokenReuse) {
			errs <- err
			return
		}
		errs <- nil
	}()
	go func() {
		defer wg.Done()
		<-start
		errs <- repo.RevokeFamily(ctx, initial.TokenFamily)
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent rotation/revocation: %v", err)
		}
	}
	active, err := repo.IsFamilyActive(ctx, initial.TokenFamily)
	if err != nil {
		t.Fatalf("IsFamilyActive: %v", err)
	}
	if active {
		t.Fatal("family became active after concurrent revocation and rotation")
	}
}

func TestPgRefreshTokenRepo_UserRevocationSerializesWithSessionCreation(t *testing.T) {
	for _, tc := range []struct {
		name              string
		revokeQueuedFirst bool
		newFamilyActive   bool
	}{
		{name: "revocation then creation keeps the later session", revokeQueuedFirst: true, newFamilyActive: true},
		{name: "creation then revocation ends both sessions", revokeQueuedFirst: false, newFamilyActive: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool, applicationName := newIsolatedUserTestPool(t, "read committed")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			user := testAdministrator(0)
			if err := NewPgUserRepo(pool).Create(ctx, user); err != nil {
				t.Fatalf("create user: %v", err)
			}
			repo := NewPgRefreshTokenRepo(pool)
			now := time.Now().UTC()
			oldToken := &domain.RefreshToken{ID: newTestUUID(), UserID: user.ID, TokenHash: "serialize-old", TokenFamily: "serialize-old-family", SessionRole: domain.RoleAdmin, ExpiresAt: now.Add(time.Hour), CreatedAt: now}
			if _, err := repo.CreateSession(ctx, oldToken, 5); err != nil {
				t.Fatalf("CreateSession old: %v", err)
			}
			newToken := &domain.RefreshToken{ID: newTestUUID(), UserID: user.ID, TokenHash: "serialize-new", TokenFamily: "serialize-new-family", SessionRole: domain.RoleAdmin, ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(time.Second)}

			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin blocker: %v", err)
			}
			defer blocker.Rollback(context.Background())
			if _, err := blocker.Exec(ctx, refreshTokenUserLockSQL, user.ID); err != nil {
				t.Fatalf("acquire blocker lock: %v", err)
			}

			type operationResult struct {
				name string
				err  error
			}
			results := make(chan operationResult, 2)
			startCreate := func() {
				go func() {
					_, err := repo.CreateSession(ctx, newToken, 5)
					results <- operationResult{name: "create", err: err}
				}()
			}
			startRevoke := func() {
				go func() {
					_, err := repo.RevokeUserSessions(ctx, user.ID)
					results <- operationResult{name: "revoke", err: err}
				}()
			}

			if tc.revokeQueuedFirst {
				startRevoke()
			} else {
				startCreate()
			}
			if err := waitForAdvisoryWaiters(ctx, pool, applicationName, 1); err != nil {
				t.Fatalf("wait for first operation: %v", err)
			}
			if tc.revokeQueuedFirst {
				startCreate()
			} else {
				startRevoke()
			}
			if err := waitForAdvisoryWaiters(ctx, pool, applicationName, 2); err != nil {
				t.Fatalf("wait for second operation: %v", err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatalf("release blocker lock: %v", err)
			}
			for range 2 {
				result := <-results
				if result.err != nil {
					t.Fatalf("%s operation: %v", result.name, result.err)
				}
			}

			oldActive, err := repo.IsFamilyActive(ctx, oldToken.TokenFamily)
			if err != nil {
				t.Fatalf("IsFamilyActive old: %v", err)
			}
			if oldActive {
				t.Fatal("old family remains active after user-wide revocation")
			}
			newActive, err := repo.IsFamilyActive(ctx, newToken.TokenFamily)
			if err != nil {
				t.Fatalf("IsFamilyActive new: %v", err)
			}
			if newActive != tc.newFamilyActive {
				t.Fatalf("new family active = %t, want %t", newActive, tc.newFamilyActive)
			}
		})
	}
}
