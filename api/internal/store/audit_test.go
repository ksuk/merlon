package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// TestPostgresAuditRepoListInetAddresses verifies the audit repository's
// INET scan path against the real PostgreSQL codec. The old *string target
// returned a scan error (and consequently a 500 from GET /audit) for every
// non-NULL INET value.
func TestPostgresAuditRepoListInetAddresses(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgAuditRepo(pool)
	ctx := context.Background()
	userID := "audit-inet-" + newTestUUID()
	base := time.Now().UTC().Add(-time.Minute)

	entries := []*domain.AuditEntry{
		{UserID: userID, Action: "inet_ipv4", ResourceType: "audit", ResourceID: "ipv4", IPAddress: "192.0.2.10", CreatedAt: base},
		{UserID: userID, Action: "inet_ipv6", ResourceType: "audit", ResourceID: "ipv6", IPAddress: "2001:db8::10", CreatedAt: base.Add(time.Second)},
		{UserID: userID, Action: "inet_null", ResourceType: "audit", ResourceID: "null", CreatedAt: base.Add(2 * time.Second)},
	}
	for _, entry := range entries {
		if err := repo.Create(ctx, entry); err != nil {
			t.Fatalf("Create(%s): %v", entry.Action, err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM audit_logs WHERE user_id = $1`, userID)
	})

	page, err := repo.List(ctx, domain.AuditListFilter{UserID: userID, Limit: 2})
	if err != nil {
		t.Fatalf("List first page: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("first page length = %d, want 2", len(page))
	}
	if page[0].IPAddress != "" || page[1].IPAddress != "2001:db8::10" {
		t.Fatalf("first page IPs = [%q, %q], want [null, 2001:db8::10]", page[0].IPAddress, page[1].IPAddress)
	}

	last := page[len(page)-1]
	rest, err := repo.List(ctx, domain.AuditListFilter{
		UserID: userID,
		Cursor: &domain.Cursor{CreatedAt: last.CreatedAt, ID: fmt.Sprintf("%d", last.ID)},
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("List cursor page: %v", err)
	}
	if len(rest) != 1 || rest[0].IPAddress != "192.0.2.10" {
		t.Fatalf("cursor page = %+v, want the IPv4 entry", rest)
	}
}
