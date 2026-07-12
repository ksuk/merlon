-- EDD 3-stage escalation (the case-management workflow §EDD未実施継続時の段階的措置).
-- edd_requested_at marks when the customer entered the current High-tier EDD
-- requirement window; the *_notified_at columns make RunEDDEscalationJob
-- idempotent (stage 2/3 fire at most once, stage 1 at most once per day).
ALTER TABLE customers ADD COLUMN IF NOT EXISTS edd_requested_at TIMESTAMPTZ;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS edd_stage1_last_sent_at TIMESTAMPTZ;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS edd_stage2_notified_at TIMESTAMPTZ;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS edd_stage3_notified_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_customers_edd_requested_at ON customers (edd_requested_at) WHERE edd_requested_at IS NOT NULL;

-- CasePriority gains "critical" (the case-management workflow: EDD stage 3 "ケースを
-- CRITICALに引き上げる"). Additive only: existing values are kept.
ALTER TABLE cases DROP CONSTRAINT IF EXISTS cases_priority_check;
ALTER TABLE cases ADD CONSTRAINT cases_priority_check
    CHECK (priority IN ('low', 'medium', 'high', 'critical'));
