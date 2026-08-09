-- Wave 3 #74: retain the per-customer outcome of every durable manual run.
-- The object is a server-pinned snapshot; audit_logs and the transactional
-- outbox retain the append-only action history for each update.
ALTER TABLE batch_runs
    ADD COLUMN IF NOT EXISTS customer_outcomes JSONB NOT NULL DEFAULT '{}'::jsonb;
