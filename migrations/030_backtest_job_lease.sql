ALTER TABLE backtest_jobs ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_backtest_jobs_lease ON backtest_jobs(status, lease_expires_at);
