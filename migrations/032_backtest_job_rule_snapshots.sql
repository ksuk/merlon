-- PH9: pin versioned rule bodies when a durable backtest is created. The
-- reference alone is not reproducible because an active rule may be replaced
-- while a queued job is waiting for a worker.
ALTER TABLE backtest_jobs
    ADD COLUMN IF NOT EXISTS baseline_rule_version INTEGER,
    ADD COLUMN IF NOT EXISTS candidate_rule_version INTEGER,
    ADD COLUMN IF NOT EXISTS baseline_rule_definition JSONB,
    ADD COLUMN IF NOT EXISTS candidate_rule_definition JSONB;
