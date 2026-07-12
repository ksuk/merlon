package events_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/events"
	"github.com/ksuk/merlon/api/internal/events/nats"
	"github.com/ksuk/merlon/api/internal/events/pgnotify"
	"github.com/ksuk/merlon/api/internal/logging"
)

func withTestLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(logging.NewLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestNewBus_PgNotifyDefault verifies pg_notify is selected when EVENT_BUS
// is unset (the implementation plan design decision: pg_notify default).
func TestNewBus_PgNotifyDefault(t *testing.T) {
	withTestLogger(t)

	bus, err := events.NewBus(events.Config{})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	if _, ok := bus.(*pgnotify.Bus); !ok {
		t.Errorf("bus = %T, want *pgnotify.Bus", bus)
	}
}

// TestNewBus_NatsSelection verifies EVENT_BUS=nats selects the NATS bus
// skeleton (Task 7).
func TestNewBus_NatsSelection(t *testing.T) {
	withTestLogger(t)

	bus, err := events.NewBus(events.Config{Driver: "nats", NatsURL: "nats://127.0.0.1:4222"})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	if _, ok := bus.(*nats.Bus); !ok {
		t.Errorf("bus = %T, want *nats.Bus", bus)
	}
}

// TestNewBus_HorizontalScaleWarning verifies that selecting pg_notify with
// more than one API instance configured logs a warning, since pg_notify
// does not fan out NOTIFYs across separate LISTEN connections on different
// instances the way NATS would (the operational design §4.4 / the implementation plan
// horizontal-scale constraint).
func TestNewBus_HorizontalScaleWarning(t *testing.T) {
	buf := withTestLogger(t)

	if _, err := events.NewBus(events.Config{Driver: "pg_notify", InstanceCount: 3}); err != nil {
		t.Fatalf("NewBus: %v", err)
	}

	if !strings.Contains(buf.String(), "horizontal") {
		t.Errorf("expected horizontal-scale warning in log output, got: %s", buf.String())
	}
}

// TestNewBus_UnknownDriver verifies an unrecognized EVENT_BUS value is
// rejected rather than silently falling back to a default.
func TestNewBus_UnknownDriver(t *testing.T) {
	withTestLogger(t)

	if _, err := events.NewBus(events.Config{Driver: "kafka"}); err == nil {
		t.Error("expected error for unknown driver")
	}
}
