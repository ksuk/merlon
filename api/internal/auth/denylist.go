package auth

import (
	"context"
	"sync"
	"time"
)

// Denylist tracks revoked JWTs by user_id so a still-unexpired access token
// can be rejected immediately (forced logout, password change, role change).
// The standard (Redis-backed) configuration should implement this against
// Redis so revocation is visible across horizontally scaled API instances;
// NewInMemoryDenylist below only approximates this for a single process
// (minimal / no-Redis configuration).
type Denylist interface {
	Revoke(ctx context.Context, userID string, ttl time.Duration) error
	IsRevoked(ctx context.Context, userID string) (bool, error)
}

// InMemoryDenylist is a process-local Denylist for the minimal configuration
// (no Redis). Combined with the 15-minute access token TTL, this approximates
// immediate revocation without a shared cache. It does NOT work correctly
// across multiple horizontally-scaled API instances: a standard/Redis
// configuration must use a shared Denylist implementation instead.
type InMemoryDenylist struct {
	mu      sync.Mutex
	revoked map[string]time.Time // userID -> expiry
}

// NewInMemoryDenylist builds a process-local, TTL-based Denylist.
func NewInMemoryDenylist() *InMemoryDenylist {
	return &InMemoryDenylist{revoked: make(map[string]time.Time)}
}

func (d *InMemoryDenylist) Revoke(_ context.Context, userID string, ttl time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.revoked[userID] = time.Now().Add(ttl)
	return nil
}

func (d *InMemoryDenylist) IsRevoked(_ context.Context, userID string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	expiresAt, ok := d.revoked[userID]
	if !ok {
		return false, nil
	}
	if time.Now().After(expiresAt) {
		delete(d.revoked, userID)
		return false, nil
	}
	return true, nil
}
