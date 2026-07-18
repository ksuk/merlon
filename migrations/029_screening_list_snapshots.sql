-- Durable sanctions/PEP snapshots. A successful import atomically replaces
-- one list; failed fetches leave this immutable last-good snapshot intact.
CREATE TABLE IF NOT EXISTS screening_list_snapshots (
    list_id TEXT PRIMARY KEY,
    list_type TEXT NOT NULL,
    name TEXT NOT NULL,
    source TEXT NOT NULL,
    entries JSONB NOT NULL,
    imported_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS screening_list_failures (
    list_id TEXT PRIMARY KEY,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_success_at TIMESTAMPTZ
);
