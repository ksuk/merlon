package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func signedHeaders(secret []byte, eventID string, at time.Time, body []byte) http.Header {
	timestamp := at.UTC().Format(time.RFC3339Nano)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(timestamp + "." + eventID + "."))
	mac.Write(body)
	h := make(http.Header)
	h.Set("X-Merlon-Event-Id", eventID)
	h.Set("X-Merlon-Timestamp", timestamp)
	h.Set("X-Merlon-Signature", "v1="+hex.EncodeToString(mac.Sum(nil)))
	return h
}

func TestAuthenticatorAcceptsCanonicalSignatureAndRejectsTampering(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	secret := []byte("test-secret")
	body := []byte(`{"records":[{"external_id":"c-1"}]}`)
	auth := Authenticator{Secret: secret, Clock: func() time.Time { return now }}
	h := signedHeaders(secret, "evt-1", now, body)
	if err := auth.Verify(h.Get("X-Merlon-Event-Id"), h.Get("X-Merlon-Timestamp"), h.Get("X-Merlon-Signature"), body); err != nil {
		t.Fatalf("Verify(valid): %v", err)
	}
	body[0] = ' '
	if err := auth.Verify(h.Get("X-Merlon-Event-Id"), h.Get("X-Merlon-Timestamp"), h.Get("X-Merlon-Signature"), body); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify(tampered) = %v, want ErrInvalidSignature", err)
	}
}

func TestAuthenticatorRejectsTimestampOutsideSkew(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	secret := []byte("test-secret")
	body := []byte(`[{"external_id":"c-1"}]`)
	at := now.Add(-DefaultTimestampSkew - time.Second)
	h := signedHeaders(secret, "evt-old", at, body)
	auth := Authenticator{Secret: secret, Clock: func() time.Time { return now }}
	if err := auth.Verify(h.Get("X-Merlon-Event-Id"), h.Get("X-Merlon-Timestamp"), h.Get("X-Merlon-Signature"), body); !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("Verify(old) = %v, want ErrInvalidTimestamp", err)
	}
}

func TestServiceAcceptsIdempotentReplayAndRejectsDifferentDigest(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	secret := []byte("test-secret")
	repo := NewMemoryEventRepository()
	svc := NewServiceWithConfig(Config{Repository: repo, Secret: secret, Clock: func() time.Time { return now }})
	body := []byte(`[{"external_id":"c-1"}]`)
	h := signedHeaders(secret, "evt-replay", now, body)
	first, err := svc.Accept(context.Background(), KindCustomers, h, body)
	if err != nil {
		t.Fatalf("Accept(first): %v", err)
	}
	second, err := svc.Accept(context.Background(), KindCustomers, h, body)
	if err != nil || second.ID != first.ID {
		t.Fatalf("Accept(replay) = %#v, %v", second, err)
	}
	body2 := []byte(`[{"external_id":"c-2"}]`)
	h2 := signedHeaders(secret, "evt-replay", now, body2)
	if _, err := svc.Accept(context.Background(), KindCustomers, h2, body2); !errors.Is(err, ErrConflict) {
		t.Fatalf("Accept(conflicting replay) = %v, want ErrConflict", err)
	}
	if got, _ := repo.GetEvent(context.Background(), first.ID); got.PayloadCiphertext == string(body) {
		t.Fatal("event repository contains plaintext payload")
	}
}

