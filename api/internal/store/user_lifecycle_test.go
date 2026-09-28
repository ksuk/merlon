package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func lifecycleUser(id, email string, role domain.Role, active bool, now time.Time) *domain.User {
	return &domain.User{ID: id, Email: email, PasswordHash: "hash", Role: role, Active: active, CreatedAt: now, UpdatedAt: now}
}

func lifecycleAudit(actor, target, action string, now time.Time) *domain.AuditEntry {
	return &domain.AuditEntry{UserID: actor, Action: action, ResourceType: "users", ResourceID: target, CreatedAt: now}
}

func TestMemoryUserLifecycleCreatesUniqueUserWithAudit(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	users := NewMemoryUserRepo()
	tokens := NewMemoryRefreshTokenRepo()
	audit := NewMemoryAuditRepo()
	lifecycle := NewMemoryUserLifecycleRepo(users, tokens, audit)

	user := lifecycleUser("analyst-1", "analyst@example.com", domain.RoleAnalyst, true, now)
	if err := lifecycle.CreateUser(ctx, user, lifecycleAudit("admin-1", user.ID, "user_created", now)); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := users.Get(ctx, user.ID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	entries, err := audit.List(ctx, domain.AuditListFilter{})
	if err != nil || len(entries) != 1 || entries[0].Action != "user_created" {
		t.Fatalf("audit entries = %#v, err = %v", entries, err)
	}

	duplicate := lifecycleUser("analyst-2", user.Email, domain.RoleViewer, true, now)
	err = lifecycle.CreateUser(ctx, duplicate, lifecycleAudit("admin-1", duplicate.ID, "user_created", now))
	var conflict *domain.ErrConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("duplicate error = %v, want conflict", err)
	}
}

func TestMemoryUserLifecycleAuthorityChangeRevokesSessionsAtomically(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	users := NewMemoryUserRepo()
	audit := NewMemoryAuditRepo()
	tokens := NewMemoryRefreshTokenRepoWithAudit(audit)
	lifecycle := NewMemoryUserLifecycleRepo(users, tokens, audit)
	admin := lifecycleUser("admin-1", "admin@example.com", domain.RoleAdmin, true, now)
	target := lifecycleUser("analyst-1", "analyst@example.com", domain.RoleAnalyst, true, now)
	_ = users.Create(ctx, admin)
	_ = users.Create(ctx, target)
	_, _ = tokens.CreateSession(ctx, &domain.RefreshToken{ID: "rt-1", UserID: target.ID, TokenHash: "token", TokenFamily: "family-1", SessionRole: target.Role, ExpiresAt: now.Add(time.Hour), CreatedAt: now}, 5)

	updated, families, err := lifecycle.UpdateAuthority(ctx, target.ID, domain.RoleViewer, true, now.Add(time.Minute), lifecycleAudit(admin.ID, target.ID, "user_authority_updated", now))
	if err != nil {
		t.Fatalf("UpdateAuthority: %v", err)
	}
	if updated.Role != domain.RoleViewer || len(families) != 1 || families[0] != "family-1" {
		t.Fatalf("updated = %#v, families = %#v", updated, families)
	}
	active, _ := tokens.IsFamilyActive(ctx, "family-1")
	if active {
		t.Fatal("old session remains active")
	}
}

func TestMemoryUserLifecycleAuditFailureRollsBackAuthorityAndSessions(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	users := NewMemoryUserRepo()
	audit := NewMemoryAuditRepo()
	tokens := NewMemoryRefreshTokenRepoWithAudit(audit)
	lifecycle := NewMemoryUserLifecycleRepo(users, tokens, audit)
	target := lifecycleUser("admin-1", "admin@example.com", domain.RoleAdmin, true, now)
	otherAdmin := lifecycleUser("admin-2", "admin2@example.com", domain.RoleAdmin, true, now)
	_ = users.Create(ctx, target)
	_ = users.Create(ctx, otherAdmin)
	_, _ = tokens.CreateSession(ctx, &domain.RefreshToken{ID: "rt-1", UserID: target.ID, TokenHash: "token", TokenFamily: "family-1", SessionRole: target.Role, ExpiresAt: now.Add(time.Hour), CreatedAt: now}, 5)
	audit.SetCreateFailure(errors.New("audit unavailable"))

	_, _, err := lifecycle.UpdateAuthority(ctx, target.ID, domain.RoleViewer, true, now.Add(time.Minute), lifecycleAudit(otherAdmin.ID, target.ID, "user_authority_updated", now))
	if err == nil {
		t.Fatal("UpdateAuthority succeeded despite audit failure")
	}
	stored, _ := users.Get(ctx, target.ID)
	if stored.Role != domain.RoleAdmin {
		t.Fatalf("role = %s, want admin", stored.Role)
	}
	active, _ := tokens.IsFamilyActive(ctx, "family-1")
	if !active {
		t.Fatal("session revoked despite rollback")
	}
}

