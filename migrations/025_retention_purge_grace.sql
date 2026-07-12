-- Retention lifecycle: records become unavailable at purge_marked_at and are
-- physically removed after the application's 30-day grace period.
ALTER TABLE customers ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;
ALTER TABLE cases ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;
ALTER TABLE customer_score_history ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;
ALTER TABLE screening_results ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_customers_purge_marked_at ON customers (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_transactions_purge_marked_at ON transactions (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_alerts_purge_marked_at ON alerts (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_cases_purge_marked_at ON cases (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_score_history_purge_marked_at ON customer_score_history (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_audit_logs_purge_marked_at ON audit_logs (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_screening_results_purge_marked_at ON screening_results (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
