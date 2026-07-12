package events

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config selects and configures the Bus implementation via the EVENT_BUS
// environment variable (the implementation plan design decision).
type Config struct {
	// Driver is "pg_notify" (default) or "nats".
	Driver string
	// Pool is required when Driver is "pg_notify".
	Pool *pgxpool.Pool
	// NatsURL is required when Driver is "nats".
	NatsURL string
	// InstanceCount is the number of API instances this process is part
	// of. When >1 with Driver "pg_notify", the pg_notify driver logs a
	// warning since pg_notify does not fan NOTIFYs out across instances
	// the way NATS does (horizontal-scale / 10k-events-per-day threshold
	// requires NATS).
	InstanceCount int
}

// BusFactory constructs a Bus from Config. Driver packages (pgnotify, nats)
// register themselves via Register in an init() function.
type BusFactory func(cfg Config) (Bus, error)

var registry = map[string]BusFactory{}

// Register associates driver with factory. Driver packages call this from
// init() so that events.NewBus can select them by name without package
// events importing them directly — pgnotify and nats both import events
// for the Event/Bus types, so events importing them back would be an
// import cycle (the database/sql driver-registration pattern avoids this
// the same way).
func Register(driver string, factory BusFactory) {
	registry[driver] = factory
}

// NewBus builds a Bus per cfg.Driver, defaulting to pg_notify. The
// corresponding driver package (events/pgnotify, events/nats) must be
// imported somewhere in the program (for its init() side effect) before
// NewBus is called.
func NewBus(cfg Config) (Bus, error) {
	driver := cfg.Driver
	if driver == "" {
		driver = "pg_notify"
	}

	factory, ok := registry[driver]
	if !ok {
		return nil, fmt.Errorf("unknown EVENT_BUS driver: %q (is its package imported for init() registration?)", driver)
	}
	return factory(cfg)
}
