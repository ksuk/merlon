-- Durable STR report lifecycle for Wave 2 issue #62.
-- Source IDs remain foreign-key links while the transaction snapshot preserves
-- the exact evidence reviewed when the draft was created.
CREATE TABLE IF NOT EXISTS str_reports (
    id                  TEXT PRIMARY KEY,
    alert_id            UUID NOT NULL REFERENCES alerts(id),
    customer_id         UUID NOT NULL REFERENCES customers(id),
    case_id             TEXT REFERENCES cases(id),
    report_type         TEXT NOT NULL DEFAULT 'str' CHECK (report_type = 'str'),
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'submitted')),
    suspicious_point    TEXT NOT NULL,
    transaction_ids     UUID[] NOT NULL DEFAULT '{}',
    transaction_snapshot JSONB NOT NULL DEFAULT '[]',
    total_amount        NUMERIC(18,2) NOT NULL,
    currency            VARCHAR(3) NOT NULL,
    alert_snapshot      JSONB NOT NULL DEFAULT '{}',
    customer_snapshot   JSONB NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    submitted_at        TIMESTAMPTZ,
    created_by          TEXT NOT NULL DEFAULT '',
    submitted_by        TEXT,
    submission_evidence TEXT,
    CONSTRAINT str_reports_submitted_evidence_check CHECK (
        status = 'draft'
        OR (submitted_at IS NOT NULL AND NULLIF(BTRIM(submission_evidence), '') IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_str_reports_created_at
    ON str_reports (created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_str_reports_status_created_at
    ON str_reports (status, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_str_reports_customer_id
    ON str_reports (customer_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_str_reports_alert_id
    ON str_reports (alert_id);
