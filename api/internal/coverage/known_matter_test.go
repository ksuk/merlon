package coverage

import (
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestBuildKnownMatterUnionPrefersCaseAndDeduplicatesLinkedEvidence(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	alerts := []domain.Alert{
		{ID: "alert-case", CustomerID: "cust-1", ScenarioID: "scenario-a", Status: domain.AlertStatusClosedTruePositive, TransactionIDs: []string{"tx-1"}, DetectedAt: now},
		{ID: "alert-standalone", CustomerID: "cust-2", ScenarioID: "scenario-b", Status: domain.AlertStatusClosedTruePositive, TransactionIDs: []string{"tx-2"}, DetectedAt: now},
	}
	cases := []domain.Case{{ID: "case-1", CustomerID: "cust-1", AlertIDs: []string{"alert-case"}, Status: domain.CaseStatusEscalated}}
	reports := []domain.STRReport{
		{ID: "report-linked", AlertID: "alert-case", CaseID: "case-1", CustomerID: "cust-1", Status: domain.ReportStatusSubmitted},
		{ID: "report-independent", AlertID: "alert-independent", CustomerID: "cust-3", Status: domain.ReportStatusSubmitted, TransactionIDs: []string{"tx-3"}, AlertSnapshot: domain.STRAlertSnapshot{CustomerID: "cust-3", ScenarioID: "scenario-c", DetectedAt: now}},
	}
	items := BuildKnownMatterUnion(alerts, cases, reports)
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, want 3: %#v", len(items), items)
	}
	for _, item := range items {
		if item.ID == "str:report-linked" || item.ID == "alert:alert-case" {
			t.Fatalf("linked evidence was not deduplicated: %#v", items)
		}
	}
	if items[0].ID != "alert:alert-standalone" {
		t.Fatalf("items are not deterministically sorted: %#v", items)
	}
}
