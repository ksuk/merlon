-- Distinguish an intentionally empty resolved population from a job that has
-- not been materialized yet. This keeps filter snapshots durable across worker
-- retries without inventing a sentinel customer row.
CREATE TABLE IF NOT EXISTS backtest_job_customer_snapshots (
    job_id UUID PRIMARY KEY REFERENCES backtest_jobs(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
