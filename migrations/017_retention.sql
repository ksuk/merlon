-- Data retention policy (the audit design RET-001/RET-002/RET-003, §6 保持期間表).
-- Statutory retention data categories (customer/transaction/alert-case/CDD
-- score history) may only be extended, never shortened, hence
-- min_retention_days is set equal to the initial retention_days and enforced
-- by retention_no_shorten below. audit_log has no statutory lower bound
-- (min_retention_days NULL), so its retention may be changed freely.
CREATE TABLE retention_policies (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    data_category      VARCHAR(50) NOT NULL UNIQUE,
    retention_days     INTEGER NOT NULL,
    min_retention_days INTEGER,
    updated_by         VARCHAR(255),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT retention_no_shorten CHECK (
        min_retention_days IS NULL OR retention_days >= min_retention_days
    )
);

INSERT INTO retention_policies (data_category, retention_days, min_retention_days) VALUES
    ('customer_data', 2555, 2555),
    ('transaction_data', 2555, 2555),
    ('alert_case_data', 2555, 2555),
    ('cdd_score_history', 2555, 2555),
    ('audit_log', 3650, NULL);

-- APPI anonymization (RET-004, the data model §3.7): anonymized_at marks a
-- customer whose direct-PII attributes fields have been replaced after the
-- statutory retention period elapsed. NULL means not anonymized.
ALTER TABLE customers ADD COLUMN IF NOT EXISTS anonymized_at TIMESTAMPTZ;

-- Rule effectiveness review tracking (the audit design §8): last time a scenario's
-- backtest/detection-analytics review was recorded as completed. NULL means
-- never reviewed, which the dashboard (the operational design §3.3) flags once the
-- configured review interval (default 1 year) has elapsed.
ALTER TABLE rule_definitions ADD COLUMN IF NOT EXISTS last_effectiveness_review_at TIMESTAMPTZ;
