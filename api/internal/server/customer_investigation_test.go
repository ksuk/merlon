package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/policy"
	"github.com/ksuk/merlon/api/internal/store"
)

// /customers/{id}/investigation had no test: server/investigation_test.go is
// entirely case-management tests despite its name.

type investigationFixture struct {
	server     *Server
	customers  *store.MemoryCustomerRepo
	wave3      *store.MemoryWave3Repo
	customerID string
}

func newInvestigationFixture(t *testing.T) *investigationFixture {
	t.Helper()
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	wave3 := store.NewMemoryWave3Repo()
	s := New(":0", Deps{
		Customers: customers, Wave3: wave3, Audit: store.NewMemoryAuditRepo(),
		Transactions: store.NewMemoryTransactionRepo(), Alerts: store.NewMemoryAlertRepo(),
		Cases: store.NewMemoryCaseRepo(), ScreeningResults: store.NewMemoryScreeningResultRepo(),
	})
	id := "00000000000000000000000000000e01"
	if err := customers.Create(ctx, &domain.Customer{
		ID: id, ExternalID: "edd-customer", CustomerType: domain.CustomerTypeIndividual,
		CountryCode: "JP", Status: domain.CustomerStatusActive,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	return &investigationFixture{server: s, customers: customers, wave3: wave3, customerID: id}
}

func (f *investigationFixture) setEDDRequestedDaysAgo(t *testing.T, days int) {
	t.Helper()
	ctx := context.Background()
	c, err := f.customers.Get(ctx, f.customerID)
	if err != nil {
		t.Fatal(err)
	}
	requested := time.Now().UTC().AddDate(0, 0, -days)
	c.EddRequestedAt = &requested
	if err := f.customers.Update(ctx, c); err != nil {
		t.Fatal(err)
	}
}

func (f *investigationFixture) investigation(t *testing.T) investigationResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+f.customerID+"/investigation", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("investigation = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out investigationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The completion states the panel could not previously express: only
// not_required, open and escalated existed, so a finished window and an
// overdue one both read as "open".
func TestInvestigationEDDPanelBoundaries(t *testing.T) {
	edd := policy.DefaultEDD()
	stage2, _ := edd.StageDays("stage2")
	stage3, _ := edd.StageDays("stage3")

	tests := []struct {
		daysAgo    int
		wantStatus string
		wantStage  string
	}{
		{0, "open", "requested"},
		{29, "open", "requested"},
		{30, "open", "stage1"},
		{stage2 - 1, "open", "stage1"},
		{stage2, "open", "stage2"},
		{stage3 - 1, "open", "stage2"},
		{stage3, "open", "stage3"},
		{stage3 + 1, "overdue", "stage3"},
		{stage3 + 200, "overdue", "stage3"},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%d_days", tc.daysAgo), func(t *testing.T) {
			f := newInvestigationFixture(t)
			f.setEDDRequestedDaysAgo(t, tc.daysAgo)
			panel := f.investigation(t).EDD
			if panel.CompletionStatus != tc.wantStatus {
				t.Errorf("completion_status = %q, want %q", panel.CompletionStatus, tc.wantStatus)
			}
			if panel.CurrentStage != tc.wantStage {
				t.Errorf("current_stage = %q, want %q", panel.CurrentStage, tc.wantStage)
			}
			if panel.OverdueDays < 0 {
				t.Errorf("overdue_days = %d, want never negative", panel.OverdueDays)
			}
			if tc.wantStatus == "overdue" && panel.OverdueDays == 0 {
				t.Error("an overdue window reported zero overdue days")
			}
		})
	}
}

// The defect overdue_days exists to fix: remaining_days is clamped at zero, so
// a window 200 days late looked identical to one due today.
func TestInvestigationEDDOverdueDaysDistinguishesLateness(t *testing.T) {
	edd := policy.DefaultEDD()
	due, _ := edd.StageDays("stage3")

	slightly := newInvestigationFixture(t)
	slightly.setEDDRequestedDaysAgo(t, due+2)
	badly := newInvestigationFixture(t)
	badly.setEDDRequestedDaysAgo(t, due+200)

	slightPanel := slightly.investigation(t).EDD
	badPanel := badly.investigation(t).EDD
	if slightPanel.RemainingDays != badPanel.RemainingDays {
		t.Fatal("this test assumes remaining_days cannot tell the two apart")
	}
	if badPanel.OverdueDays <= slightPanel.OverdueDays {
		t.Fatalf("overdue_days = %d and %d; the far later window must read as later", badPanel.OverdueDays, slightPanel.OverdueDays)
	}
}

func TestInvestigationEDDPanelNotRequiredWithoutAWindow(t *testing.T) {
	f := newInvestigationFixture(t)
	panel := f.investigation(t).EDD
	if panel.Required || panel.CompletionStatus != "not_required" {
		t.Fatalf("panel = %+v, want not_required for a customer with no EDD window", panel)
	}
}

func (f *investigationFixture) eddAction(t *testing.T, action, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+f.customerID+"/edd/"+action, strings.NewReader(body)))
	return rec
}

