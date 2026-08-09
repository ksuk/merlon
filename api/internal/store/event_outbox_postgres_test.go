package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestPostgresEventOutboxPersistsPendingFailureAndPublish(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgEventOutboxRepo(pool)
	ctx := context.Background()
	first := &domain.DurableEvent{ID: "pg-outbox-1-" + newTestUUID(), Topic: "test.topic", Payload: []byte(`{"n":1}`)}
	second := &domain.DurableEvent{ID: "pg-outbox-2-" + newTestUUID(), Topic: "test.topic", Payload: []byte(`{"n":2}`)}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM domain_event_outbox WHERE id = ANY($1)`, []string{first.ID, second.ID})
	})

	if err := repo.Enqueue(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repo.Enqueue(ctx, second); err != nil {
		t.Fatal(err)
	}
	if first.SequenceNum >= second.SequenceNum {
		t.Fatalf("sequence numbers = %d, %d; want increasing", first.SequenceNum, second.SequenceNum)
	}

	next := time.Now().UTC().Add(time.Hour)
	if err := repo.RecordFailure(ctx, first.ID, errors.New("transport down"), next); err != nil {
		t.Fatal(err)
	}
	pending, err := repo.ListPending(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != second.ID {
		t.Fatalf("due pending events = %+v, want only second event", pending)
	}
	var payload map[string]int
	if pending[0].Payload == nil || json.Unmarshal(pending[0].Payload, &payload) != nil || payload["n"] != 2 {
		t.Fatalf("pending payload = %s, want second payload", pending[0].Payload)
	}

	if err := repo.MarkPublished(ctx, second.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	ordered, err := repo.ListAfter(ctx, "test.topic", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered) != 2 || ordered[0].ID != first.ID || ordered[1].ID != second.ID {
		t.Fatalf("source-of-truth order = %+v", ordered)
	}
	if ordered[0].Attempts != 1 || ordered[0].LastError != "transport down" {
		t.Fatalf("failed event state = %+v, want retry metadata", ordered[0])
	}
}
