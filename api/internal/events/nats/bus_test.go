package nats

import (
	"context"
	"testing"

	"github.com/ksuk/merlon/api/internal/events"
)

// TestNew_SatisfiesBusInterface documents that Bus implements events.Bus
// even though the underlying JetStream connection is not yet implemented
// (the implementation plan: NATS is interface-only for D2's first half).
func TestNew_SatisfiesBusInterface(t *testing.T) {
	var _ events.Bus = (*Bus)(nil)
}

// TestNew_ReturnsErrorWithoutRealConnection documents the current behavior:
// New does not fail merely because no NATS server is reachable at
// construction time (it defers connection errors to Publish/Subscribe), but
// Publish/Subscribe surface a clear "not yet implemented" error until the
// real JetStream client lands.
func TestNew_ReturnsErrorWithoutRealConnection(t *testing.T) {
	b, err := New("nats://127.0.0.1:4222")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := b.Publish(context.Background(), events.Event{ID: "e1", Topic: "t"}); err == nil {
		t.Error("Publish: expected error (NATS backend not yet implemented)")
	}
	if err := b.Subscribe(context.Background(), "t", func(events.Event) {}); err == nil {
		t.Error("Subscribe: expected error (NATS backend not yet implemented)")
	}
}
