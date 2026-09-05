package auth

import (
	"context"
	"sync"
	"time"
)

// Denylist tracks revoked access-token and session identifiers so a
// still-unexpired JWT can be rejected immediately. Token revocation ends one
// access token; session revocation ends every access token issued for one
// refresh-token family without preventing a later independent login.
// The standard (Redis-backed) configuration should implement this against
// Redis so revocation is visible across horizontally scaled API instances;
// NewInMemoryDenylist below only approximates this for a single process
// (minimal / no-Redis configuration).
type Denylist interface {
	RevokeToken(ctx context.Context, tokenID string, ttl time.Duration) error
	RevokeSession(ctx context.Context, sessionID string, ttl time.Duration) error
	IsTokenRevoked(ctx context.Context, tokenID string) (bool, error)
	IsSessionRevoked(ctx context.Context, sessionID string) (bool, error)
}

// InMemoryDenylist is a process-local Denylist for the minimal configuration
// (no Redis). Combined with the 15-minute access token TTL, this approximates
// immediate revocation without a shared cache. It does NOT work correctly
// across multiple horizontally-scaled API instances: a standard/Redis
// configuration must use a shared Denylist implementation instead.
type InMemoryDenylist struct {
	mu      sync.Mutex
	revoked map[string]time.Time // namespaced identifier -> expiry
}

// NewInMemoryDenylist builds a process-local, TTL-based Denylist.
func NewInMemoryDenylist() *InMemoryDenylist {
	return &InMemoryDenylist{revoked: make(map[string]time.Time)}
}

func (d *InMemoryDenylist) RevokeToken(_ context.Context, tokenID string, ttl time.Duration) error {
	return d.revoke("token:"+tokenID, ttl)
}

func (d *InMemoryDenylist) RevokeSession(_ context.Context, sessionID string, ttl time.Duration) error {
	return d.revoke("session:"+sessionID, ttl)
}

func (d *InMemoryDenylist) revoke(identifier string, ttl time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.revoked[identifier] = time.Now().Add(ttl)
	return nil
}

func (d *InMemoryDenylist) IsTokenRevoked(_ context.Context, tokenID string) (bool, error) {
	return d.isRevoked("token:" + tokenID)
}

func (d *InMemoryDenylist) IsSessionRevoked(_ context.Context, sessionID string) (bool, error) {
	return d.isRevoked("session:" + sessionID)
}

func (d *InMemoryDenylist) isRevoked(identifier string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	expiresAt, ok := d.revoked[identifier]
	if !ok {
		return false, nil
	}
	if time.Now().After(expiresAt) {
		delete(d.revoked, identifier)
		return false, nil
	}
	return true, nil
}
