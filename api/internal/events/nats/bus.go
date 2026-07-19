// Package nats provides an events.Bus skeleton for NATS JetStream. The real
// connection/publish/subscribe logic is deferred to a later D2 task
// (the implementation plan design decision: NATS ships interface-only
// until the horizontal-scale / 10k-events-per-day threshold requires it);
// this package only guarantees the interface contract so EVENT_BUS=nats can
// be selected without a compile-time dependency change once the real
// client is added.
package nats

import (
	"context"
	"errors"

	"github.com/ksuk/merlon/api/internal/events"
)

func init() {
	events.Register("nats", func(cfg events.Config) (events.Bus, error) {
		return New(cfg.NatsURL)
	})
}

// Bus is a not-yet-connected placeholder satisfying events.Bus.
type Bus struct {
	url string
}

var _ events.Bus = (*Bus)(nil)
var _ events.ReadyBus = (*Bus)(nil)

// New records url for the eventual JetStream connection. It does not dial
// out yet, so it cannot fail merely because no NATS server is reachable.
func New(url string) (*Bus, error) {
	return &Bus{url: url}, nil
}

var errNotImplemented = errors.New("nats backend not yet implemented")

func (b *Bus) Publish(_ context.Context, _ events.Event) error {
	return errNotImplemented
}

func (b *Bus) Subscribe(_ context.Context, _ string, _ func(events.Event)) error {
	return errNotImplemented
}

func (b *Bus) SubscribeReady(_ context.Context, _ string, _ func(events.Event), _ func()) error {
	return errNotImplemented
}
