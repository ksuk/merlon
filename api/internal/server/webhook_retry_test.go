package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

// createTestWebhook registers a webhook pointing at a loopback
// httptest.NewServer URL. It temporarily disables the SSRF private-IP guard
// (webhookAllowLoopbackForTests), which otherwise rejects loopback targets
// in both URL validation and at HTTP dial time.
func createTestWebhook(t *testing.T, s *Server, url string, events []string) domain.Webhook {
	t.Helper()
	webhookAllowLoopbackForTests = true
	t.Cleanup(func() { webhookAllowLoopbackForTests = false })

	eventsJSON, _ := json.Marshal(events)
	body := `{"url":"` + url + `","events":` + string(eventsJSON) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create webhook failed: %d %s", rec.Code, rec.Body.String())
	}
	var hook domain.Webhook
	json.NewDecoder(rec.Body).Decode(&hook)
	return hook
}

// TestComputeBackoff_ExponentialWithCaps verifies api.md §3.1's backoff
// schedule: 30s initially, doubling each attempt, capped at 6h.
func TestComputeBackoff_ExponentialWithCaps(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 30 * time.Second},
		{2, 60 * time.Second},
		{3, 120 * time.Second},
		{4, 240 * time.Second},
		{5, 480 * time.Second},
		{6, 960 * time.Second},
		{7, 1920 * time.Second},
		{8, 3840 * time.Second},
		{9, 7680 * time.Second},
		{10, 15360 * time.Second},
		{20, 6 * time.Hour},
	}
	for _, tt := range tests {
		if got := computeBackoff(tt.attempt); got != tt.want {
			t.Errorf("computeBackoff(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

// TestComputeBackoff_MaxAttemptsIs10 verifies that the 10th consecutive
// failed retry moves the event to the DLQ instead of scheduling attempt 11.
func TestComputeBackoff_MaxAttemptsIs10(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()

	s := testServerWithWebhooks()
	hook := createTestWebhook(t, s, failing.URL, []string{"alert.created"})

	ctx := context.Background()
	eventID := "evt-max-attempts"
	s.deliverWebhook(hook, domain.WebhookEventAlertCreated, eventID, []byte(`{"id":"a1"}`))

	deliveries, err := s.webhooks.ListDeliveries(ctx, hook.ID, 1)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d, err %v", len(deliveries), err)
	}
	d := deliveries[0]
	if d.AttemptCount != 1 {
		t.Fatalf("initial attempt_count = %d, want 1", d.AttemptCount)
	}

	for d.AttemptCount < webhookMaxAttempts {
		if err := s.retryFailedDelivery(ctx, &d); err != nil {
			t.Fatalf("retryFailedDelivery: %v", err)
		}
	}

	if d.AttemptCount != webhookMaxAttempts {
		t.Errorf("final attempt_count = %d, want %d", d.AttemptCount, webhookMaxAttempts)
	}
	if d.NextAttemptAt != nil {
		t.Error("expected NextAttemptAt to be nil after exhausting retries")
	}

	dlqEntries, err := s.webhooks.ListDLQEntries(ctx)
	if err != nil {
		t.Fatalf("ListDLQEntries: %v", err)
	}
	if len(dlqEntries) != 1 {
		t.Fatalf("expected 1 DLQ entry, got %d", len(dlqEntries))
	}
	if dlqEntries[0].EventID != eventID {
		t.Errorf("DLQ entry event_id = %q, want %q", dlqEntries[0].EventID, eventID)
	}
	if dlqEntries[0].AttemptCount != webhookMaxAttempts {
		t.Errorf("DLQ entry attempt_count = %d, want %d", dlqEntries[0].AttemptCount, webhookMaxAttempts)
	}
}

// TestDeliverWebhook_SameEventIdAcrossRetries verifies api.md §4.2: retries
// of the same event must carry the same X-Merlon-Event-Id so the receiver
// can deduplicate.
func TestDeliverWebhook_SameEventIdAcrossRetries(t *testing.T) {
	var receivedEventIDs []string
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedEventIDs = append(receivedEventIDs, r.Header.Get("X-Merlon-Event-Id"))
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()

	s := testServerWithWebhooks()
	hook := createTestWebhook(t, s, failing.URL, []string{"alert.created"})

	ctx := context.Background()
	eventID := "evt-same-id"
	s.deliverWebhook(hook, domain.WebhookEventAlertCreated, eventID, []byte(`{"id":"a1"}`))

	deliveries, _ := s.webhooks.ListDeliveries(ctx, hook.ID, 1)
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(deliveries))
	}
	d := deliveries[0]

	if err := s.retryFailedDelivery(ctx, &d); err != nil {
		t.Fatalf("retryFailedDelivery: %v", err)
	}

	if len(receivedEventIDs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(receivedEventIDs))
	}
	if receivedEventIDs[0] != eventID || receivedEventIDs[1] != eventID {
		t.Errorf("event ids = %v, want both %q", receivedEventIDs, eventID)
	}
	if d.EventID != eventID {
		t.Errorf("delivery event_id = %q, want %q", d.EventID, eventID)
	}
}

