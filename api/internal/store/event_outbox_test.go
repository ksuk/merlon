package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestMemoryEventOutboxPreservesOrderAndRetries(t *testing.T) {
	repo := NewMemoryEventOutboxRepo()
	ctx := context.Background()
	first := &domain.DurableEvent{ID: "event-1", Topic: "topic", Payload: []byte(`{"n":1}`)}
	second := &domain.DurableEvent{ID: "event-2", Topic: "topic", Payload: []byte(`{"n":2}`)}
	if err := repo.Enqueue(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repo.Enqueue(ctx, second); err != nil {
		t.Fatal(err)
	}
	if first.SequenceNum >= second.SequenceNum {
		t.Fatalf("sequence numbers = %d, %d; want increasing", first.SequenceNum, second.SequenceNum)
	}

	next := time.Now().Add(time.Hour)
	wantErr := errors.New("transport down")
	if err := repo.RecordFailure(ctx, first.ID, wantErr, next); err != nil {
		t.Fatal(err)
	}
	pending, err := repo.ListPending(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != second.ID {
		t.Fatalf("due pending events = %+v, want only second event", pending)
	}

	if err := repo.MarkPublished(ctx, second.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	after, err := repo.ListAfter(ctx, "topic", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 || after[0].ID != first.ID || after[1].ID != second.ID {
		t.Fatalf("source-of-truth order = %+v", after)
	}
}

func TestMemoryEventOutboxRejectsDuplicateID(t *testing.T) {
	repo := NewMemoryEventOutboxRepo()
	event := &domain.DurableEvent{ID: "same", Topic: "topic", Payload: []byte(`{}`)}
	if err := repo.Enqueue(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	err := repo.Enqueue(context.Background(), &domain.DurableEvent{ID: event.ID, Topic: event.Topic, Payload: []byte(`{}`)})
	var conflict *domain.ErrConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("duplicate enqueue error = %v, want ErrConflict", err)
	}
}
