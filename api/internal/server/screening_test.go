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
	"github.com/ksuk/merlon/api/internal/screening"
	"github.com/ksuk/merlon/api/internal/store"
)

func testServerWithScreeningResults() *Server {
	return New(":0", Deps{
		Customers:        store.NewMemoryCustomerRepo(),
		Alerts:           store.NewMemoryAlertRepo(),
		Scoring:          &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Monitoring:       &engine.MockMonitoringEngine{},
		Screening:        &engine.MockScreeningEngine{},
		Cases:            store.NewMemoryCaseRepo(),
		Webhooks:         store.NewMemoryWebhookRepo(),
		ScreeningResults: store.NewMemoryScreeningResultRepo(),
	})
}

// seedScreeningResult creates a customer and a screening_results row in
// status via the repo directly (bypassing the engine's single-shot screen
// call, which this WS's persistence layer does not yet write through).
func seedScreeningResult(t *testing.T, s *Server, status domain.ScreeningResultStatus) *domain.ScreeningResultRecord {
	t.Helper()

	body := `{"external_id":"SCR-` + generateID() + `","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created customer: %v", err)
	}

	now := time.Now()
	record := &domain.ScreeningResultRecord{
		ID:          generateID(),
		CustomerID:  created.ID,
		ListID:      "mof_japan",
		ListType:    "sanctions",
		EntryID:     "MOF-001",
		MatchedName: "Kim Jong Un",
		Similarity:  0.97,
		Status:      domain.ScreeningResultStatusNew,
		ScreenedAt:  now,
		CreatedAt:   now,
	}
	if err := s.screeningResults.Create(context.Background(), record); err != nil {
		t.Fatalf("seed screening result: %v", err)
	}
	if status != domain.ScreeningResultStatusNew {
		if err := record.ApplyStatusTransition(status, "moving to reviewing for test setup"); err != nil {
			t.Fatalf("ApplyStatusTransition to %q: %v", status, err)
		}
		if err := s.screeningResults.Update(context.Background(), record); err != nil {
			t.Fatalf("update seeded screening result: %v", err)
		}
	}
	return record
}

func TestHandleScreeningCheck_ExplicitRequestScreensCustomer(t *testing.T) {
	mockScreening := &engine.MockScreeningEngine{
		Result: &domain.ScreenResult{
			Hit:          true,
			Matches:      []domain.ScreenMatch{{ListID: "mof_japan", EntryID: "MOF-001", MatchedName: "Kim Jong Un", Similarity: 0.97, ListType: "sanctions", Source: "test"}},
			ListsChecked: 1,
			ScreenedAt:   time.Now(),
		},
	}
	s := testServerWithEngines(
		&engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		&engine.MockMonitoringEngine{},
		mockScreening,
	)

	body := `{"external_id":"CHK001","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created customer: %v", err)
	}

	checkBody := `{"customer_id":"` + created.ID + `"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/screening/check", strings.NewReader(checkBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var result screening.BatchResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Trigger != screening.TriggerAPIRequest {
		t.Errorf("Trigger = %q, want %q", result.Trigger, screening.TriggerAPIRequest)
	}
	if len(result.Outcomes) != 1 || !result.Outcomes[0].Screened || result.Outcomes[0].CustomerID != created.ID {
		t.Errorf("outcomes = %+v, want single screened outcome for %q", result.Outcomes, created.ID)
	}
}

func TestHandleScreeningCheck_MissingCustomerIDRejected(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/screening/check", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleScreeningCheck_UnknownCustomerNotFound(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/screening/check", strings.NewReader(`{"customer_id":"does-not-exist"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleScreeningCheck_NoEngineConfigured(t *testing.T) {
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/screening/check", strings.NewReader(`{"customer_id":"whatever"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

// screeningCheckServer wires the durable workflow store so the check route
// takes the same persistence path production does.
func screeningCheckServer(t *testing.T) (*Server, *store.MemoryWave3Repo, string) {
	t.Helper()
	customers := store.NewMemoryCustomerRepo()
	wave3 := store.NewMemoryWave3Repo()
	s := New(":0", Deps{
		Customers: customers, Wave3: wave3, Audit: store.NewMemoryAuditRepo(),
		Screening: &engine.MockScreeningEngine{Result: &domain.ScreenResult{ListsChecked: 1, ScreenedAt: time.Now()}},
		Scoring:   &engine.MockScoringEngine{}, Monitoring: &engine.MockMonitoringEngine{},
	})
	customerID := "0000000000000000000000000000c001"
	if err := customers.Create(context.Background(), &domain.Customer{ID: customerID, ExternalID: "screening-check", CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", Status: domain.CustomerStatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	return s, wave3, customerID
}

func postScreeningCheck(t *testing.T, s *Server, customerID string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/screening/check", strings.NewReader(`{"customer_id":"`+customerID+`"}`)))
	return rec
}

func TestHandleScreeningCheck_RecordsDegradationWhenRequiredSourcesUnready(t *testing.T) {
	s, wave3, customerID := screeningCheckServer(t)
	// No source has ever been imported, so every required list is unready.

	rec := postScreeningCheck(t, s, customerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Degraded        bool     `json:"degraded"`
		DegradedSources []string `json:"degraded_sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Degraded || len(response.DegradedSources) == 0 {
		t.Fatalf("response = %+v, want the unready required sources reported", response)
	}
	// Fail-Alert: the run is executed, not blocked -- and the caveat is durable.
	runs, err := wave3.ListScreeningRuns(context.Background(), customerID, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want the screening to have gone ahead", len(runs))
	}
	if !runs[0].Degraded || len(runs[0].DegradedSources) != len(response.DegradedSources) {
		t.Fatalf("persisted run = %+v, want the degradation recorded on the run", runs[0])
	}
}

func TestHandleScreeningCheck_NoDegradationWhenSourcesFresh(t *testing.T) {
	s, wave3, customerID := screeningCheckServer(t)
	for _, id := range s.configuredScreeningSourceIDs(nil) {
		wave3.RecordScreeningSource(id, "sanctions", true, "")
	}

	rec := postScreeningCheck(t, s, customerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Degraded bool `json:"degraded"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Degraded {
		t.Fatal("freshly imported sources were reported as degraded")
	}
	runs, err := wave3.ListScreeningRuns(context.Background(), customerID, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Degraded {
		t.Fatalf("persisted runs = %+v, want one undegraded run", runs)
	}
}

func TestHandleScreeningCheck_TotalPersistenceFailureIsReportedAsServerError(t *testing.T) {
	s, wave3, customerID := screeningCheckServer(t)
	for _, id := range s.configuredScreeningSourceIDs(nil) {
		wave3.RecordScreeningSource(id, "sanctions", true, "")
	}
	// A run already occupying the id is the memory store's conflict path;
	// what matters is that the only customer in the request fails to persist.
	wave3.SetPersistFailure(errors.New("screening run store unavailable"))

	rec := postScreeningCheck(t, s, customerID)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when nothing at all was persisted", rec.Code)
	}
}

func TestHandleUpdateScreeningResult_TruePositiveCreatesCriticalCase(t *testing.T) {
	s := testServerWithScreeningResults()
	record := seedScreeningResult(t, s, domain.ScreeningResultStatusReviewing)

	body := `{"status":"TRUE_POSITIVE"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/screening/results/"+record.ID, strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated domain.ScreeningResultRecord
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Status != domain.ScreeningResultStatusTruePositive {
		t.Errorf("status = %q, want TRUE_POSITIVE", updated.Status)
	}

	cases, err := s.cases.ListByCustomer(context.Background(), record.CustomerID)
	if err != nil {
		t.Fatalf("ListByCustomer: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("cases = %+v, want exactly 1 auto-created case", cases)
	}
	if cases[0].Priority != domain.CasePriorityCritical {
		t.Errorf("case priority = %q, want %q", cases[0].Priority, domain.CasePriorityCritical)
	}
}

func TestHandleUpdateScreeningResult_TruePositiveDispatchesWebhook(t *testing.T) {
	s := testServerWithScreeningResults()
	record := seedScreeningResult(t, s, domain.ScreeningResultStatusReviewing)

	hook := &domain.Webhook{
		ID:        generateID(),
		URL:       "http://127.0.0.1:1/unreachable",
		Events:    []domain.WebhookEventType{domain.WebhookEventScreeningTruePositive},
		Active:    true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.webhooks.Create(context.Background(), hook); err != nil {
		t.Fatalf("seed webhook: %v", err)
	}

	body := `{"status":"TRUE_POSITIVE"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/screening/results/"+record.ID, strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		deliveries, err := s.webhooks.ListDeliveries(context.Background(), hook.ID, 10)
		if err != nil {
			t.Fatalf("ListDeliveries: %v", err)
		}
		if len(deliveries) > 0 {
			if deliveries[0].Event != domain.WebhookEventScreeningTruePositive {
				t.Errorf("delivery event = %q, want %q", deliveries[0].Event, domain.WebhookEventScreeningTruePositive)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for screening_true_positive webhook delivery attempt")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHandleUpdateScreeningResult_FalsePositiveRequiresReason(t *testing.T) {
	s := testServerWithScreeningResults()
	record := seedScreeningResult(t, s, domain.ScreeningResultStatusReviewing)

	body := `{"status":"FALSE_POSITIVE"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/screening/results/"+record.ID, strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	body = `{"status":"FALSE_POSITIVE","false_positive_reason":"different date of birth"}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/screening/results/"+record.ID, strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHandleUpdateScreeningResult_InvalidTransitionRejected(t *testing.T) {
	s := testServerWithScreeningResults()
	record := seedScreeningResult(t, s, domain.ScreeningResultStatusNew)

	body := `{"status":"TRUE_POSITIVE"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/screening/results/"+record.ID, strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (NEW -> TRUE_POSITIVE is not a valid direct transition)", rec.Code, http.StatusBadRequest)
	}
}
