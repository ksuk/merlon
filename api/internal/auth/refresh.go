package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// MaxConcurrentSessions is the default cap on simultaneous active sessions
// (refresh token families) per user (the authentication model §2).
const MaxConcurrentSessions = 5

// ErrTokenReuseDetected is returned when a refresh token that has already
// been rotated (or otherwise revoked) is presented again. The caller must
// treat this as a compromise signal: the whole token_family has already been
// revoked by RotateRefreshToken by the time this error is returned.
var ErrTokenReuseDetected = errors.New("refresh token reuse detected")

func hashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// HashRefreshToken exposes the raw-token hashing scheme so callers (e.g. the
// session API) can look up a RefreshToken row by its raw cookie value.
func HashRefreshToken(raw string) string {
	return hashRefreshToken(raw)
}

// RevokeRefreshTokenFamily revokes every token in rawToken's session
// (token_family), used by logout to end the whole session rather than just
// its current tip.
func RevokeRefreshTokenFamily(ctx context.Context, repo domain.RefreshTokenRepository, rawToken string) error {
	tok, err := repo.GetByHash(ctx, hashRefreshToken(rawToken))
	if err != nil {
		return fmt.Errorf("lookup refresh token: %w", err)
	}
	return repo.RevokeFamily(ctx, tok.TokenFamily)
}

func randomHex(numBytes int) (string, error) {
	b := make([]byte, numBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// IssueRefreshToken starts a brand-new session (token family) for userID. If
// the user already has MaxConcurrentSessions active sessions, the oldest one
// is evicted first (the authentication model §2 "同時セッション制限").
func IssueRefreshToken(ctx context.Context, repo domain.RefreshTokenRepository, userID string) (rawToken, family string, err error) {
	rawToken, family, _, err = IssueRefreshTokenWithEviction(ctx, repo, userID)
	return rawToken, family, err
}

// IssueRefreshTokenWithEviction starts a session and reports the family that
// was evicted to enforce MaxConcurrentSessions. The session API uses that
// family identifier to deny its still-unexpired access tokens as well as its
// persisted refresh tokens.
func IssueRefreshTokenWithEviction(ctx context.Context, repo domain.RefreshTokenRepository, userID string) (rawToken, family, evictedFamily string, err error) {
	active, err := repo.ListActiveByUser(ctx, userID)
	if err != nil {
		return "", "", "", fmt.Errorf("list active sessions: %w", err)
	}
	if len(active) >= MaxConcurrentSessions {
		sort.Slice(active, func(i, j int) bool { return active[i].CreatedAt.Before(active[j].CreatedAt) })
		evictedFamily = active[0].TokenFamily
		if err := repo.RevokeFamily(ctx, evictedFamily); err != nil {
			return "", "", "", fmt.Errorf("evict oldest session: %w", err)
		}
	}

	raw, err := randomHex(32)
	if err != nil {
		return "", "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	family, err = randomHex(16)
	if err != nil {
		return "", "", "", fmt.Errorf("generate token family: %w", err)
	}
	id, err := randomHex(16)
	if err != nil {
		return "", "", "", fmt.Errorf("generate token id: %w", err)
	}

	now := time.Now()
	tok := &domain.RefreshToken{
		ID:          id,
		UserID:      userID,
		TokenHash:   hashRefreshToken(raw),
		TokenFamily: family,
		ExpiresAt:   now.Add(RefreshTokenTTL),
		CreatedAt:   now,
	}
	if err := repo.Create(ctx, tok); err != nil {
		return "", "", "", fmt.Errorf("create refresh token: %w", err)
	}

	return raw, family, evictedFamily, nil
}

// RotateRefreshToken consumes rawToken and issues a new token in the same
// token_family, revoking the consumed one. If rawToken was already revoked
// (reuse of a rotated-away token), the entire family is revoked and
// ErrTokenReuseDetected is returned.
func RotateRefreshToken(ctx context.Context, repo domain.RefreshTokenRepository, rawToken string) (newRawToken string, family string, err error) {
	tok, err := repo.GetByHash(ctx, hashRefreshToken(rawToken))
	if err != nil {
		return "", "", fmt.Errorf("lookup refresh token: %w", err)
	}

	if tok.RevokedAt != nil {
		if err := repo.RevokeFamily(ctx, tok.TokenFamily); err != nil {
			return "", "", fmt.Errorf("revoke family after reuse: %w", err)
		}
		return "", tok.TokenFamily, ErrTokenReuseDetected
	}

	if time.Now().After(tok.ExpiresAt) {
		return "", "", errors.New("refresh token expired")
	}

	if err := repo.Revoke(ctx, tok.ID); err != nil {
		return "", "", fmt.Errorf("revoke consumed token: %w", err)
	}

	raw, err := randomHex(32)
	if err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	id, err := randomHex(16)
	if err != nil {
		return "", "", fmt.Errorf("generate token id: %w", err)
	}

	now := time.Now()
	newTok := &domain.RefreshToken{
		ID:          id,
		UserID:      tok.UserID,
		TokenHash:   hashRefreshToken(raw),
		TokenFamily: tok.TokenFamily,
		ExpiresAt:   now.Add(RefreshTokenTTL),
		CreatedAt:   now,
	}
	if err := repo.Create(ctx, newTok); err != nil {
		return "", "", fmt.Errorf("create rotated token: %w", err)
	}

	return raw, tok.TokenFamily, nil
}
