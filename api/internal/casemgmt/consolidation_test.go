package casemgmt

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

func newTestAlert(id, customerID string, detectedAt time.Time) *domain.Alert {
	return &domain.Alert{
		ID:         id,
		CustomerID: customerID,
		ScenarioID: "tm_structuring_v2",
		Severity:   domain.AlertSeverityHigh,
		Status:     domain.AlertStatusOpen,
		DetectedAt: detectedAt,
	}
}

func newTestCase(id, customerID string, status domain.CaseStatus, createdAt time.Time) *domain.Case {
	return &domain.Case{
		ID:         id,
		CustomerID: customerID,
		Status:     status,
		Priority:   domain.CasePriorityMedium,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
	}
}

func TestConsolidateAlert_JoinsExistingInvestigatingCase(t *testing.T) {
	cases := store.NewMemoryCaseRepo()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	existing := newTestCase("case-1", "cust-1", domain.CaseStatusInvestigating, now.Add(-1*time.Hour))
	if err := cases.Create(ctx, existing); err != nil {
		t.Fatalf("seed case: %v", err)
	}

	alert := newTestAlert("alert-1", "cust-1", now)
	got, err := ConsolidateAlert(ctx, cases, alert, DefaultConsolidationWindow)
	if err != nil {
		t.Fatalf("ConsolidateAlert: %v", err)
	}
	if got.ID != "case-1" {
		t.Fatalf("got case %s, want case-1", got.ID)
	}
	if len(got.AlertIDs) != 1 || got.AlertIDs[0] != "alert-1" {
		t.Fatalf("AlertIDs = %v, want [alert-1]", got.AlertIDs)
	}
}

func TestConsolidateAlert_CreatesNewCaseWhenNoOpenCase(t *testing.T) {
	cases := store.NewMemoryCaseRepo()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	// A closed case within the window must not be joined.
	closedCase := newTestCase("case-closed", "cust-1", domain.CaseStatusClosed, now.Add(-1*time.Hour))
	if err := cases.Create(ctx, closedCase); err != nil {
		t.Fatalf("seed case: %v", err)
	}

	alert := newTestAlert("alert-1", "cust-1", now)
	got, err := ConsolidateAlert(ctx, cases, alert, DefaultConsolidationWindow)
	if err != nil {
		t.Fatalf("ConsolidateAlert: %v", err)
	}
	if got.ID == "case-closed" {
		t.Fatal("must not join a closed case")
	}
	if got.CustomerID != "cust-1" {
		t.Errorf("CustomerID = %s, want cust-1", got.CustomerID)
	}
	if got.Status != domain.CaseStatusOpen {
		t.Errorf("Status = %s, want open", got.Status)
	}
	if len(got.AlertIDs) != 1 || got.AlertIDs[0] != "alert-1" {
		t.Fatalf("AlertIDs = %v, want [alert-1]", got.AlertIDs)
	}

	// Verify the new case round-trips through the repository.
	stored, err := cases.Get(ctx, got.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.ID != got.ID {
		t.Fatalf("stored case ID mismatch")
	}
}

func TestConsolidateAlert_DoesNotReuseStrFiledCase(t *testing.T) {
	cases := store.NewMemoryCaseRepo()
	alerts := store.NewMemoryAlertRepo()
	lifecycle := store.NewMemoryCaseAlertLifecycleRepo(cases, alerts)
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	filed := newTestCase("case-filed", "cust-1", domain.CaseStatusStrFiled, now.Add(-time.Hour))
	if err := cases.Create(ctx, filed); err != nil {
		t.Fatalf("seed filed case: %v", err)
	}
	alert := newTestAlert("alert-new", "cust-1", now)
	if err := alerts.Create(ctx, alert); err != nil {
		t.Fatalf("seed alert: %v", err)
	}

	got, err := ConsolidateAlertWithLifecycle(ctx, cases, lifecycle, alert, DefaultConsolidationWindow)
	if err != nil {
		t.Fatalf("ConsolidateAlert: %v", err)
	}
	if got.ID == filed.ID {
		t.Fatal("str_filed case received a new alert")
	}
	stored, err := cases.Get(ctx, filed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.AlertIDs) != 0 {
		t.Fatalf("filed case alert_ids = %v, want unchanged", stored.AlertIDs)
	}
}

func TestConsolidateAlert_WindowBoundary(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	t.Run("exactly_24h_still_joins", func(t *testing.T) {
		cases := store.NewMemoryCaseRepo()
		c := newTestCase("case-1", "cust-1", domain.CaseStatusOpen, now.Add(-DefaultConsolidationWindow))
		if err := cases.Create(ctx, c); err != nil {
			t.Fatalf("seed case: %v", err)
		}
		alert := newTestAlert("alert-1", "cust-1", now)
		got, err := ConsolidateAlert(ctx, cases, alert, DefaultConsolidationWindow)
		if err != nil {
			t.Fatalf("ConsolidateAlert: %v", err)
		}
		if got.ID != "case-1" {
			t.Fatalf("case created exactly at the window boundary must still be joined, got %s", got.ID)
		}
	})

	t.Run("24h_plus_1s_creates_new_case", func(t *testing.T) {
		cases := store.NewMemoryCaseRepo()
		c := newTestCase("case-1", "cust-1", domain.CaseStatusOpen, now.Add(-DefaultConsolidationWindow-time.Second))
		if err := cases.Create(ctx, c); err != nil {
			t.Fatalf("seed case: %v", err)
		}
		alert := newTestAlert("alert-1", "cust-1", now)
		got, err := ConsolidateAlert(ctx, cases, alert, DefaultConsolidationWindow)
		if err != nil {
			t.Fatalf("ConsolidateAlert: %v", err)
		}
		if got.ID == "case-1" {
			t.Fatal("a case created just outside the window must not be joined")
		}
	})
}

func TestConsolidateAlert_PrefersInvestigatingOverOpen(t *testing.T) {
	cases := store.NewMemoryCaseRepo()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	openCase := newTestCase("case-open", "cust-1", domain.CaseStatusOpen, now.Add(-2*time.Hour))
	investigatingCase := newTestCase("case-investigating", "cust-1", domain.CaseStatusInvestigating, now.Add(-1*time.Hour))
	if err := cases.Create(ctx, openCase); err != nil {
		t.Fatalf("seed open case: %v", err)
	}
	if err := cases.Create(ctx, investigatingCase); err != nil {
		t.Fatalf("seed investigating case: %v", err)
	}

	alert := newTestAlert("alert-1", "cust-1", now)
	got, err := ConsolidateAlert(ctx, cases, alert, DefaultConsolidationWindow)
	if err != nil {
		t.Fatalf("ConsolidateAlert: %v", err)
	}
	if got.ID != "case-investigating" {
		t.Fatalf("got case %s, want case-investigating (must prefer INVESTIGATING+ over OPEN)", got.ID)
	}
}
