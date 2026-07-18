package domain

import (
	"context"
	"time"
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
	ExpiresAt   time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
}

type UserRepository interface {
	Get(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Create(ctx context.Context, u *User) error
	Update(ctx context.Context, u *User) error
	// Count is used to determine whether initial setup (the operational design §4.5) has
	// already been completed.
	Count(ctx context.Context) (int, error)
	// List returns all users, for the admin user-management screen.
	List(ctx context.Context) ([]User, error)
}

type RefreshTokenRepository interface {
	Create(ctx context.Context, t *RefreshToken) error
	GetByHash(ctx context.Context, tokenHash string) (*RefreshToken, error)
	RevokeFamily(ctx context.Context, tokenFamily string) error
	Revoke(ctx context.Context, id string) error
	CountActiveByUser(ctx context.Context, userID string) (int, error)
	// ListActiveByUser returns the user's non-revoked, unexpired sessions,
	// used to evict the oldest session when MaxConcurrentSessions is exceeded.
	ListActiveByUser(ctx context.Context, userID string) ([]RefreshToken, error)
}
