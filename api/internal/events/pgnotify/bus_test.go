package pgnotify

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/events"
)

// newTestPool connects to MERLON_DATABASE_URL for integration tests. It
// skips (not fails) when the variable is unset, matching the pattern used
// by api/internal/store's Postgres integration tests.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("MERLON_DATABASE_URL")
	if dsn == "" {
		t.Skip("MERLON_DATABASE_URL not set, skipping Postgres integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("pool.Ping: %v", err)
	}
	return pool
}

// TestPgNotifyBus_PublishSubscribe verifies the basic LISTEN/NOTIFY round
// trip: an Event published on a topic is delivered to a handler subscribed
// to that same topic (the HTTP API contract §5).
func TestPgNotifyBus_PublishSubscribe(t *testing.T) {
	pool := newTestPool(t)
	bus := New(pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan events.Event, 1)
	subscribed := make(chan struct{})
	go func() {
		_ = bus.SubscribeReady(ctx, "ws4_test_topic", func(e events.Event) {
			received <- e
		}, func() {
			close(subscribed)
		})
	}()
	<-subscribed

	if err := bus.Publish(context.Background(), events.Event{ID: "e1", Topic: "ws4_test_topic", SequenceNum: 1}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case e := <-received:
		if e.ID != "e1" {
			t.Errorf("ID = %s, want e1", e.ID)
		}
		if e.SequenceNum != 1 {
			t.Errorf("SequenceNum = %d, want 1", e.SequenceNum)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for notification")
	}
}

// TestPgNotifyBus_ReconnectRequeriesCatchup verifies the reconnect path
// (the operational design §4.4 event delivery guarantees): after a dropped connection,
// the injected Requery callback is invoked to catch up on events missed
// while disconnected, and its results are delivered to the handler.
func TestPgNotifyBus_ReconnectRequeriesCatchup(t *testing.T) {
	bus := New(nil)
	bus.lastSeq["topic1"] = 3

	var calls int32
	bus.Requery = func(_ context.Context, topic string, afterSeq int64) ([]events.Event, error) {
		atomic.AddInt32(&calls, 1)
		if topic != "topic1" {
			t.Errorf("topic = %s, want topic1", topic)
		}
		if afterSeq != 3 {
			t.Errorf("afterSeq = %d, want 3", afterSeq)
		}
		return []events.Event{{ID: "requeried", Topic: topic, SequenceNum: afterSeq + 1}}, nil
	}

	var received []events.Event
	bus.catchUp(context.Background(), "topic1", func(e events.Event) {
		received = append(received, e)
	})

	if calls != 1 {
		t.Errorf("Requery called %d times, want 1", calls)
	}
	if len(received) != 1 || received[0].ID != "requeried" {
		t.Errorf("received = %+v, want one requeried event", received)
	}
}

// TestPgNotifyBus_SequenceGapWaitsThenFallsBackToRequery verifies that a
// detected sequence-number gap waits the configured GapWait (default 5s,
// the operational design §4.4) before falling back to a source-of-truth requery. The
// sleep function is injected so the test does not actually block.
func TestPgNotifyBus_SequenceGapWaitsThenFallsBackToRequery(t *testing.T) {
	bus := New(nil)
	bus.lastSeq["topic1"] = 5

	var slept time.Duration
	bus.sleep = func(d time.Duration) { slept = d }

	var calls int32
	bus.Requery = func(_ context.Context, topic string, afterSeq int64) ([]events.Event, error) {
		atomic.AddInt32(&calls, 1)
		if afterSeq != 5 {
			t.Errorf("afterSeq = %d, want 5", afterSeq)
		}
		return nil, nil
	}

	// Simulate receiving sequence 7 when the last seen was 5 (missing 6).
	bus.handleNotification(context.Background(), "topic1", notifyPayload{EventID: "e7", SequenceNum: 7}, func(events.Event) {})

	if slept != defaultGapWait {
		t.Errorf("slept = %v, want %v", slept, defaultGapWait)
	}
	if calls != 1 {
		t.Errorf("Requery called %d times, want 1", calls)
	}
}

// TestPgNotifyBus_NoGapDeliversDirectly ensures the common case (no gap)
// does not sleep or requery.
func TestPgNotifyBus_NoGapDeliversDirectly(t *testing.T) {
	bus := New(nil)
	bus.lastSeq["topic1"] = 5

	var slept time.Duration
	bus.sleep = func(d time.Duration) { slept = d }
	bus.Requery = func(context.Context, string, int64) ([]events.Event, error) {
		t.Fatal("Requery should not be called when there is no gap")
		return nil, nil
	}

	var received []events.Event
	bus.handleNotification(context.Background(), "topic1", notifyPayload{EventID: "e6", SequenceNum: 6}, func(e events.Event) {
		received = append(received, e)
	})

	if slept != 0 {
		t.Errorf("slept = %v, want 0 (no gap)", slept)
	}
	if len(received) != 1 || received[0].ID != "e6" {
		t.Errorf("received = %+v, want one e6 event", received)
	}
}
