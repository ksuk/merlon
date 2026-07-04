package server

import (
	"context"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/store"
)

func testServerWithWhitelistOnly() *Server {
	return New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(),
		Whitelist: store.NewMemoryWhitelistRepo(),
	})
}

func newTestAlert(customerID, scenarioID string) *domain.Alert {
	now := time.Now()
	return &domain.Alert{
		ID:         generateID(),
		CustomerID: customerID,
		ScenarioID: scenarioID,
		Severity:   domain.AlertSeverityMedium,
		Status:     domain.AlertStatusOpen,
		DetectedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func createActiveWhitelistEntry(t *testing.T, s *Server, customerID string, excludedRuleIDs []string, validUntil time.Time) *domain.WhitelistEntry {
	t.Helper()
	now := time.Now()
	approvedBy := "apikey:approver"
	entry := &domain.WhitelistEntry{
		ID:              generateID(),
		CustomerID:      customerID,
		Status:          domain.WhitelistEntryStatusActive,
		Reason:          "trusted customer",
		ExcludedRuleIDs: excludedRuleIDs,
		ValidFrom:       now,
		ValidUntil:      validUntil,
		RequestedBy:     "apikey:requester",
		ApprovedBy:      &approvedBy,
		ApprovedAt:      &now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.whitelist.Create(context.Background(), entry); err != nil {
		t.Fatalf("create active whitelist entry: %v", err)
	}
	return entry
}

func TestApplyWhitelistSuppression_NoActiveEntry_PassesThrough(t *testing.T) {
	s := testServerWithWhitelistOnly()
	alert := newTestAlert("cust-no-entry", "rule-a")

	out, err := s.applyWhitelistSuppression(context.Background(), alert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Suppressed {
		t.Error("Suppressed = true, want false")
	}
	if out.Status != domain.AlertStatusOpen {
		t.Errorf("Status = %q, want %q", out.Status, domain.AlertStatusOpen)
	}
	if out.SuppressionReason != "" {
		t.Errorf("SuppressionReason = %q, want empty", out.SuppressionReason)
	}
}

func TestApplyWhitelistSuppression_FullExclusion_Suppresses(t *testing.T) {
	s := testServerWithWhitelistOnly()
	validUntil := time.Now().Add(90 * 24 * time.Hour)
	entry := createActiveWhitelistEntry(t, s, "cust-full", nil, validUntil)
	alert := newTestAlert("cust-full", "rule-a")

	out, err := s.applyWhitelistSuppression(context.Background(), alert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Suppressed {
		t.Error("Suppressed = false, want true")
	}
	if out.Status != domain.AlertStatusSuppressed {
		t.Errorf("Status = %q, want %q", out.Status, domain.AlertStatusSuppressed)
	}
	wantReason := "whitelist:" + entry.ID
	if out.SuppressionReason != wantReason {
		t.Errorf("SuppressionReason = %q, want %q", out.SuppressionReason, wantReason)
	}
}

func TestApplyWhitelistSuppression_PartialExclusion_OnlyExcludedRulesSuppressed(t *testing.T) {
	s := testServerWithWhitelistOnly()
	validUntil := time.Now().Add(90 * 24 * time.Hour)
	createActiveWhitelistEntry(t, s, "cust-partial", []string{"rule-a"}, validUntil)

	excluded := newTestAlert("cust-partial", "rule-a")
	out, err := s.applyWhitelistSuppression(context.Background(), excluded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Suppressed {
		t.Error("excluded rule alert: Suppressed = false, want true")
	}

	other := newTestAlert("cust-partial", "rule-b")
	out2, err := s.applyWhitelistSuppression(context.Background(), other)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out2.Suppressed {
		t.Error("non-excluded rule alert: Suppressed = true, want false")
	}
	if out2.Status != domain.AlertStatusOpen {
		t.Errorf("Status = %q, want %q", out2.Status, domain.AlertStatusOpen)
	}
}

func TestApplyWhitelistSuppression_ExpiredEntry_NoSuppression(t *testing.T) {
	s := testServerWithWhitelistOnly()
	// GetActiveByCustomer only considers status, so this reproduces the
	// "expired but daily job hasn't run yet" case: status is still active
	// while valid_until is already in the past.
	validUntil := time.Now().Add(-24 * time.Hour)
	createActiveWhitelistEntry(t, s, "cust-expired", nil, validUntil)
	alert := newTestAlert("cust-expired", "rule-a")

	out, err := s.applyWhitelistSuppression(context.Background(), alert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Suppressed {
		t.Error("Suppressed = true, want false")
	}
	if out.Status != domain.AlertStatusOpen {
		t.Errorf("Status = %q, want %q", out.Status, domain.AlertStatusOpen)
	}
}
