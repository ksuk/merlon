package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
	inboundwebhook "github.com/ksuk/merlon/api/internal/webhook"
)

func inboundRequest(method, path string, secret []byte, eventID string, at time.Time, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	timestamp := at.UTC().Format(time.RFC3339Nano)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(timestamp + "." + eventID + "." + body))
	r.Header.Set("X-Merlon-Event-Id", eventID)
	r.Header.Set("X-Merlon-Timestamp", timestamp)
	r.Header.Set("X-Merlon-Signature", "v1="+hex.EncodeToString(mac.Sum(nil)))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestInboundWebhookHTTPContractAndDurableStatus(t *testing.T) {
	secret := []byte("server-secret")
	customers := store.NewMemoryCustomerRepo()
	repo := store.NewMemoryInboundWebhookRepo()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	service := inboundwebhook.NewServiceWithConfig(inboundwebhook.Config{Repository: repo, Secret: secret, Clock: func() time.Time { return now }, Handler: func(_ context.Context, kind inboundwebhook.Kind, index int, raw json.RawMessage) (inboundwebhook.RecordOutcome, error) {
		var record struct {
			ExternalID string `json:"external_id"`
		}
		_ = json.Unmarshal(raw, &record)
		return inboundwebhook.RecordOutcome{EntityType: strings.TrimSuffix(string(kind), "s"), ExternalID: record.ExternalID, Status: inboundwebhook.RecordAccepted}, nil
	}})
	s := New(":0", Deps{Customers: customers, InboundWebhooks: service})
	body := `{"records":[{"external_id":"EXT-WEBHOOK-1"}]}`
	req := inboundRequest(http.MethodPost, "/api/v1/webhooks/inbound/customers", secret, "evt-http-1", now, body)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("accept status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var event inboundwebhook.Event
	if err := json.Unmarshal(rec.Body.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event.ID != "evt-http-1" || event.Status != inboundwebhook.StatusAccepted {
		t.Fatalf("event = %#v", event)
	}
	if stored, _ := repo.GetEvent(context.Background(), event.ID); stored.PayloadCiphertext == body {
		t.Fatal("plaintext webhook body was persisted")
	}

	// Same event ID/body is harmless and returns the original durable result.
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, inboundRequest(http.MethodPost, "/api/v1/webhooks/inbound/customers", secret, event.ID, now, body))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("idempotent replay status = %d", rec.Code)
	}
	conflicting := `[{"external_id":"different"}]`
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, inboundRequest(http.MethodPost, "/api/v1/webhooks/inbound/customers", secret, event.ID, now, conflicting))
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflicting replay status = %d, want 409", rec.Code)
	}

	// Invalid HMAC never creates an event row.
	bad := inboundRequest(http.MethodPost, "/api/v1/webhooks/inbound/customers", secret, "evt-bad", now, body)
	bad.Header.Set("X-Merlon-Signature", "v1="+strings.Repeat("0", 64))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, bad)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid signature status = %d", rec.Code)
	}
	if _, err := repo.GetEvent(context.Background(), "evt-bad"); err == nil {
		t.Fatal("invalid event was persisted")
	}

	now = now.Add(inboundwebhook.DefaultRetryInterval)
	if _, err := service.Process(context.Background(), event.ID); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	get := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/inbound/events/evt-http-1", nil)
	s.Handler().ServeHTTP(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("status endpoint = %d, body = %s", rec.Code, rec.Body.String())
	}
	var view inboundwebhook.EventView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Event.Status != inboundwebhook.StatusCompleted || len(view.Outcomes) != 1 {
		t.Fatalf("view = %#v", view)
	}
}

func TestInboundWebhookUsesCommonRepositoryIngestor(t *testing.T) {
	secret := []byte("server-secret")
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	repo := store.NewMemoryInboundWebhookRepo()
	now := time.Now().UTC().Truncate(time.Microsecond)
	s := New(":0", Deps{Customers: customers, Transactions: transactions, InboundWebhooks: inboundwebhook.NewServiceWithConfig(inboundwebhook.Config{Repository: repo, Secret: secret, Clock: func() time.Time { return now }})})
	body := fmt.Sprintf(`{"records":[{"external_id":"EXT-COMMON-1","customer_type":"individual","country_code":"JP","attributes":{"name":"Webhook User"},"source_updated_at":%q}]}`, now.Format(time.RFC3339Nano))
	req := inboundRequest(http.MethodPost, "/api/v1/webhooks/inbound/customers", secret, "evt-common-1", now, body)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	service := s.inboundWebhooks
	if _, err := service.Process(context.Background(), "evt-common-1"); err != nil {
		t.Fatal(err)
	}
	customer, err := customers.GetByExternalID(context.Background(), "EXT-COMMON-1")
	if err != nil || customer.CustomerType != domain.CustomerTypeIndividual {
		t.Fatalf("customer = %#v, err = %v", customer, err)
	}
}
