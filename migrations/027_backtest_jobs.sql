-- PH9 durable asynchronous backtests. Results are stored as JSON because the
-- engine contract is versioned independently from the job scheduler; no alert
-- or case rows are created by a backtest.
CREATE TABLE IF NOT EXISTS backtest_jobs (
    id UUID PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('queued','running','completed','failed','cancelled')),
    from_at TIMESTAMPTZ NOT NULL,
    to_at TIMESTAMPTZ NOT NULL,
    customer_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    customer_filter JSONB,
    scenario_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    baseline_rule_set_id TEXT NOT NULL,
    candidate_rule_set_id TEXT NOT NULL,
    config_digests JSONB NOT NULL DEFAULT '{}'::jsonb,
    snapshot_at TIMESTAMPTZ NOT NULL,
    total_customers INTEGER NOT NULL DEFAULT 0,
    processed_customers INTEGER NOT NULL DEFAULT 0,
    progress DOUBLE PRECISION NOT NULL DEFAULT 0,
    eta_seconds BIGINT,
    baseline JSONB,
    candidate JSONB,
    delta JSONB,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_backtest_jobs_status_created ON backtest_jobs(status, created_at);
CREATE INDEX IF NOT EXISTS idx_customers_created_id ON customers(created_at, id);
CREATE INDEX IF NOT EXISTS idx_transactions_customer_event_id ON transactions(customer_id, executed_at, id);
