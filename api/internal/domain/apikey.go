package domain

import (
	"context"
	"time"
)

type Role string

const (
	RoleAdmin   Role = "admin"
	RoleAnalyst Role = "analyst"
	RoleViewer  Role = "viewer"
)

type APIKey struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	KeyHash   string    `json:"-"`
	Role      Role      `json:"role"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
}

type APIKeyRepository interface {
	GetByHash(ctx context.Context, keyHash string) (*APIKey, error)
	Create(ctx context.Context, key *APIKey) error
	List(ctx context.Context) ([]APIKey, error)
	Revoke(ctx context.Context, id string) error
}
