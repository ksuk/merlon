package auth

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryDenylist_RevokeAndCheck(t *testing.T) {
	ctx := context.Background()
	dl := NewInMemoryDenylist()

	if err := dl.Revoke(ctx, "user-1", time.Minute); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	revoked, err := dl.IsRevoked(ctx, "user-1")
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

	if err := dl.Revoke(ctx, "user-1", 20*time.Millisecond); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	revoked, err := dl.IsRevoked(ctx, "user-1")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if !revoked {
		t.Fatal("IsRevoked = false immediately after Revoke")
	}

	time.Sleep(40 * time.Millisecond)

	revoked, err = dl.IsRevoked(ctx, "user-1")
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

	revoked, err := dl.IsRevoked(ctx, "never-revoked-user")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if revoked {
		t.Fatal("IsRevoked = true for a user that was never revoked")
	}
}
