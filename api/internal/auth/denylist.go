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
// Refresh-token family state in RefreshTokenRepository is the authoritative
// cross-process revocation check. This denylist is an additional short-lived
// cache for individual access-token identifiers and session identifiers.
type Denylist interface {
	RevokeToken(ctx context.Context, tokenID string, ttl time.Duration) error
	RevokeSession(ctx context.Context, sessionID string, ttl time.Duration) error
	IsTokenRevoked(ctx context.Context, tokenID string) (bool, error)
	IsSessionRevoked(ctx context.Context, sessionID string) (bool, error)
}

// InMemoryDenylist is a process-local Denylist. Session authorization does not
// rely on it alone: authenticated requests also verify the persisted refresh
// family through RefreshTokenRepository, which remains shared across API
// replicas in PostgreSQL deployments.
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
	expiresAt := time.Now().Add(ttl)
	if current, ok := d.revoked[identifier]; !ok || expiresAt.After(current) {
		d.revoked[identifier] = expiresAt
	}
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
