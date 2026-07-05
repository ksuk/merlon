package batch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/store"
)

func testID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func createActiveEntry(t *testing.T, repo domain.WhitelistRepository, customerID string, validUntil time.Time) *domain.WhitelistEntry {
	t.Helper()
	now := time.Now()
	approvedBy := "apikey:approver"
	entry := &domain.WhitelistEntry{
		ID:          testID(),
		CustomerID:  customerID,
		Status:      domain.WhitelistEntryStatusActive,
		Reason:      "trusted customer",
		ValidFrom:   now,
		ValidUntil:  validUntil,
		RequestedBy: "apikey:requester",
		ApprovedBy:  &approvedBy,
		ApprovedAt:  &now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := repo.Create(context.Background(), entry); err != nil {
		t.Fatalf("create active entry: %v", err)
	}
	return entry
}

func TestRunWhitelistExpiryJob_ExpiresOverdueEntries(t *testing.T) {
	repo := store.NewMemoryWhitelistRepo()
	overdue := createActiveEntry(t, repo, "cust-overdue", time.Now().Add(-24*time.Hour))
	stillValid := createActiveEntry(t, repo, "cust-valid", time.Now().Add(90*24*time.Hour))

	result, err := RunWhitelistExpiryJob(context.Background(), repo, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Expired != 1 {
		t.Errorf("Expired = %d, want 1", result.Expired)
	}

	got, err := repo.Get(context.Background(), overdue.ID)
	if err != nil {
		t.Fatalf("get overdue entry: %v", err)
	}
	if got.Status != domain.WhitelistEntryStatusExpired {
		t.Errorf("overdue entry status = %q, want %q", got.Status, domain.WhitelistEntryStatusExpired)
	}

	stillGot, err := repo.Get(context.Background(), stillValid.ID)
	if err != nil {
		t.Fatalf("get still-valid entry: %v", err)
	}
	if stillGot.Status != domain.WhitelistEntryStatusActive {
		t.Errorf("still-valid entry status = %q, want %q", stillGot.Status, domain.WhitelistEntryStatusActive)
	}
}

func TestRunWhitelistExpiryJob_Idempotent(t *testing.T) {
	repo := store.NewMemoryWhitelistRepo()
	createActiveEntry(t, repo, "cust-overdue", time.Now().Add(-24*time.Hour))

	first, err := RunWhitelistExpiryJob(context.Background(), repo, nil)
	if err != nil {
		t.Fatalf("first run: unexpected error: %v", err)
	}
	if first.Expired != 1 {
		t.Fatalf("first run: Expired = %d, want 1", first.Expired)
	}

	second, err := RunWhitelistExpiryJob(context.Background(), repo, nil)
	if err != nil {
		t.Fatalf("second run: unexpected error: %v", err)
	}
	if second.Expired != 0 {
		t.Errorf("second run: Expired = %d, want 0", second.Expired)
	}
}

func TestRunWhitelistExpiryJob_NotifiesExpiringSoon(t *testing.T) {
	repo := store.NewMemoryWhitelistRepo()
	soon := createActiveEntry(t, repo, "cust-soon", time.Now().Add(15*24*time.Hour))
	createActiveEntry(t, repo, "cust-far", time.Now().Add(90*24*time.Hour))

	var notified []domain.WhitelistEntry
	notify := func(entries []domain.WhitelistEntry) { notified = entries }

	result, err := RunWhitelistExpiryJob(context.Background(), repo, notify)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExpiringSoon != 1 {
		t.Errorf("ExpiringSoon = %d, want 1", result.ExpiringSoon)
	}
	if len(notified) != 1 || notified[0].ID != soon.ID {
		t.Errorf("notified = %+v, want single entry %q", notified, soon.ID)
	}
}
