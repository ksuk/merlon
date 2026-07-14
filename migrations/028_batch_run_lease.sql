-- PH9 bounded TM batch traversal. The legacy processed UUID array remains for
-- backward-compatible resume, while a keyset cursor and an active-run guard
-- prevent unbounded per-customer row growth and split-brain schedulers.
ALTER TABLE batch_runs ADD COLUMN IF NOT EXISTS customer_cursor_created_at TIMESTAMPTZ;
ALTER TABLE batch_runs ADD COLUMN IF NOT EXISTS customer_cursor_id UUID;
CREATE UNIQUE INDEX IF NOT EXISTS idx_batch_runs_one_active
    ON batch_runs(job_type) WHERE status = 'running';