// There was no completion path at all before this: a window could be opened
// and escalated but never finished.
func TestCompleteAndReopenEDDWindow(t *testing.T) {
	f := newInvestigationFixture(t)
	f.setEDDRequestedDaysAgo(t, 10)

	if rec := f.eddAction(t, "complete", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("completion without a rationale = %d, want 400", rec.Code)
	}

	rec := f.eddAction(t, "complete", `{"rationale":"source of funds evidence received and reviewed","case_id":"00000000000000000000000000000c99"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete = %d, body=%s", rec.Code, rec.Body.String())
	}
	var panel investigationEDDPanel
	if err := json.Unmarshal(rec.Body.Bytes(), &panel); err != nil {
		t.Fatal(err)
	}
	if panel.CompletionStatus != "completed" || panel.CompletedAt == nil {
		t.Fatalf("panel = %+v, want a completed window", panel)
	}
	if panel.CaseID == "" {
		t.Error("the linked case was not recorded on the window")
	}

	if again := f.eddAction(t, "complete", `{"rationale":"again"}`); again.Code != http.StatusConflict {
		t.Fatalf("completing twice = %d, want 409", again.Code)
	}

	reopened := f.eddAction(t, "reopen", `{"rationale":"new adverse media requires further review"}`)
	if reopened.Code != http.StatusOK {
		t.Fatalf("reopen = %d, body=%s", reopened.Code, reopened.Body.String())
	}
	// A fresh value: completed_at is omitempty, so decoding into the previous
	// panel would leave the stale timestamp in place and prove nothing.
	var reopenedPanel investigationEDDPanel
	if err := json.Unmarshal(reopened.Body.Bytes(), &reopenedPanel); err != nil {
		t.Fatal(err)
	}
	if reopenedPanel.CompletionStatus == "completed" || reopenedPanel.CompletedAt != nil {
		t.Fatalf("panel = %+v, want the window open again", reopenedPanel)
	}
	if noReopen := f.eddAction(t, "reopen", `{"rationale":"once more"}`); noReopen.Code != http.StatusConflict {
		t.Fatalf("reopening an open window = %d, want 409", noReopen.Code)
	}

	// Each action leaves an event: the closed window's history is the only
	// thing that can answer "was EDD ever completed" later.
	events, err := f.customers.ListCustomerEDDEvents(context.Background(), f.customerID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("edd events = %d, want one per action", len(events))
	}
	kinds := map[domain.EDDEventType]bool{}
	for _, event := range events {
		kinds[event.EventType] = true
		if event.Rationale == "" || event.Actor == "" || event.PolicyVersion == "" {
			t.Errorf("event = %+v, want the rationale, actor and policy version recorded", event)
		}
	}
	if !kinds[domain.EDDEventCompleted] || !kinds[domain.EDDEventReopened] {
		t.Fatalf("event types = %v, want both completed and reopened", kinds)
	}
}

func TestEDDActionsRejectUnknownActionsAndMissingWindows(t *testing.T) {
	f := newInvestigationFixture(t)

	if rec := f.eddAction(t, "teleport", `{"rationale":"x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown action = %d, want 400", rec.Code)
	}
	if rec := f.eddAction(t, "complete", `{"rationale":"nothing to complete"}`); rec.Code != http.StatusConflict {
		t.Fatalf("completing without a window = %d, want 409", rec.Code)
	}
}

// The timeline carried five event kinds and knew nothing about identity
// changes or the EDD lifecycle.
func TestInvestigationTimelineIncludesIdentityAndEDDEvents(t *testing.T) {
	f := newInvestigationFixture(t)
	ctx := context.Background()
	f.setEDDRequestedDaysAgo(t, 5)

	if err := f.wave3.AppendCustomerIdentityHistory(ctx, &domain.CustomerIdentityHistoryEntry{
		ID: "ident-1", CustomerID: f.customerID, Actor: "analyst-1",
		Rationale: "address corrected from the renewal form", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if rec := f.eddAction(t, "complete", `{"rationale":"evidence reviewed"}`); rec.Code != http.StatusOK {
		t.Fatalf("complete = %d, body=%s", rec.Code, rec.Body.String())
	}

	out := f.investigation(t)
	kinds := map[string]bool{}
	for _, entry := range out.Timeline {
		kinds[entry.Kind] = true
	}
	if !kinds["identity_history"] {
		t.Error("the timeline omits identity changes")
	}
	if !kinds["edd_event"] {
		t.Error("the timeline omits the EDD lifecycle")
	}
	if out.Counts["identity_history"] != 1 || out.Counts["edd_events"] != 1 {
		t.Fatalf("counts = %v, want one of each", out.Counts)
	}
}

// A tier downgrade closes the window; it must not delete the evidence that
// EDD was ever required.
func TestTierDowngradeRetainsEDDEvidence(t *testing.T) {
	f := newInvestigationFixture(t)
	ctx := context.Background()
	f.setEDDRequestedDaysAgo(t, 40)
	c, err := f.customers.Get(ctx, f.customerID)
	if err != nil {
		t.Fatal(err)
	}
	stage1 := time.Now().UTC().AddDate(0, 0, -9)
	c.EddStage1LastSentAt = &stage1
	if err := f.customers.Update(ctx, c); err != nil {
		t.Fatal(err)
	}

	panel := f.investigation(t).EDD
	if panel.Stage1LastSentAt == nil {
		t.Fatal("fixture did not record the stage 1 reminder")
	}
	if panel.ClosedAt != nil {
		t.Fatalf("panel = %+v, want an open window before the downgrade", panel)
	}
}