func TestMemoryUserLifecycleProtectsLastActiveAdministrator(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	users := NewMemoryUserRepo()
	audit := NewMemoryAuditRepo()
	lifecycle := NewMemoryUserLifecycleRepo(users, NewMemoryRefreshTokenRepoWithAudit(audit), audit)
	admin := lifecycleUser("admin-1", "admin@example.com", domain.RoleAdmin, true, now)
	_ = users.Create(ctx, admin)

	_, _, err := lifecycle.UpdateAuthority(ctx, admin.ID, domain.RoleViewer, true, now.Add(time.Minute), lifecycleAudit(admin.ID, admin.ID, "user_authority_updated", now))
	var conflict *domain.ErrConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("demote error = %v, want conflict", err)
	}
	_, _, err = lifecycle.UpdateAuthority(ctx, admin.ID, domain.RoleAdmin, false, now.Add(time.Minute), lifecycleAudit(admin.ID, admin.ID, "user_authority_updated", now))
	if !errors.As(err, &conflict) {
		t.Fatalf("disable error = %v, want conflict", err)
	}
}

func TestMemoryUserLifecyclePasswordResetRevokesSessionsWithoutAuditingSecret(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	users := NewMemoryUserRepo()
	audit := NewMemoryAuditRepo()
	tokens := NewMemoryRefreshTokenRepoWithAudit(audit)
	lifecycle := NewMemoryUserLifecycleRepo(users, tokens, audit)
	user := lifecycleUser("viewer-1", "viewer@example.com", domain.RoleViewer, true, now)
	_ = users.Create(ctx, user)
	_, _ = tokens.CreateSession(ctx, &domain.RefreshToken{ID: "rt-1", UserID: user.ID, TokenHash: "token", TokenFamily: "family-1", SessionRole: user.Role, ExpiresAt: now.Add(time.Hour), CreatedAt: now}, 5)

	updated, families, err := lifecycle.ResetPassword(ctx, user.ID, "replacement-hash", now.Add(time.Minute), lifecycleAudit("admin-1", user.ID, "user_password_reset", now))
	if err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if updated.PasswordHash != "replacement-hash" || len(families) != 1 {
		t.Fatalf("updated = %#v, families = %#v", updated, families)
	}
	entries, _ := audit.List(ctx, domain.AuditListFilter{})
	if len(entries) != 1 || len(entries[0].Details) != 0 {
		t.Fatalf("audit entries = %#v", entries)
	}
}

