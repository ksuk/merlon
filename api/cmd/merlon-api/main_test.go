package main

import (
	"context"
	"errors"
	"testing"

	"github.com/ksuk/merlon/api/internal/events"
)

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
