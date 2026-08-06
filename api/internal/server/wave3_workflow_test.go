package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

func TestScreeningReviewRequiredAuditFailureRollsBackWave3Workflow(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	alerts := store.NewMemoryAlertRepo()
	cases := store.NewMemoryCaseRepo()
	audit := store.NewMemoryAuditRepo()
	outbox := store.NewMemoryEventOutboxRepo()
	wave3 := store.NewMemoryWave3Repo()
	s := New(":0", Deps{
		Customers: customers, Transactions: transactions, Alerts: alerts, Cases: cases,
		Reports: store.NewMemorySTRReportRepo(), Audit: audit, EventOutbox: outbox,
		Wave3: wave3, Screening: &engine.MockScreeningEngine{}, Scoring: &engine.MockScoringEngine{},
		Monitoring: &engine.MockMonitoringEngine{},
	})
	ctx := context.Background()
	customerID := "00000000000000000000000000000041"
	if err := customers.Create(ctx, &domain.Customer{ID: customerID, ExternalID: "wave3-audit-customer", CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", Status: domain.CustomerStatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	resultID := "00000000000000000000000000000042"
	if err := wave3.PersistScreeningRun(ctx, &domain.ScreeningRun{ID: "00000000000000000000000000000043", CustomerID: customerID, Status: domain.ScreeningRunCompleted}, []domain.ScreeningResultRecord{{ID: resultID, CustomerID: customerID, ListID: "mof", ListType: "sanctions", EntryID: "entry", MatchedName: "Example", Status: domain.ScreeningResultStatusReviewing}}); err != nil {
		t.Fatal(err)
	}
	audit.SetCreateFailure(errors.New("audit unavailable"))
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/screening/results/"+resultID, strings.NewReader(`{"status":"TRUE_POSITIVE","rationale":"confirmed","expected_version":1}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want failure on required audit append", rec.Code)
	}
	stored, err := wave3.GetScreeningResult(ctx, resultID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.ScreeningResultStatusReviewing || stored.Version != 1 || stored.CaseID != "" {
		t.Fatalf("screening result after rollback = %+v, want unchanged reviewing version 1", stored)
	}
	history, err := wave3.ListScreeningResultHistory(ctx, resultID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("history after rollback = %d, want 0", len(history))
	}
	if got, err := cases.ListByCustomer(ctx, customerID); err != nil {
		t.Fatal(err)
	} else if len(got) != 0 {
		t.Fatalf("cases after rollback = %d, want 0", len(got))
	}
	if got, err := audit.List(ctx, domain.AuditListFilter{}); err != nil {
		t.Fatal(err)
	} else if len(got) != 0 {
		t.Fatalf("audit after rollback = %d, want 0", len(got))
	}
	if got, err := outbox.ListPending(ctx, 10); err != nil {
		t.Fatal(err)
	} else if len(got) != 0 {
		t.Fatalf("outbox after rollback = %d, want 0", len(got))
	}

	audit.SetCreateFailure(nil)
	retry := httptest.NewRequest(http.MethodPatch, "/api/v1/screening/results/"+resultID, strings.NewReader(`{"status":"TRUE_POSITIVE","rationale":"confirmed","expected_version":1}`))
	retryRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(retryRec, retry)
	if retryRec.Code != http.StatusOK {
		t.Fatalf("retry status = %d, body=%s", retryRec.Code, retryRec.Body.String())
	}
	var outcome domain.ScreeningReviewOutcome
	if err := json.NewDecoder(retryRec.Body).Decode(&outcome); err != nil {
		t.Fatal(err)
	}
	if outcome.CaseID == "" || !outcome.CaseCreated {
		t.Fatalf("retry outcome = %+v, want one created case", outcome)
	}
	if got, err := outbox.ListPending(ctx, 10); err != nil {
		t.Fatal(err)
	} else if len(got) != 1 {
		t.Fatalf("outbox after retry = %d, want 1", len(got))
	}
}

func TestTargetConfirmationRequiresRationale(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	wave3 := store.NewMemoryWave3Repo()
	audit := store.NewMemoryAuditRepo()
	s := New(":0", Deps{Customers: customers, Audit: audit, Wave3: wave3})
	ctx := context.Background()
	customerID := "00000000000000000000000000000051"
	if err := customers.Create(ctx, &domain.Customer{ID: customerID, ExternalID: "target-rationale", CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", Status: domain.CustomerStatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	manifest := &domain.TargetManifest{ID: "00000000000000000000000000000052", Operation: "batch_score", TargetMode: domain.TargetModeSelected, CustomerIDs: []string{customerID}, Token: "target-token", Status: "preview", Version: 1, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err := wave3.CreateTargetManifest(ctx, manifest); err != nil {
		t.Fatal(err)
	}

	withoutRationale := httptest.NewRequest(http.MethodPost, "/api/v1/batch/targets/"+manifest.ID+"/confirm", strings.NewReader(`{"token":"target-token","expected_version":1}`))
	withoutResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(withoutResponse, withoutRationale)
	if withoutResponse.Code != http.StatusBadRequest {
		t.Fatalf("confirmation without rationale status = %d, want 400", withoutResponse.Code)
	}

	withRationale := httptest.NewRequest(http.MethodPost, "/api/v1/batch/targets/"+manifest.ID+"/confirm", strings.NewReader(`{"token":"target-token","rationale":"reviewed target population","expected_version":1}`))
	withResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(withResponse, withRationale)
	if withResponse.Code != http.StatusOK {
		t.Fatalf("confirmation with rationale status = %d, body=%s", withResponse.Code, withResponse.Body.String())
	}
}
