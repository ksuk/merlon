package domain

import (
	"context"
	"errors"
	"time"
)

var (
	ErrRefreshTokenReuse       = errors.New("refresh token reuse detected")
	ErrRefreshTokenExpired     = errors.New("refresh token expired")
	ErrSessionAuthorityChanged = errors.New("session authority changed before creation")
)

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type RefreshToken struct {
	ID          string
	UserID      string
	TokenHash   string
	TokenFamily string
	SessionRole Role
	// AuthorityUpdatedAt is a creation-time compare value. It is not persisted:
	// session_role remains the durable authority snapshot after insertion.
	AuthorityUpdatedAt time.Time
	ExpiresAt          time.Time
	RevokedAt          *time.Time
	CreatedAt          time.Time
}

type UserRepository interface {
	Get(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Create(ctx context.Context, u *User) error
	// CreateIfEmpty atomically creates u only when no user exists. It is the
	// final serialization point for initial-administrator setup.
	CreateIfEmpty(ctx context.Context, u *User) (bool, error)
	Update(ctx context.Context, u *User) error
	// Count is used to determine whether initial setup (the operational design §4.5) has
	// already been completed.
	Count(ctx context.Context) (int, error)
	// List returns all users, for the admin user-management screen.
	List(ctx context.Context) ([]User, error)
}

// UserLifecycleRepository applies administrator-managed account mutations,
// session revocation, and the corresponding audit append as one transaction.
// Returned family IDs are used only to invalidate process-local caches; the
// durable refresh-token rows are already revoked when the method succeeds.
type UserLifecycleRepository interface {
	CreateUser(ctx context.Context, user *User, audit *AuditEntry) error
	UpdateAuthority(ctx context.Context, id string, role Role, active bool, updatedAt time.Time, audit *AuditEntry) (*User, []string, error)
	ResetPassword(ctx context.Context, id, passwordHash string, updatedAt time.Time, audit *AuditEntry) (*User, []string, error)
}

type RefreshTokenRepository interface {
	// CreateSession atomically enforces maxActiveFamilies before inserting t.
	// It returns every family evicted by the insertion, oldest first.
	CreateSession(ctx context.Context, t *RefreshToken, maxActiveFamilies int) ([]string, error)
	// CreateSessionWithAudit commits the session-limit mutation and audit entry
	// as one unit. A failed audit must leave both the new and existing sessions
	// unchanged.
	CreateSessionWithAudit(ctx context.Context, t *RefreshToken, maxActiveFamilies int, audit *AuditEntry) ([]string, error)
	GetByHash(ctx context.Context, tokenHash string) (*RefreshToken, error)
	// Rotate atomically consumes tokenHash and creates replacement in the same
	// family. Reuse revokes the entire family and returns
	// ErrRefreshTokenReuse; expiration returns ErrRefreshTokenExpired.
	Rotate(ctx context.Context, tokenHash string, replacement *RefreshToken) (*RefreshToken, error)
	RevokeFamily(ctx context.Context, tokenFamily string) error
	// RevokeUserSessions atomically revokes every family active at its
	// serialization point. A concurrent later CreateSession remains usable.
	RevokeUserSessions(ctx context.Context, userID string) ([]string, error)
	// RevokeUserSessionsWithAudit commits the user-wide revocation and its audit
	// entry as one unit.
	RevokeUserSessionsWithAudit(ctx context.Context, userID string, audit *AuditEntry) ([]string, error)
	// IsFamilyActive is the persistent authorization check for access tokens.
	IsFamilyActive(ctx context.Context, tokenFamily string) (bool, error)
	CountActiveByUser(ctx context.Context, userID string) (int, error)
	// ListActiveByUser returns the user's non-revoked, unexpired sessions,
	// used to evict the oldest session when MaxConcurrentSessions is exceeded.
	ListActiveByUser(ctx context.Context, userID string) ([]RefreshToken, error)
}
