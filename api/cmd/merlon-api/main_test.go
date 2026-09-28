package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/events"
)

type startupPinger struct {
	failures int
	calls    int
}

func (p *startupPinger) Ping(context.Context) error {
	p.calls++
	if p.calls <= p.failures {
		return errors.New("database is starting")
	}
	return nil
}

func TestWaitForDatabaseRetriesUntilReady(t *testing.T) {
	pinger := &startupPinger{failures: 2}
	if err := waitForDatabase(context.Background(), pinger, 100*time.Millisecond, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if pinger.calls != 3 {
		t.Fatalf("Ping calls = %d, want 3", pinger.calls)
	}
}

func TestWaitForDatabaseStopsAtDeadline(t *testing.T) {
	pinger := &startupPinger{failures: 100}
	err := waitForDatabase(context.Background(), pinger, 5*time.Millisecond, time.Millisecond)
	if err == nil {
		t.Fatal("waitForDatabase returned nil for a database that never became ready")
	}
	if pinger.calls < 2 {
		t.Fatalf("Ping calls = %d, want at least 2", pinger.calls)
	}
}

type startupBus struct {
	initErr error
}

func (b startupBus) Publish(context.Context, events.Event) error { return nil }

func (b startupBus) Subscribe(ctx context.Context, _ string, _ func(events.Event)) error {
	return b.initErr
}

func (b startupBus) SubscribeReady(ctx context.Context, _ string, _ func(events.Event), onReady func()) error {
	if b.initErr != nil {
		return b.initErr
	}
	onReady()
	<-ctx.Done()
	return ctx.Err()
}

func TestStartEventSubscriptionFailsBeforeReadiness(t *testing.T) {
	want := errors.New("LISTEN rejected")
	if _, err := startEventSubscription(context.Background(), startupBus{initErr: want}, "topic", func(events.Event) {}); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestStartEventSubscriptionWaitsForReadiness(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errs, err := startEventSubscription(ctx, startupBus{}, "topic", func(events.Event) {})
	if err != nil {
		t.Fatalf("startEventSubscription: %v", err)
	}
	cancel()
	if got := <-errs; !errors.Is(got, context.Canceled) {
		t.Fatalf("subscription error = %v, want context.Canceled", got)
	}
}
