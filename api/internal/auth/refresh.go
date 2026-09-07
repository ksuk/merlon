package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	return IssueRefreshTokenWithRoleAndEviction(ctx, repo, userID, "")
}

// IssueRefreshTokenWithRoleAndEviction records the authority snapshot that
// owns a new session. Refresh rejects a family when the user's current role no
// longer matches this snapshot, requiring an explicit re-login after an
// authority change.
func IssueRefreshTokenWithRoleAndEviction(ctx context.Context, repo domain.RefreshTokenRepository, userID string, sessionRole domain.Role) (rawToken, family, evictedFamily string, err error) {
	raw, tok, err := PrepareRefreshToken(userID, sessionRole)
	if err != nil {
		return "", "", "", err
	}
	evictedFamilies, err := repo.CreateSession(ctx, tok, MaxConcurrentSessions)
	if err != nil {
		return "", "", "", fmt.Errorf("create refresh token: %w", err)
	}
	if len(evictedFamilies) > 0 {
		evictedFamily = evictedFamilies[0]
	}

	return raw, tok.TokenFamily, evictedFamily, nil
}

// PrepareRefreshToken creates an unpersisted refresh token. Callers that must
// perform fallible work before changing the session set (for example access
// token signing) can complete that work and then commit the token through the
// repository's atomic CreateSessionWithAudit operation.
func PrepareRefreshToken(userID string, sessionRole domain.Role) (rawToken string, token *domain.RefreshToken, err error) {
	raw, err := randomHex(32)
	if err != nil {
		return "", nil, fmt.Errorf("generate refresh token: %w", err)
	}
	family, err := randomHex(16)
	if err != nil {
		return "", nil, fmt.Errorf("generate token family: %w", err)
	}
	id, err := randomHex(16)
	if err != nil {
		return "", nil, fmt.Errorf("generate token id: %w", err)
	}

	now := time.Now()
	tok := &domain.RefreshToken{
		ID:          id,
		UserID:      userID,
		TokenHash:   hashRefreshToken(raw),
		TokenFamily: family,
		SessionRole: sessionRole,
		ExpiresAt:   now.Add(RefreshTokenTTL),
		CreatedAt:   now,
	}
	return raw, tok, nil
}

// RotateRefreshToken consumes rawToken and issues a new token in the same
// token_family, revoking the consumed one. If rawToken was already revoked
// (reuse of a rotated-away token), the entire family is revoked and
// ErrTokenReuseDetected is returned.
func RotateRefreshToken(ctx context.Context, repo domain.RefreshTokenRepository, rawToken string) (newRawToken string, family string, err error) {
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
		ID:        id,
		TokenHash: hashRefreshToken(raw),
		ExpiresAt: now.Add(RefreshTokenTTL),
		CreatedAt: now,
	}
	tok, err := repo.Rotate(ctx, hashRefreshToken(rawToken), newTok)
	if errors.Is(err, domain.ErrRefreshTokenReuse) {
		if tok == nil {
			return "", "", fmt.Errorf("rotate refresh token: %w", err)
		}
		return "", tok.TokenFamily, ErrTokenReuseDetected
	}
	if errors.Is(err, domain.ErrRefreshTokenExpired) {
		return "", "", errors.New("refresh token expired")
	}
	if err != nil {
		return "", "", fmt.Errorf("rotate refresh token: %w", err)
	}

	return raw, tok.TokenFamily, nil
}
