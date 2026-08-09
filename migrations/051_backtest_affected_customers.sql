-- Durable per-scenario backtest outcomes.
--
-- GET /backtests/{id}/affected-customers rebuilt its answer on every request:
-- it concatenated every scenario's affected_customer_ids out of the job's
-- JSONB result, sorted the whole thing, de-duplicated it, and sliced out one
-- page. A job covering 50,000 customers paid that cost to return 50 rows, and
-- the offset paging it supported cannot be stable while it does so.
--
-- Writing the rows when the job completes turns the read into a keyset scan
-- and, more importantly, lets the answer carry per-scenario detail the
-- flattened id list could never express: whether the candidate rule set would
-- start alerting on a customer, stop alerting, or change nothing.

CREATE TABLE IF NOT EXISTS backtest_job_affected_customers (
    job_id      UUID NOT NULL REFERENCES backtest_jobs(id) ON DELETE CASCADE,
    scenario_id TEXT NOT NULL,
    customer_id UUID NOT NULL,
    delta_kind  TEXT NOT NULL CHECK (delta_kind IN ('added', 'removed', 'unchanged')),
    PRIMARY KEY (job_id, scenario_id, customer_id)
);

-- customer_id deliberately carries no foreign key to customers. This row is
-- evidence about what a rule set computed at a past moment; a customer whose
-- retention period has expired must still be purgeable, and an ON DELETE
-- RESTRICT here would let a historical comparison veto that. The job's own
-- population table (backtest_job_customers, migration 031) keeps the
-- referential link for the live cohort.

-- The read is "next page of customers for this job", optionally narrowed to
-- one scenario, ordered by customer_id. Both indexes serve that directly.
CREATE INDEX IF NOT EXISTS idx_backtest_affected_by_customer
    ON backtest_job_affected_customers (job_id, customer_id);
CREATE INDEX IF NOT EXISTS idx_backtest_affected_by_scenario
    ON backtest_job_affected_customers (job_id, scenario_id, customer_id);

-- Purging follows the job: backtest_jobs already carries purge_marked_at and
-- the retention purger deletes it (migration 049), which cascades here. There
-- is no separate marker column because these rows have no independent
-- lifecycle -- they are meaningless without the job that produced them.
