package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

func testServerWithWebhooks() *Server {
	return New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Scoring:      &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Monitoring:   &engine.MockMonitoringEngine{},
		Screening:    &engine.MockScreeningEngine{},
		Backtest:     &engine.MockBacktestEngine{},
		Audit:        store.NewMemoryAuditRepo(),
		Cases:        store.NewMemoryCaseRepo(),
		Webhooks:     store.NewMemoryWebhookRepo(),
	})
}

func TestCreateWebhook(t *testing.T) {
	s := testServerWithWebhooks()

	body := `{"url":"https://example.com/webhook","events":["alert.created","case.created"],"secret":"mysecret"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var hook domain.Webhook
	json.NewDecoder(rec.Body).Decode(&hook)

	if hook.ID == "" {
		t.Error("expected non-empty ID")
	}
	if hook.URL != "https://example.com/webhook" {
		t.Errorf("url = %q, want %q", hook.URL, "https://example.com/webhook")
	}
	if len(hook.Events) != 2 {
		t.Errorf("events count = %d, want 2", len(hook.Events))
	}
	if !hook.Active {
		t.Error("expected active to be true")
	}
}

func TestCreateWebhookSecretRedactedOnCreate(t *testing.T) {
	s := testServerWithWebhooks()

	body := `{"url":"https://example.com/webhook","events":["alert.created"],"secret":"mysecret"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)

	if _, ok := resp["secret"]; ok {
		t.Error("create response should not include secret")
	}
}

func TestWebhookRedirectToPrivateAddressIsBlocked(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("private redirect target should not be reached")
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	if err := postWebhook(context.Background(), redirector.URL, nil, nil); err == nil {
		t.Fatal("expected redirect to private address to be blocked")
	}
}

func TestWebhookSecretNotInListResponse(t *testing.T) {
	s := testServerWithWebhooks()

	body := `{"url":"https://example.com/webhook","events":["alert.created"],"secret":"mysecret"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/webhooks", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var hooks []map[string]any
	json.NewDecoder(rec.Body).Decode(&hooks)

	if len(hooks) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(hooks))
	}
	if _, ok := hooks[0]["secret"]; ok {
		t.Error("secret should not appear in list response")
	}
}

func TestWebhookSecretNotInGetResponse(t *testing.T) {
	s := testServerWithWebhooks()

	body := `{"url":"https://example.com/webhook","events":["alert.created"],"secret":"topsecret"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created map[string]any
	json.NewDecoder(rec.Body).Decode(&created)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/"+created["id"].(string), nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var fetched map[string]any
	json.NewDecoder(rec.Body).Decode(&fetched)

	if _, ok := fetched["secret"]; ok {
		t.Error("secret should not appear in get response")
	}
}

func TestCreateWebhookSSRFPrivateIP(t *testing.T) {
	s := testServerWithWebhooks()

	tests := []struct {
		name string
		url  string
	}{
		{"loopback", "http://127.0.0.1/hook"},
		{"private_10", "http://10.0.0.1/hook"},
		{"private_172", "http://172.16.0.1/hook"},
		{"private_192", "http://192.168.1.1/hook"},
		{"localhost", "http://localhost/hook"},
		{"ipv6_loopback", "http://[::1]/hook"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"url":"` + tt.url + `","events":["alert.created"]}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d for url %s", rec.Code, http.StatusBadRequest, tt.url)
			}
		})
	}
}

func TestCreateWebhookMissingURL(t *testing.T) {
	s := testServerWithWebhooks()

	body := `{"events":["alert.created"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateWebhookMissingEvents(t *testing.T) {
	s := testServerWithWebhooks()

	body := `{"url":"https://example.com/webhook"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListWebhooks(t *testing.T) {
	s := testServerWithWebhooks()

	body := `{"url":"https://example.com/hook1","events":["alert.created"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/webhooks", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var hooks []domain.Webhook
	json.NewDecoder(rec.Body).Decode(&hooks)

	if len(hooks) != 1 {
		t.Errorf("count = %d, want 1", len(hooks))
	}
}

func TestGetWebhook(t *testing.T) {
	s := testServerWithWebhooks()

	body := `{"url":"https://example.com/hook","events":["case.updated"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Webhook
	json.NewDecoder(rec.Body).Decode(&created)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/"+created.ID, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var fetched domain.Webhook
	json.NewDecoder(rec.Body).Decode(&fetched)
	if fetched.ID != created.ID {
		t.Errorf("id = %q, want %q", fetched.ID, created.ID)
	}
}

func TestDeleteWebhook(t *testing.T) {
	s := testServerWithWebhooks()

	body := `{"url":"https://example.com/hook","events":["alert.created"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Webhook
	json.NewDecoder(rec.Body).Decode(&created)

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/webhooks/"+created.ID, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/"+created.ID, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d after delete", rec.Code, http.StatusNotFound)
	}
}

func TestWebhookDeliveries(t *testing.T) {
	s := testServerWithWebhooks()

	body := `{"url":"https://example.com/hook","events":["alert.created"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Webhook
	json.NewDecoder(rec.Body).Decode(&created)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/"+created.ID+"/deliveries", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var deliveries []domain.WebhookDelivery
	json.NewDecoder(rec.Body).Decode(&deliveries)

	if len(deliveries) != 0 {
		t.Errorf("expected 0 deliveries, got %d", len(deliveries))
	}
}

func TestOpenAPIEndpoint(t *testing.T) {
	s := testServerWithWebhooks()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var spec map[string]any
	json.NewDecoder(rec.Body).Decode(&spec)

	if spec["openapi"] != "3.0.3" {
		t.Errorf("openapi = %v, want 3.0.3", spec["openapi"])
	}

	info, ok := spec["info"].(map[string]any)
	if !ok {
		t.Fatal("missing info")
	}
	if info["title"] != "Merlon AML/CFT API" {
		t.Errorf("title = %v", info["title"])
	}

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("missing paths")
	}
	if len(paths) < 20 {
		t.Errorf("paths count = %d, expected >= 20", len(paths))
	}
}