// TestDeliverWebhook_IncludesTimestampHeader verifies X-Merlon-Timestamp
// (Unix epoch) is present on delivery (api.md §3).
func TestDeliverWebhook_IncludesTimestampHeader(t *testing.T) {
	var gotTimestamp string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTimestamp = r.Header.Get("X-Merlon-Timestamp")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	s := testServerWithWebhooks()
	hook := createTestWebhook(t, s, ts.URL, []string{"alert.created"})

	before := time.Now().Unix()
	s.deliverWebhook(hook, domain.WebhookEventAlertCreated, "evt-ts", []byte(`{"id":"a1"}`))
	after := time.Now().Unix()

	if gotTimestamp == "" {
		t.Fatal("expected X-Merlon-Timestamp header to be set")
	}
	ts64, err := strconv.ParseInt(gotTimestamp, 10, 64)
	if err != nil {
		t.Fatalf("X-Merlon-Timestamp not a valid integer: %q", gotTimestamp)
	}
	if ts64 < before || ts64 > after {
		t.Errorf("timestamp %d not within [%d, %d]", ts64, before, after)
	}
}

func TestHandleListDLQEntries_ReturnsUndeliveredEvents(t *testing.T) {
	s := testServerWithWebhooks()
	hook := createTestWebhook(t, s, "https://example.com/hook", []string{"alert.created"})

	ctx := context.Background()
	reprocessedAt := time.Now()
	s.webhooks.CreateDLQEntry(ctx, &domain.DLQEntry{
		ID: "dlq-pending", WebhookID: hook.ID, EventID: "evt-pending",
		Event: domain.WebhookEventAlertCreated, Payload: `{}`, AttemptCount: 10,
		LastError: "boom", FailedAt: time.Now(),
	})
	s.webhooks.CreateDLQEntry(ctx, &domain.DLQEntry{
		ID: "dlq-done", WebhookID: hook.ID, EventID: "evt-done",
		Event: domain.WebhookEventAlertCreated, Payload: `{}`, AttemptCount: 10,
		LastError: "boom", FailedAt: time.Now(), ReprocessedAt: &reprocessedAt,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/dlq", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var entries []domain.DLQEntry
	json.NewDecoder(rec.Body).Decode(&entries)
	if len(entries) != 1 {
		t.Fatalf("expected 1 undelivered entry, got %d", len(entries))
	}
	if entries[0].ID != "dlq-pending" {
		t.Errorf("entry id = %q, want %q", entries[0].ID, "dlq-pending")
	}
}

func TestHandleReprocessDLQEntry_RedeliversAndRecordsAudit(t *testing.T) {
	var received bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	s := testServerWithWebhooks()
	hook := createTestWebhook(t, s, ts.URL, []string{"alert.created"})

	ctx := context.Background()
	s.webhooks.CreateDLQEntry(ctx, &domain.DLQEntry{
		ID: "dlq-reprocess", WebhookID: hook.ID, EventID: "evt-reprocess",
		Event: domain.WebhookEventAlertCreated, Payload: `{"id":"a1"}`, AttemptCount: 10,
		LastError: "boom", FailedAt: time.Now(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/dlq/dlq-reprocess/reprocess", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !received {
		t.Error("expected the webhook endpoint to receive the reprocessed request")
	}

	var resp reprocessDLQEntryResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if !resp.Success {
		t.Errorf("expected reprocess success = true, got %+v", resp)
	}

	entry, err := s.webhooks.GetDLQEntry(ctx, "dlq-reprocess")
	if err != nil {
		t.Fatalf("GetDLQEntry: %v", err)
	}
	if entry.ReprocessedAt == nil {
		t.Error("expected ReprocessedAt to be set after reprocessing")
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/api/v1/audit?resource_type=webhook_dlq&resource_id=dlq-reprocess", nil)
	auditRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(auditRec, auditReq)
	if auditRec.Code != http.StatusOK {
		t.Fatalf("audit status = %d, want %d, body: %s", auditRec.Code, http.StatusOK, auditRec.Body.String())
	}
	var auditEntries []domain.AuditEntry
	json.NewDecoder(auditRec.Body).Decode(&auditEntries)
	if len(auditEntries) < 1 {
		t.Fatal("expected at least 1 audit entry for the reprocess action")
	}
	if auditEntries[0].Action != "reprocess_dlq_entry" {
		t.Errorf("audit action = %q, want %q", auditEntries[0].Action, "reprocess_dlq_entry")
	}
}