func TestPgUserLifecyclePersistsAuthorityAndRevocationWithAudit(t *testing.T) {
	pool, _ := newIsolatedUserTestPool(t, "read committed")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	users := NewPgUserRepo(pool)
	tokens := NewPgRefreshTokenRepo(pool)
	lifecycle := NewPgUserLifecycleRepo(pool)
	admin := lifecycleUser(newTestUUID(), "admin@example.com", domain.RoleAdmin, true, now)
	target := lifecycleUser(newTestUUID(), "analyst@example.com", domain.RoleAnalyst, true, now)
	if err := users.Create(ctx, admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := lifecycle.CreateUser(ctx, target, lifecycleAudit(admin.ID, target.ID, "user_created", now)); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token := &domain.RefreshToken{ID: newTestUUID(), UserID: target.ID, TokenHash: "pg-token", TokenFamily: "pg-family", SessionRole: target.Role, ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	if _, err := tokens.CreateSession(ctx, token, 5); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	updated, families, err := lifecycle.UpdateAuthority(ctx, target.ID, domain.RoleViewer, false, now.Add(time.Minute), lifecycleAudit(admin.ID, target.ID, "user_authority_updated", now.Add(time.Minute)))
	if err != nil {
		t.Fatalf("UpdateAuthority: %v", err)
	}
	if updated.Role != domain.RoleViewer || updated.Active || len(families) != 1 || families[0] != token.TokenFamily {
		t.Fatalf("updated = %#v, families = %#v", updated, families)
	}
	active, err := tokens.IsFamilyActive(ctx, token.TokenFamily)
	if err != nil || active {
		t.Fatalf("active = %v, err = %v", active, err)
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE resource_type = 'users' AND resource_id = $1`, target.ID).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("audit count = %d, err = %v", auditCount, err)
	}
}

func TestPgUserLifecycleAuditFailureRollsBackPasswordAndRevocation(t *testing.T) {
	pool, _ := newIsolatedUserTestPool(t, "read committed")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	users := NewPgUserRepo(pool)
	tokens := NewPgRefreshTokenRepo(pool)
	lifecycle := NewPgUserLifecycleRepo(pool)
	user := lifecycleUser(newTestUUID(), "viewer@example.com", domain.RoleViewer, true, now)
	if err := users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := &domain.RefreshToken{ID: newTestUUID(), UserID: user.ID, TokenHash: "rollback-token", TokenFamily: "rollback-family", SessionRole: user.Role, ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	if _, err := tokens.CreateSession(ctx, token, 5); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	entry := lifecycleAudit("admin-1", user.ID, "user_password_reset", now.Add(time.Minute))
	entry.IPAddress = "not-an-ip"
	if _, _, err := lifecycle.ResetPassword(ctx, user.ID, "replacement", now.Add(time.Minute), entry); err == nil {
		t.Fatal("ResetPassword succeeded despite audit failure")
	}
	stored, err := users.Get(ctx, user.ID)
	if err != nil || stored.PasswordHash != "hash" {
		t.Fatalf("stored = %#v, err = %v", stored, err)
	}
	active, err := tokens.IsFamilyActive(ctx, token.TokenFamily)
	if err != nil || !active {
		t.Fatalf("active = %v, err = %v", active, err)
	}
}

func TestMemorySessionCreationRejectsStaleUserAuthoritySnapshot(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	users := NewMemoryUserRepo()
	user := lifecycleUser("user-1", "user@example.com", domain.RoleViewer, true, now)
	_ = users.Create(ctx, user)
	tokens := NewMemoryRefreshTokenRepoWithAuditAndUsers(NewMemoryAuditRepo(), users)
	user.UpdatedAt = now.Add(time.Minute)
	_ = users.Update(ctx, user)
	token := &domain.RefreshToken{ID: "rt-1", UserID: user.ID, TokenHash: "stale", TokenFamily: "family", SessionRole: user.Role, AuthorityUpdatedAt: now, ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(time.Minute)}
	if _, err := tokens.CreateSession(ctx, token, 5); !errors.Is(err, domain.ErrSessionAuthorityChanged) {
		t.Fatalf("CreateSession error = %v, want authority changed", err)
	}
}

func TestPgSessionCreationRejectsStaleUserAuthoritySnapshot(t *testing.T) {
	pool, _ := newIsolatedUserTestPool(t, "read committed")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Microsecond)
	user := lifecycleUser(newTestUUID(), "user@example.com", domain.RoleViewer, true, now)
	users := NewPgUserRepo(pool)
	if err := users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	user.UpdatedAt = now.Add(time.Minute)
	if err := users.Update(ctx, user); err != nil {
		t.Fatalf("update user: %v", err)
	}
	token := &domain.RefreshToken{ID: newTestUUID(), UserID: user.ID, TokenHash: "stale", TokenFamily: "family", SessionRole: user.Role, AuthorityUpdatedAt: now, ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(time.Minute)}
	if _, err := NewPgRefreshTokenRepo(pool).CreateSession(ctx, token, 5); !errors.Is(err, domain.ErrSessionAuthorityChanged) {
		t.Fatalf("CreateSession error = %v, want authority changed", err)
	}
}
