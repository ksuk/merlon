package events

import "context"

// Bus decouples event producers (e.g. CDD tier changes) from consumers
// (e.g. TM re-evaluation) so WS-5 and later workstreams can subscribe
// without the publisher depending on them directly (the HTTP API contract §5, the operational design
// §4.4 event delivery guarantees).
type Bus interface {
	Publish(ctx context.Context, e Event) error
	Subscribe(ctx context.Context, topic string, h func(Event)) error
}

// ReadyBus is implemented by transports that can report the point at which
// their initial subscription handshake has completed. It is separate from
// Bus so existing producers and test doubles using Subscribe remain source
// compatible.
type ReadyBus interface {
	SubscribeReady(ctx context.Context, topic string, h func(Event), onReady func()) error
}
