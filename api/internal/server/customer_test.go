package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/events"
	"github.com/ksuk/merlon/api/internal/store"
)

// fakeBus is an in-memory test double for events.Bus that records every
// published event, so tests can assert on what handleScoreCustomer (Task 8)
// publishes without a real transport.
type fakeBus struct {
	mu         sync.Mutex
	published  []events.Event
	publishErr error
}

func (b *fakeBus) Publish(_ context.Context, e events.Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.published = append(b.published, e)
	return b.publishErr
}

func (b *fakeBus) Subscribe(context.Context, string, func(events.Event)) error {
	return nil
}

func TestCreateCustomer(t *testing.T) {
	s := testServer()

	body := `{"external_id":"EXT001","customer_type":"individual","country_code":"JP","product_types":["spot_trading"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var c domain.Customer
	json.NewDecoder(rec.Body).Decode(&c)

	if c.ID == "" {
		t.Error("expected non-empty ID")
	}
	if c.ExternalID != "EXT001" {
		t.Errorf("external_id = %q, want %q", c.ExternalID, "EXT001")
	}
	if c.CustomerType != domain.CustomerTypeIndividual {
		t.Errorf("customer_type = %q, want %q", c.CustomerType, domain.CustomerTypeIndividual)
	}
}

func TestCustomerIdentityHistoryIncludesLifecycleAndCountryUpdates(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	wave3 := store.NewMemoryWave3Repo()
	s := New(":0", Deps{
		Customers: customers,
		Audit:     store.NewMemoryAuditRepo(),
		Wave3:     wave3,
	})

	create := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(`{"external_id":"IDENTITY-HISTORY","customer_type":"individual","country_code":"JP","identity":{"name":"Before"}}`))
	createdResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(createdResponse, create)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	var customer domain.Customer
	if err := json.NewDecoder(createdResponse.Body).Decode(&customer); err != nil {
		t.Fatal(err)
	}

	updateBody := fmt.Sprintf(`{"country_code":"US","status":"frozen","identity":{"name":"After"},"expected_updated_at":%q}`, customer.UpdatedAt.UTC().Format(time.RFC3339Nano))
	update := httptest.NewRequest(http.MethodPut, "/api/v1/customers/"+customer.ID, strings.NewReader(updateBody))
	updatedResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(updatedResponse, update)
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", updatedResponse.Code, updatedResponse.Body.String())
	}

	entries, err := wave3.ListCustomerIdentityHistory(context.Background(), customer.ID, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("identity history entries = %d, want creation and update", len(entries))
	}
	latest := entries[0]
	if latest.ChangedFields["after_status"] != domain.CustomerStatusFrozen || latest.ChangedFields["after_country_code"] != "US" {
		t.Fatalf("latest identity history = %+v", latest.ChangedFields)
	}
	stored, err := customers.Get(context.Background(), customer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.CustomerStatusFrozen || stored.CountryCode != "US" {
		t.Fatalf("stored lifecycle identity = %+v", stored)
	}
}

func TestCreateCustomerAuditFailureRollsBackCustomer(t *testing.T) {
	s := testServer()
	s.audit.(*store.MemoryAuditRepo).SetCreateFailure(errors.New("audit unavailable"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(`{"external_id":"EXT-AUDIT-FAIL","customer_type":"individual","country_code":"JP"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusCreated {
		t.Fatalf("status = %d, want failure when required audit append fails", rec.Code)
	}
	customers, err := s.customers.List(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("list customers: %v", err)
	}
	if len(customers) != 0 {
		t.Fatalf("customers after rollback = %d, want 0", len(customers))
	}
}

func TestUpdateCustomerAuditFailureRollsBackCustomer(t *testing.T) {
	s := testServer()
	c := &domain.Customer{ID: generateID(), ExternalID: "EXT-UPDATE-AUDIT-FAIL", CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", Status: domain.CustomerStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.customers.Create(context.Background(), c); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	original := c.CountryCode
	s.audit.(*store.MemoryAuditRepo).SetCreateFailure(errors.New("audit unavailable"))

	req := httptest.NewRequest(http.MethodPut, "/api/v1/customers/"+c.ID, strings.NewReader(`{"country_code":"US"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want failure when required audit append fails", rec.Code)
	}
	stored, err := s.customers.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("get customer: %v", err)
	}
	if stored.CountryCode != original {
		t.Fatalf("country_code after rollback = %q, want %q", stored.CountryCode, original)
	}
}

func TestScoreCustomerAuditFailureRollsBackScoreAndHistory(t *testing.T) {
	s := testServer()
	c := &domain.Customer{ID: generateID(), ExternalID: "EXT-SCORE-AUDIT-FAIL", CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", Status: domain.CustomerStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.customers.Create(context.Background(), c); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	s.audit.(*store.MemoryAuditRepo).SetCreateFailure(errors.New("audit unavailable"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+c.ID+"/score", strings.NewReader(`{"rule_set_id":"test"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want failure when required audit append fails", rec.Code)
	}
	stored, err := s.customers.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("get customer: %v", err)
	}
	if stored.RiskScore != nil || stored.RiskTier != nil {
		t.Fatalf("customer risk projection after rollback = score %v tier %v, want unset", stored.RiskScore, stored.RiskTier)
	}
	history, err := s.customers.ListScoreHistory(context.Background(), c.ID, 10)
	if err != nil {
		t.Fatalf("list score history: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("score history after rollback = %d, want 0", len(history))
	}
}

func TestScoreCustomerOutboxFailureRollsBackScoreAndHistory(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	alerts := store.NewMemoryAlertRepo()
	cases := store.NewMemoryCaseRepo()
	outbox := store.NewMemoryEventOutboxRepo()
	want := errors.New("event outbox unavailable")
	outbox.SetEnqueueFailure(want)
	s := New(":0", Deps{
		Customers:          customers,
		Transactions:       store.NewMemoryTransactionRepo(),
		Alerts:             alerts,
		Reports:            store.NewMemorySTRReportRepo(),
		Scoring:            &engine.MockScoringEngine{Score: 9.0, Tier: domain.RiskTierHigh},
		Monitoring:         &engine.MockMonitoringEngine{},
		Screening:          &engine.MockScreeningEngine{},
		Audit:              store.NewMemoryAuditRepo(),
		Cases:              cases,
		CaseAlertLifecycle: store.NewMemoryCaseAlertLifecycleRepo(cases, alerts),
		Events:             &fakeBus{},
		EventOutbox:        outbox,
	})

	create := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(`{"external_id":"OUTBOX-FAIL","customer_type":"individual","country_code":"JP"}`))
	createdResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(createdResponse, create)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create customer status = %d, body: %s", createdResponse.Code, createdResponse.Body.String())
	}
	var created domain.Customer
	if err := json.NewDecoder(createdResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	score := httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/score", strings.NewReader(`{"rule_set_id":"test"}`))
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, score)
	if response.Code == http.StatusOK {
		t.Fatalf("score status = %d, want failure when outbox enqueue fails", response.Code)
	}
	stored, err := customers.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RiskScore != nil || stored.RiskTier != nil {
		t.Fatalf("customer risk projection after outbox rollback = score %v tier %v", stored.RiskScore, stored.RiskTier)
	}
	history, err := customers.ListScoreHistory(context.Background(), created.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("score history after outbox rollback = %d, want 0", len(history))
	}
	pending, err := outbox.ListPending(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("outbox rows after rollback = %d, want 0", len(pending))
	}
}

func TestCreateCustomerMissingExternalID(t *testing.T) {
	s := testServer()

	body := `{"customer_type":"individual"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateCustomerOversizedAttributes(t *testing.T) {
	s := testServer()

	bigVal := strings.Repeat("x", 10001)
	body := `{"external_id":"EXT_BIG","customer_type":"individual","country_code":"JP","attributes":{"name":"` + bigVal + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateCustomerRejectsOversizedBody(t *testing.T) {
	s := testServer()

	body := `{"external_id":"BIG","customer_type":"individual","country_code":"JP","attributes":{"blob":"` + strings.Repeat("A", maxRequestBodyBytes) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestCreateCustomerTooManyAttributes(t *testing.T) {
	s := testServer()

	attrs := make(map[string]string, 51)
	for i := 0; i < 51; i++ {
		attrs[fmt.Sprintf("key_%d", i)] = "value"
	}
	attrsJSON, _ := json.Marshal(attrs)
	body := `{"external_id":"EXT_MANY","customer_type":"individual","country_code":"JP","attributes":` + string(attrsJSON) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateCustomerInvalidType(t *testing.T) {
	s := testServer()

	body := `{"external_id":"EXT_BAD","customer_type":"unknown_type","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestGetCustomer(t *testing.T) {
	s := testServer()

	// Create
	body := `{"external_id":"EXT002","customer_type":"corporate_domestic","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	// Get
	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+created.ID, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got domain.Customer
	json.NewDecoder(rec.Body).Decode(&got)

	if got.ID != created.ID {
		t.Errorf("id = %q, want %q", got.ID, created.ID)
	}
}

func TestGetCustomerNotFound(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers/nonexistent", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestListCustomers(t *testing.T) {
	s := testServer()

	// Create two customers
	for _, ext := range []string{"EXT010", "EXT011"} {
		body := `{"external_id":"` + ext + `","customer_type":"individual","country_code":"JP"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	customers, _ := decodeListResponse[domain.Customer](t, rec.Body)

	if len(customers) != 2 {
		t.Errorf("len = %d, want 2", len(customers))
	}
}

func TestHandleListCustomers_CursorPagination(t *testing.T) {
	s := testServer()

	for _, ext := range []string{"EXT020", "EXT021", "EXT022"} {
		body := `{"external_id":"` + ext + `","customer_type":"individual","country_code":"JP"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers?limit=2&cursor="+url.QueryEscape("bm90LWEtcmVhbC1jdXJzb3I="), nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d for malformed cursor", rec.Code, http.StatusBadRequest)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers?limit=2", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	page1, meta1 := decodeListResponse[domain.Customer](t, rec.Body)
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}
	if !meta1.HasMore || meta1.NextCursor == "" {
		t.Fatal("expected has_more with a next_cursor on first page")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/customers?limit=2&cursor="+url.QueryEscape(meta1.NextCursor), nil)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec2.Code, http.StatusOK, rec2.Body.String())
	}

	page2, meta2 := decodeListResponse[domain.Customer](t, rec2.Body)
	if len(page2) != 1 {
		t.Fatalf("page2 len = %d, want 1", len(page2))
	}
	if meta2.HasMore {
		t.Error("expected has_more = false on second page")
	}
}

func TestHandleListCustomers_ServerSearchRetainsCursorFilter(t *testing.T) {
	s := testServer()

	for i, ext := range []string{"TARGET-001", "OTHER-001", "TARGET-002"} {
		body := `{"external_id":"` + ext + `","customer_type":"individual","country_code":"JP","attributes":{"name":"` + ext + `"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create customer %d: status = %d, body: %s", i, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers?search=TARGET&limit=1", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search page 1 status = %d, body: %s", rec.Code, rec.Body.String())
	}
	page1, meta1 := decodeListResponse[domain.Customer](t, rec.Body)
	if len(page1) != 1 || !strings.Contains(page1[0].ExternalID, "TARGET") {
		t.Fatalf("search page 1 = %#v, want one TARGET customer", page1)
	}
	if !meta1.HasMore || meta1.NextCursor == "" {
		t.Fatal("expected a cursor for the second matching customer")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers?search=TARGET&limit=1&cursor="+url.QueryEscape(meta1.NextCursor), nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search page 2 status = %d, body: %s", rec.Code, rec.Body.String())
	}
	page2, meta2 := decodeListResponse[domain.Customer](t, rec.Body)
	if len(page2) != 1 || !strings.Contains(page2[0].ExternalID, "TARGET") {
		t.Fatalf("search page 2 = %#v, want one TARGET customer", page2)
	}
	if meta2.HasMore {
		t.Error("expected search page 2 to be the end")
	}
}

func TestUpdateCustomer(t *testing.T) {
	s := testServer()

	// Create
	body := `{"external_id":"EXT020","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	// Update
	updateBody := `{"country_code":"US","attributes":{"pep":"true"}}`
	req = httptest.NewRequest(http.MethodPut, "/api/v1/customers/"+created.ID, strings.NewReader(updateBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated domain.Customer
	json.NewDecoder(rec.Body).Decode(&updated)

	if updated.CountryCode != "US" {
		t.Errorf("country_code = %q, want %q", updated.CountryCode, "US")
	}
	if updated.Attributes["pep"] != "true" {
		t.Errorf("attributes[pep] = %q, want %q", updated.Attributes["pep"], "true")
	}
}

func TestUpdateCustomerRejectsStaleVersion(t *testing.T) {
	s := testServer()
	create := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(`{"external_id":"EXT-CAS","customer_type":"individual","country_code":"JP"}`))
	createdResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(createdResponse, create)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body: %s", createdResponse.Code, createdResponse.Body.String())
	}
	var created domain.Customer
	if err := json.NewDecoder(createdResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	stale := created.UpdatedAt.Format(time.RFC3339Nano)

	first := httptest.NewRequest(http.MethodPut, "/api/v1/customers/"+created.ID, strings.NewReader(`{"country_code":"US","expected_updated_at":"`+stale+`"}`))
	firstResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first CAS update status = %d, body: %s", firstResponse.Code, firstResponse.Body.String())
	}

	second := httptest.NewRequest(http.MethodPut, "/api/v1/customers/"+created.ID, strings.NewReader(`{"country_code":"GB","expected_updated_at":"`+stale+`"}`))
	secondResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(secondResponse, second)
	if secondResponse.Code != http.StatusConflict {
		t.Fatalf("stale CAS update status = %d, want %d, body: %s", secondResponse.Code, http.StatusConflict, secondResponse.Body.String())
	}
	stored, err := s.customers.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CountryCode != "US" {
		t.Fatalf("stale update changed country_code to %q", stored.CountryCode)
	}
}

func TestScoreCustomerConfirmationRequiresRationale(t *testing.T) {
	s := testServer()
	create := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(`{"external_id":"SCORE-CONFIRM","customer_type":"individual","country_code":"JP"}`))
	createdResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(createdResponse, create)
	var created domain.Customer
	if err := json.NewDecoder(createdResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	for _, body := range []string{`{"rule_set_id":"test","confirmed":false}`, `{"rule_set_id":"test","confirmed":true}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/score", strings.NewReader(body))
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, req)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, want %d", body, response.Code, http.StatusBadRequest)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/score", strings.NewReader(`{"rule_set_id":"test","confirmed":true,"rationale":"periodic KYC review"}`))
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("confirmed score status = %d, body: %s", response.Code, response.Body.String())
	}
	var record domain.ScoreRecord
	if err := json.NewDecoder(response.Body).Decode(&record); err != nil {
		t.Fatal(err)
	}
	if record.RuleSetID != "test" || record.Rationale != "periodic KYC review" {
		t.Fatalf("score snapshot = %+v", record)
	}
}

func TestScoreCustomer(t *testing.T) {
	s := testServer()

	// Create customer
	body := `{"external_id":"SCORE001","customer_type":"individual","country_code":"JP","product_types":["spot_trading"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	// Score
	scoreBody := `{"rule_set_id":"test_preset"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/score", strings.NewReader(scoreBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var record domain.ScoreRecord
	json.NewDecoder(rec.Body).Decode(&record)

	if record.Score != 2.5 {
		t.Errorf("score = %f, want 2.5", record.Score)
	}
	if record.Tier != domain.RiskTierMedium {
		t.Errorf("tier = %q, want %q", record.Tier, domain.RiskTierMedium)
	}

	// Verify customer was updated
	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+created.ID, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var updated domain.Customer
	json.NewDecoder(rec.Body).Decode(&updated)

	if updated.RiskScore == nil || *updated.RiskScore != 2.5 {
		t.Errorf("risk_score not updated")
	}
	if updated.RiskTier == nil || *updated.RiskTier != domain.RiskTierMedium {
		t.Errorf("risk_tier not updated")
	}

	// Verify score history
	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+created.ID+"/scores", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var history []domain.ScoreRecord
	json.NewDecoder(rec.Body).Decode(&history)

	if len(history) != 1 {
		t.Errorf("score history len = %d, want 1", len(history))
	}
}

// TestScoreCustomer_PublishesTierChangeEvent verifies Task 8 (CDD-009): a
// scoring call that sets a customer's risk tier publishes a
// "cdd.tier_changed" event on the configured events.Bus.
func TestScoreCustomer_PublishesTierChangeEvent(t *testing.T) {
	bus := &fakeBus{}
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Scoring:      &engine.MockScoringEngine{Score: 9.0, Tier: domain.RiskTierHigh},
		Monitoring:   &engine.MockMonitoringEngine{},
		Screening:    &engine.MockScreeningEngine{},
		Events:       bus,
	})

	body := `{"external_id":"TIER001","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	scoreBody := `{"rule_set_id":"test"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/score", strings.NewReader(scoreBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	bus.mu.Lock()
	defer bus.mu.Unlock()
	if len(bus.published) != 1 {
		t.Fatalf("published events = %d, want 1", len(bus.published))
	}
	if bus.published[0].Topic != "cdd.tier_changed" {
		t.Errorf("Topic = %q, want %q", bus.published[0].Topic, "cdd.tier_changed")
	}
	if bus.published[0].ChainID == "" {
		t.Error("expected non-empty ChainID")
	}
}

func TestPublishTierChangeReturnsPublishError(t *testing.T) {
	want := errors.New("event transport unavailable")
	s := New(":0", Deps{Events: &fakeBus{publishErr: want}})
	got := s.publishTierChange(context.Background(), "cust-1", nil, domain.RiskTierHigh, time.Now())
	if !errors.Is(got, want) {
		t.Fatalf("publishTierChange error = %v, want %v", got, want)
	}
}

// TestScoreCustomer_SetsEddRequestedAtOnHighTier verifies WS-8 Task 6: a
// scoring call that promotes a customer to High tier starts the EDD
// escalation window (the case-management workflow §EDD未実施継続時の段階的措置),
// and a second High-tier re-score does not reset it.
func TestScoreCustomer_SetsEddRequestedAtOnHighTier(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Scoring:      &engine.MockScoringEngine{Score: 9.0, Tier: domain.RiskTierHigh},
		Monitoring:   &engine.MockMonitoringEngine{},
		Screening:    &engine.MockScreeningEngine{},
	})

	body := `{"external_id":"EDD001","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	scoreBody := `{"rule_set_id":"test"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/score", strings.NewReader(scoreBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	c, err := s.customers.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if c.EddRequestedAt == nil {
		t.Fatal("expected EddRequestedAt to be set after entering High tier")
	}
	firstEddAt := *c.EddRequestedAt

	// Re-score at High again: the window must not reset.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/score", strings.NewReader(scoreBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	c, err = s.customers.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if c.EddRequestedAt == nil || !c.EddRequestedAt.Equal(firstEddAt) {
		t.Errorf("EddRequestedAt changed on re-score at High: got %v, want %v", c.EddRequestedAt, firstEddAt)
	}
}

// TestScoreCustomer_ClearsEddRequestedAtOnTierDowngrade verifies the EDD
// escalation window closes once the customer is no longer High tier, so a
// later re-entry into High tier starts a fresh window rather than resuming a
// stale one.
func TestScoreCustomer_ClosesEddWindowButKeepsEvidenceOnTierDowngrade(t *testing.T) {
	scoring := &engine.MockScoringEngine{Score: 9.0, Tier: domain.RiskTierHigh}
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Scoring:      scoring,
		Monitoring:   &engine.MockMonitoringEngine{},
		Screening:    &engine.MockScreeningEngine{},
	})

	body := `{"external_id":"EDD002","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	scoreBody := `{"rule_set_id":"test"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/score", strings.NewReader(scoreBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	scoring.Tier = domain.RiskTierMedium
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/score", strings.NewReader(scoreBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	c, err := s.customers.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// The window closes, but the evidence that EDD was ever requested survives:
	// erasing it made a routine rescore destroy the record of an outstanding
	// obligation (ADR-0021, edd_policy tier_downgrade: retain_evidence).
	if c.EddClosedAt == nil {
		t.Error("EddClosedAt = nil, want the window closed on downgrade below High")
	}
	if c.EddCloseReason != "tier_downgrade" {
		t.Errorf("EddCloseReason = %q, want tier_downgrade", c.EddCloseReason)
	}
	if c.EddRequestedAt == nil {
		t.Error("EddRequestedAt was erased; the downgrade must close the window, not delete its history")
	}
}

func TestScoreCustomerNotFound(t *testing.T) {
	s := testServer()

	body := `{"rule_set_id":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/nonexistent/score", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestScoreCustomerNoEngine(t *testing.T) {
	s := testServerWithEngine(nil, nil)

	// Create customer first
	body := `{"external_id":"NOENG","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	scoreBody := `{"rule_set_id":"test"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/score", strings.NewReader(scoreBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestScreenCustomerHit(t *testing.T) {
	mockScreening := &engine.MockScreeningEngine{
		Result: &domain.ScreenResult{
			CustomerID:   "will-be-overridden",
			Hit:          true,
			Matches:      []domain.ScreenMatch{{ListID: "sanctions", EntryID: "S001", MatchedName: "Test Name", Similarity: 0.95, ListType: "sanctions", Source: "test"}},
			ListsChecked: 1,
			ScreenedAt:   time.Now(),
		},
	}
	s := testServerWithEngines(
		&engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		&engine.MockMonitoringEngine{},
		mockScreening,
	)

	body := `{"external_id":"SCR001","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	screenBody := `{"list_ids":[]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/screen", strings.NewReader(screenBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var result domain.ScreenResult
	json.NewDecoder(rec.Body).Decode(&result)

	if !result.Hit {
		t.Error("expected hit=true")
	}
	if len(result.Matches) != 1 {
		t.Errorf("matches len = %d, want 1", len(result.Matches))
	}
}

func TestScreenCustomerNoHit(t *testing.T) {
	s := testServer()

	body := `{"external_id":"SCR002","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	screenBody := `{"list_ids":[]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/screen", strings.NewReader(screenBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var result domain.ScreenResult
	json.NewDecoder(rec.Body).Decode(&result)

	if result.Hit {
		t.Error("expected hit=false")
	}
}

func TestScreenCustomerNotFound(t *testing.T) {
	s := testServer()

	screenBody := `{"list_ids":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/nonexistent/screen", strings.NewReader(screenBody))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestScreenCustomerNoEngine(t *testing.T) {
	s := testServerWithEngines(nil, nil, nil)

	body := `{"external_id":"SCR003","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	screenBody := `{"list_ids":[]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/screen", strings.NewReader(screenBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
