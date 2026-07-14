-- Immutable customer population resolved for a durable backtest job.
CREATE TABLE IF NOT EXISTS backtest_job_customers (
    job_id UUID NOT NULL REFERENCES backtest_jobs(id) ON DELETE CASCADE,
    customer_id UUID NOT NULL REFERENCES customers(id),
    PRIMARY KEY (job_id, customer_id)
);
CREATE INDEX IF NOT EXISTS idx_backtest_job_customers_customer
    ON backtest_job_customers(customer_id, job_id);
