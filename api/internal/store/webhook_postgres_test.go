package store

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestPostgresWebhookDeliveryAndDLQSurviveRepositoryReload(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgWebhookRepo(pool, nil)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	hook := &domain.Webhook{
		ID: "pg-webhook-" + newTestUUID(), URL: "https://example.com/merlon",
		Events: []domain.WebhookEventType{domain.WebhookEventAlertCreated}, Secret: "test-secret",
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM webhooks WHERE id=$1`, hook.ID) })
	if err := repo.Create(ctx, hook); err != nil {
		t.Fatal(err)
	}

	loaded, err := repo.Get(ctx, hook.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Secret != hook.Secret || len(loaded.Events) != 1 || loaded.Events[0] != hook.Events[0] {
		t.Fatalf("webhook reload = %+v, want durable configuration", loaded)
	}

	due := time.Now().UTC().Add(-time.Minute)
	delivery := &domain.WebhookDelivery{
		ID: "pg-delivery-" + newTestUUID(), WebhookID: hook.ID, Event: domain.WebhookEventAlertCreated,
		Payload: `{"id":"alert-1"}`, EventID: "event-" + newTestUUID(), AttemptCount: 0,
		CreatedAt: now, NextAttemptAt: &due,
	}
	if err := repo.CreateDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	pending, err := repo.ListPendingRetries(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != delivery.ID || pending[0].AttemptCount != 0 {
		t.Fatalf("pending deliveries = %+v, want persisted pre-send intent", pending)
	}

	delivery.Success = true
	delivery.StatusCode = 204
	delivery.AttemptCount = 1
	delivery.NextAttemptAt = nil
	if err := repo.UpdateDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	reloadedDeliveries, err := repo.ListDeliveries(ctx, hook.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloadedDeliveries) != 1 || !reloadedDeliveries[0].Success || reloadedDeliveries[0].EventID != delivery.EventID {
		t.Fatalf("reloaded deliveries = %+v, want successful stable event id", reloadedDeliveries)
	}

	entry := &domain.DLQEntry{
		ID: "pg-dlq-" + newTestUUID(), WebhookID: hook.ID, EventID: delivery.EventID,
		Event: domain.WebhookEventAlertCreated, Payload: delivery.Payload, AttemptCount: 10,
		LastError: "receiver unavailable", FailedAt: now,
	}
	if err := repo.CreateDLQEntry(ctx, entry); err != nil {
		t.Fatal(err)
	}
	count, err := repo.CountDLQEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("active DLQ count = %d, want 1", count)
	}
	gotEntry, err := repo.GetDLQEntry(ctx, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotEntry.EventID != entry.EventID || gotEntry.AttemptCount != 10 {
		t.Fatalf("DLQ reload = %+v, want stable event and attempt count", gotEntry)
	}
	if err := repo.MarkDLQEntryReprocessed(ctx, entry.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	count, err = repo.CountDLQEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("active DLQ count after reprocess = %d, want 0", count)
	}
}
