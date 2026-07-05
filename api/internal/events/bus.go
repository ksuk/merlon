package events

import "context"

// Bus decouples event producers (e.g. CDD tier changes) from consumers
// (e.g. TM re-evaluation) so WS-5 and later workstreams can subscribe
// without the publisher depending on them directly (api.md §5, overview.md
// §4.4 event delivery guarantees).
type Bus interface {
	Publish(ctx context.Context, e Event) error
	Subscribe(ctx context.Context, topic string, h func(Event)) error
}
