package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

// #78 requires a transaction's related alerts and cases to be listed with
// status/severity/priority and a direct drill-down. alerts.transaction_ids has
// always existed, but neither GET /alerts nor GET /cases could be filtered by
// it, so the only way to answer "what did this transaction trigger?" was to
// fetch by customer and filter client-side -- which silently loses records the
// moment the customer has more alerts than one page.

type relatedFixture struct {
	server  *Server
	alerts  *store.MemoryAlertRepo
	cases   *store.MemoryCaseRepo
	txnID   string
	otherID string
}

func newRelatedFixture(t *testing.T) *relatedFixture {
	t.Helper()
	ctx := context.Background()
	alerts := store.NewMemoryAlertRepo()
	cases := store.NewMemoryCaseRepo()
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       alerts,
		Cases:        cases,
		Audit:        store.NewMemoryAuditRepo(),
	})

	customerID := "00000000000000000000000000000d01"
	txnID := "00000000000000000000000000000d10"
	otherID := "00000000000000000000000000000d11"
	now := time.Now().UTC()

	// Two alerts on the same customer: one raised by our transaction, one not.
	matching := &domain.Alert{
		ID: "00000000000000000000000000000d20", CustomerID: customerID,
		ScenarioID: "structuring", Severity: domain.AlertSeverityHigh,
		Status: domain.AlertStatusOpen, TransactionIDs: []string{txnID},
		Description: "matched", DetectedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	unrelated := &domain.Alert{
		ID: "00000000000000000000000000000d21", CustomerID: customerID,
		ScenarioID: "rapid_movement", Severity: domain.AlertSeverityCritical,
		Status: domain.AlertStatusOpen, TransactionIDs: []string{otherID},
		Description: "unrelated", DetectedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	for _, a := range []*domain.Alert{matching, unrelated} {
		if err := alerts.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	// A case linked to the matching alert, and one linked only to the other.
	linked := &domain.Case{
		ID: "00000000000000000000000000000d30", CustomerID: customerID,
		Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityHigh,
		AlertIDs: []string{matching.ID}, Summary: "linked case",
		CreatedAt: now, UpdatedAt: now,
	}
	unlinked := &domain.Case{
		ID: "00000000000000000000000000000d31", CustomerID: customerID,
		Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityLow,
		AlertIDs: []string{unrelated.ID}, Summary: "unlinked case",
		CreatedAt: now, UpdatedAt: now,
	}
	for _, c := range []*domain.Case{linked, unlinked} {
		if err := cases.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	return &relatedFixture{server: s, alerts: alerts, cases: cases, txnID: txnID, otherID: otherID}
}

func getIDs(t *testing.T, s *Server, path string) []string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200, body: %s", path, rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode %s: %v (body %s)", path, err, rec.Body.String())
	}
	ids := make([]string, 0, len(envelope.Data))
	for _, row := range envelope.Data {
		ids = append(ids, row.ID)
	}
	return ids
}

func TestAlertListFiltersByTransactionID(t *testing.T) {
	f := newRelatedFixture(t)

	ids := getIDs(t, f.server, "/api/v1/alerts?transaction_id="+f.txnID)
	if len(ids) != 1 || ids[0] != "00000000000000000000000000000d20" {
		t.Fatalf("alerts for transaction = %v, want only the alert carrying that transaction id", ids)
	}
}

func TestCaseListFiltersByTransactionID(t *testing.T) {
	f := newRelatedFixture(t)

	ids := getIDs(t, f.server, "/api/v1/cases?transaction_id="+f.txnID)
	if len(ids) != 1 || ids[0] != "00000000000000000000000000000d30" {
		t.Fatalf("cases for transaction = %v, want only the case linked to that transaction's alert", ids)
	}
}

func TestTransactionFilterReturnsEmptyForUnknownTransaction(t *testing.T) {
	f := newRelatedFixture(t)

	// An unknown id must return an empty page, never the unfiltered queue:
	// silently ignoring the filter would show every alert as "related".
	if ids := getIDs(t, f.server, "/api/v1/alerts?transaction_id=00000000000000000000000000000dff"); len(ids) != 0 {
		t.Fatalf("alerts for an unknown transaction = %v, want none", ids)
	}
	if ids := getIDs(t, f.server, "/api/v1/cases?transaction_id=00000000000000000000000000000dff"); len(ids) != 0 {
		t.Fatalf("cases for an unknown transaction = %v, want none", ids)
	}
}
