package events

import (
	"encoding/json"
	"time"
)

// Event is the unit of propagation on the Bus. Payload carries only the
// data needed to identify what changed; consumers re-query the
// source-of-truth table for full detail (the HTTP API contract §5, the operational design §4.4 event
// delivery guarantees — NOTIFY payloads are size-limited, so they are
// notifications, not the event content itself).
type Event struct {
	ID          string          `json:"id"`
	Topic       string          `json:"topic"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	SequenceNum int64           `json:"sequence_num"`
	// ChainID (event_chain_id) links an event to the chain of events that
	// triggered it, so handlers can cut off unbounded propagation loops
	// (the CDD scoring design safety valve 4: circular dependency prevention).
	ChainID string `json:"chain_id"`
	// ChainHopCount counts how many times an event has re-triggered CDD
	// rescoring along the same ChainID. Handlers must stop propagating
	// (and increment merlon_cdd_event_chain_truncated_total) once this
	// exceeds the configured hop limit (default 3, the CDD scoring design safety
	// valve 4), instead of re-publishing indefinitely.
	ChainHopCount int       `json:"chain_hop_count"`
	CreatedAt     time.Time `json:"created_at"`
}