func TestServiceRejectsPayloadLimitsBeforePersistence(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	secret := []byte("test-secret")
	repo := NewMemoryEventRepository()
	svc := NewServiceWithConfig(Config{Repository: repo, Secret: secret, Clock: func() time.Time { return now }, MaxBodyBytes: 8, MaxRecords: 1})
	body := []byte(`[{"external_id":"c-1"}]`)
	h := signedHeaders(secret, "evt-large", now, body)
	if _, err := svc.Accept(context.Background(), KindCustomers, h, body); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("Accept(large body) = %v, want ErrPayloadTooLarge", err)
	}
	if _, err := repo.GetEvent(context.Background(), "evt-large"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("large body was persisted: %v", err)
	}

	svc = NewServiceWithConfig(Config{Repository: repo, Secret: secret, Clock: func() time.Time { return now }, MaxRecords: 1})
	body = []byte(`[{"external_id":"c-1"},{"external_id":"c-2"}]`)
	h = signedHeaders(secret, "evt-many", now, body)
	if _, err := svc.Accept(context.Background(), KindCustomers, h, body); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("Accept(too many records) = %v, want ErrPayloadTooLarge", err)
	}
}

func TestServiceProcessesPartialResultsAndRetriesDependency(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	secret := []byte("test-secret")
	repo := NewMemoryEventRepository()
	dependencyPending := true
	svc := NewServiceWithConfig(Config{
		Repository: repo, Secret: secret, Clock: func() time.Time { return now }, RetryInterval: time.Second,
		Handler: func(_ context.Context, kind Kind, index int, raw json.RawMessage) (RecordOutcome, error) {
			var value struct {
				ExternalID string `json:"external_id"`
			}
			_ = json.Unmarshal(raw, &value)
			if index == 1 && dependencyPending {
				return RecordOutcome{EntityType: strings.TrimSuffix(string(kind), "s"), ExternalID: value.ExternalID}, ErrDependency
			}
			return RecordOutcome{EntityType: strings.TrimSuffix(string(kind), "s"), ExternalID: value.ExternalID, Status: RecordUpdated}, nil
		},
	})
	body := []byte(`{"records":[{"external_id":"c-1"},{"external_id":"c-2"}]}`)
	event, err := svc.Accept(context.Background(), KindCustomers, signedHeaders(secret, "evt-partial", now, body), body)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	now = now.Add(DefaultRetryInterval)
	processed, err := svc.Process(context.Background(), event.ID)
	if err != nil || processed.Status != StatusFailed {
		t.Fatalf("Process(first) = %#v, %v; want failed retry", processed, err)
	}
	if processed.AttemptCount != 1 {
		t.Fatalf("attempt_count = %d, want 1", processed.AttemptCount)
	}
	dependencyPending = false
	now = processed.NextAttemptAt
	processed, err = svc.Process(context.Background(), event.ID)
	if err != nil || processed.Status != StatusCompleted {
		t.Fatalf("Process(second) = %#v, %v; want completed", processed, err)
	}
	view, err := svc.Get(context.Background(), event.ID)
	if err != nil || len(view.Outcomes) != 2 || view.Outcomes[1].Status != RecordUpdated {
		t.Fatalf("outcomes = %#v, %v", view.Outcomes, err)
	}
}

func TestServiceMovesRepeatedFailuresToDLQ(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	secret := []byte("test-secret")
	repo := NewMemoryEventRepository()
	svc := NewServiceWithConfig(Config{Repository: repo, Secret: secret, Clock: func() time.Time { return now }, MaxAttempts: 2, RetryInterval: time.Second, Handler: func(context.Context, Kind, int, json.RawMessage) (RecordOutcome, error) {
		return RecordOutcome{}, ErrDependency
	}})
	body := []byte(`[{"external_id":"c-1"}]`)
	event, err := svc.Accept(context.Background(), KindCustomers, signedHeaders(secret, "evt-dlq", now, body), body)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(DefaultRetryInterval)
	event, _ = svc.Process(context.Background(), event.ID)
	now = event.NextAttemptAt
	event, _ = svc.Process(context.Background(), event.ID)
	if event.Status != StatusDLQ || event.AttemptCount != 2 {
		t.Fatalf("event after retries = %#v, want dlq at 2 attempts", event)
	}
	if _, err := svc.Replay(context.Background(), event.ID); err != nil {
		t.Fatal(err)
	}
}
