-- Records the provenance of the one-time MERLON_SEED load. The row is
-- inserted in the same transaction as the dataset, so a failed/partial load
-- leaves no completion marker and can be retried safely.
CREATE TABLE IF NOT EXISTS seed_state (
    id TEXT PRIMARY KEY,
    dataset_kind TEXT NOT NULL CHECK (dataset_kind IN ('demo', 'hardcoded')),
    completed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
