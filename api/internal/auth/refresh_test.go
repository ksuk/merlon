package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/store"
)

func TestRotateRefreshToken_Success(t *testing.T) {
	ctx := context.Background()
	repo := store.NewMemoryRefreshTokenRepo()

	rawToken, family, err := IssueRefreshToken(ctx, repo, "user-1")
	if err != nil {
		t.Fatalf("IssueRefreshToken: %v", err)
	}

	oldTok, err := repo.GetByHash(ctx, hashRefreshToken(rawToken))
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}

	newRawToken, newFamily, err := RotateRefreshToken(ctx, repo, rawToken)
	if err != nil {
		t.Fatalf("RotateRefreshToken: %v", err)
	}
	if newFamily != family {
		t.Fatalf("family changed across rotation: got %s, want %s", newFamily, family)
	}
	if newRawToken == rawToken {
		t.Fatal("RotateRefreshToken returned the same raw token")
	}

	oldAfter, err := repo.GetByHash(ctx, hashRefreshToken(rawToken))
	if err != nil {
		t.Fatalf("GetByHash(old): %v", err)
	}
	if oldAfter.RevokedAt == nil {
		t.Fatal("old refresh token was not revoked after rotation")
	}
	_ = oldTok

	newTok, err := repo.GetByHash(ctx, hashRefreshToken(newRawToken))
	if err != nil {
		t.Fatalf("GetByHash(new): %v", err)
	}
	if newTok.RevokedAt != nil {
		t.Fatal("new refresh token is revoked immediately")
	}
	if newTok.TokenFamily != family {
		t.Fatalf("new token family = %s, want %s", newTok.TokenFamily, family)
	}
}

func TestRotateRefreshToken_ReuseDetected_RevokesFamily(t *testing.T) {
	ctx := context.Background()
	repo := store.NewMemoryRefreshTokenRepo()

	rawToken, family, err := IssueRefreshToken(ctx, repo, "user-1")
	if err != nil {
		t.Fatalf("IssueRefreshToken: %v", err)
	}

	newRawToken, _, err := RotateRefreshToken(ctx, repo, rawToken)
	if err != nil {
		t.Fatalf("RotateRefreshToken: %v", err)
	}

	// Reuse the already-rotated (revoked) old token.
	_, _, err = RotateRefreshToken(ctx, repo, rawToken)
	if !errors.Is(err, ErrTokenReuseDetected) {
		t.Fatalf("RotateRefreshToken(reused) error = %v, want ErrTokenReuseDetected", err)
	}

	active, err := repo.ListActiveByUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListActiveByUser: %v", err)
	}
	for _, tok := range active {
		if tok.TokenFamily == family {
			t.Fatalf("token %s in reused family is still active after reuse detection", tok.ID)
		}
	}

	newTok, err := repo.GetByHash(ctx, hashRefreshToken(newRawToken))
	if err != nil {
		t.Fatalf("GetByHash(new): %v", err)
	}
	if newTok.RevokedAt == nil {
		t.Fatal("new token in the reused family was not revoked")
	}
}

func TestRotateRefreshToken_Expired(t *testing.T) {
	ctx := context.Background()
	repo := store.NewMemoryRefreshTokenRepo()

	rawToken := "expired-raw-token"
	err := repo.Create(ctx, &domain.RefreshToken{
		ID:          "tok-1",
		UserID:      "user-1",
		TokenHash:   hashRefreshToken(rawToken),
		TokenFamily: "family-1",
		ExpiresAt:   time.Now().Add(-time.Hour),
		CreatedAt:   time.Now().Add(-8 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, _, err := RotateRefreshToken(ctx, repo, rawToken); err == nil {
		t.Fatal("RotateRefreshToken succeeded for an expired token")
	}
}

func TestRevokeRefreshTokenFamily(t *testing.T) {
	ctx := context.Background()
	repo := store.NewMemoryRefreshTokenRepo()

	rawToken, family, err := IssueRefreshToken(ctx, repo, "user-1")
	if err != nil {
		t.Fatalf("IssueRefreshToken: %v", err)
	}
	rotatedRaw, _, err := RotateRefreshToken(ctx, repo, rawToken)
	if err != nil {
		t.Fatalf("RotateRefreshToken: %v", err)
	}

	if err := RevokeRefreshTokenFamily(ctx, repo, rotatedRaw); err != nil {
		t.Fatalf("RevokeRefreshTokenFamily: %v", err)
	}

	active, err := repo.ListActiveByUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListActiveByUser: %v", err)
	}
	for _, tok := range active {
		if tok.TokenFamily == family {
			t.Fatalf("token %s in family %s is still active after RevokeRefreshTokenFamily", tok.ID, family)
		}
	}
}

func TestConcurrentSessionLimit(t *testing.T) {
	ctx := context.Background()
	repo := store.NewMemoryRefreshTokenRepo()

	var rawTokens []string
	for i := 0; i < MaxConcurrentSessions; i++ {
		raw, _, err := IssueRefreshToken(ctx, repo, "user-1")
		if err != nil {
			t.Fatalf("IssueRefreshToken #%d: %v", i, err)
		}
		rawTokens = append(rawTokens, raw)
		time.Sleep(time.Millisecond)
	}

	count, err := repo.CountActiveByUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("CountActiveByUser: %v", err)
	}
	if count != MaxConcurrentSessions {
		t.Fatalf("active sessions = %d, want %d", count, MaxConcurrentSessions)
	}

	// One more session beyond the limit must evict the oldest.
	if _, _, err := IssueRefreshToken(ctx, repo, "user-1"); err != nil {
		t.Fatalf("IssueRefreshToken (6th): %v", err)
	}

	count, err = repo.CountActiveByUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("CountActiveByUser: %v", err)
	}
	if count != MaxConcurrentSessions {
		t.Fatalf("active sessions after eviction = %d, want %d", count, MaxConcurrentSessions)
	}

	oldestTok, err := repo.GetByHash(ctx, hashRefreshToken(rawTokens[0]))
	if err != nil {
		t.Fatalf("GetByHash(oldest): %v", err)
	}
	if oldestTok.RevokedAt == nil {
		t.Fatal("oldest session was not revoked after exceeding MaxConcurrentSessions")
	}
}
