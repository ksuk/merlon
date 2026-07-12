-- WS-5 Task 6: batch job run tracking for idempotent resume
-- (the transaction-monitoring design「バッチ評価のスケジューリング」, the operational design §4.4 バッチジョブ障害復旧)

CREATE TABLE batch_runs (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_type               TEXT NOT NULL,
    status                 TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed')),
    started_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at           TIMESTAMPTZ,
    processed_customer_ids UUID[] NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_batch_runs_job_type_status ON batch_runs (job_type, status);
