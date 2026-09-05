package auth

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryDenylist_RevokeAndCheck(t *testing.T) {
	ctx := context.Background()
	dl := NewInMemoryDenylist()

	if err := dl.RevokeToken(ctx, "token-1", time.Minute); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}

	revoked, err := dl.IsTokenRevoked(ctx, "token-1")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if !revoked {
		t.Fatal("IsRevoked = false after Revoke")
	}
}

func TestInMemoryDenylist_TTLExpiry(t *testing.T) {
	ctx := context.Background()
	dl := NewInMemoryDenylist()

	if err := dl.RevokeSession(ctx, "session-1", 20*time.Millisecond); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	revoked, err := dl.IsSessionRevoked(ctx, "session-1")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if !revoked {
		t.Fatal("IsRevoked = false immediately after Revoke")
	}

	time.Sleep(40 * time.Millisecond)

	revoked, err = dl.IsSessionRevoked(ctx, "session-1")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if revoked {
		t.Fatal("IsRevoked = true after TTL expiry")
	}
}

func TestInMemoryDenylist_NotRevokedByDefault(t *testing.T) {
	ctx := context.Background()
	dl := NewInMemoryDenylist()

	revoked, err := dl.IsTokenRevoked(ctx, "never-revoked-token")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if revoked {
		t.Fatal("IsRevoked = true for a user that was never revoked")
	}
}

func TestInMemoryDenylist_TokenAndSessionRevocationAreIndependent(t *testing.T) {
	ctx := context.Background()
	dl := NewInMemoryDenylist()

	if err := dl.RevokeToken(ctx, "token-1", time.Minute); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if err := dl.RevokeSession(ctx, "session-1", time.Minute); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	for _, tc := range []struct {
		name string
		got  func() (bool, error)
		want bool
	}{
		{"revoked token", func() (bool, error) { return dl.IsTokenRevoked(ctx, "token-1") }, true},
		{"other token", func() (bool, error) { return dl.IsTokenRevoked(ctx, "session-1") }, false},
		{"revoked session", func() (bool, error) { return dl.IsSessionRevoked(ctx, "session-1") }, true},
		{"other session", func() (bool, error) { return dl.IsSessionRevoked(ctx, "token-1") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.got()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("revoked = %t, want %t", got, tc.want)
			}
		})
	}
}
