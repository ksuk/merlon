-- Wave 2 operator workflow foundation (#63-#68).
-- All additions are nullable/additive so existing Wave 0-1 records remain
-- readable and migration 036 is never rewritten.

ALTER TABLE alerts ADD COLUMN IF NOT EXISTS assigned_to TEXT;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS assigned_team TEXT;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS due_at TIMESTAMPTZ;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS disposition TEXT;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS disposition_rationale TEXT NOT NULL DEFAULT '';

ALTER TABLE cases ADD COLUMN IF NOT EXISTS assigned_team TEXT;
ALTER TABLE cases ADD COLUMN IF NOT EXISTS due_at TIMESTAMPTZ;
ALTER TABLE cases ADD COLUMN IF NOT EXISTS investigation_disposition TEXT NOT NULL DEFAULT '';
ALTER TABLE cases ADD COLUMN IF NOT EXISTS str_candidate BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE cases ADD COLUMN IF NOT EXISTS disposition_rationale TEXT NOT NULL DEFAULT '';
ALTER TABLE cases ADD COLUMN IF NOT EXISTS str_report_id TEXT REFERENCES str_reports(id);
ALTER TABLE cases ADD COLUMN IF NOT EXISTS str_filed_at TIMESTAMPTZ;
ALTER TABLE cases ADD COLUMN IF NOT EXISTS str_filed_by TEXT;
ALTER TABLE cases ADD COLUMN IF NOT EXISTS str_filing_channel TEXT;
ALTER TABLE cases ADD COLUMN IF NOT EXISTS str_destination TEXT;
ALTER TABLE cases ADD COLUMN IF NOT EXISTS str_external_reference TEXT;

-- Pin the source rows used to compose a report so later queue edits do not
-- alter CSV/JSON exports for the same durable report ID.
ALTER TABLE str_reports ADD COLUMN IF NOT EXISTS alert_snapshot JSONB NOT NULL DEFAULT '{}';
ALTER TABLE str_reports ADD COLUMN IF NOT EXISTS customer_snapshot JSONB NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_alerts_queue_owner ON alerts (assigned_to, assigned_team, due_at);
CREATE INDEX IF NOT EXISTS idx_cases_queue_owner ON cases (assigned_to, assigned_team, due_at);
CREATE INDEX IF NOT EXISTS idx_cases_str_report_id ON cases (str_report_id);

-- Append-only case investigation timeline. JSONB before/after preserves an
-- auditable representation of every correction without overwriting history.
CREATE TABLE IF NOT EXISTS case_events (
    id TEXT PRIMARY KEY,
    case_id TEXT NOT NULL REFERENCES cases(id),
    event_type TEXT NOT NULL,
    actor TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    before_state JSONB NOT NULL DEFAULT '{}',
    after_state JSONB NOT NULL DEFAULT '{}',
    related_alert_ids TEXT[] NOT NULL DEFAULT '{}',
    related_case_ids TEXT[] NOT NULL DEFAULT '{}',
    related_report_ids TEXT[] NOT NULL DEFAULT '{}',
    correlation_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_case_events_case_created ON case_events (case_id, created_at, id);

CREATE TABLE IF NOT EXISTS case_evidence (
    id TEXT PRIMARY KEY,
    case_id TEXT NOT NULL REFERENCES cases(id),
    description TEXT NOT NULL,
    source TEXT NOT NULL,
    evidence_type TEXT NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    collected_by TEXT NOT NULL,
    integrity_hash TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_case_evidence_case_created ON case_evidence (case_id, created_at, id);

CREATE TABLE IF NOT EXISTS case_checklist_items (
    case_id TEXT NOT NULL REFERENCES cases(id),
    item_key TEXT NOT NULL,
    label TEXT NOT NULL,
    completed BOOLEAN NOT NULL DEFAULT FALSE,
    completed_by TEXT NOT NULL DEFAULT '',
    completed_at TIMESTAMPTZ,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (case_id, item_key)
);

CREATE TABLE IF NOT EXISTS case_work_items (
    id TEXT PRIMARY KEY,
    case_id TEXT NOT NULL REFERENCES cases(id),
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',
    assigned_to TEXT NOT NULL DEFAULT '',
    due_at TIMESTAMPTZ,
    completed_by TEXT NOT NULL DEFAULT '',
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_case_work_items_case_created ON case_work_items (case_id, created_at, id);

CREATE TABLE IF NOT EXISTS case_relationships (
    id TEXT PRIMARY KEY,
    case_id TEXT NOT NULL REFERENCES cases(id),
    related_case_id TEXT NOT NULL REFERENCES cases(id),
    relationship_type TEXT NOT NULL DEFAULT 'related',
    rationale TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    removed_by TEXT NOT NULL DEFAULT '',
    removed_at TIMESTAMPTZ,
    removal_reason TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'manual',
    CONSTRAINT case_relationships_no_self CHECK (case_id <> related_case_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_case_relationships_active_unique
    ON case_relationships (case_id, related_case_id) WHERE active;
CREATE INDEX IF NOT EXISTS idx_case_relationships_case ON case_relationships (case_id, created_at, id);

CREATE TABLE IF NOT EXISTS alert_decision_events (
    id TEXT PRIMARY KEY,
    alert_id UUID NOT NULL REFERENCES alerts(id),
    from_status TEXT NOT NULL,
    to_status TEXT NOT NULL,
    outcome TEXT NOT NULL,
    rationale TEXT NOT NULL,
    actor TEXT NOT NULL,
    supersedes_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_alert_decisions_alert_created ON alert_decision_events (alert_id, created_at, id);

-- Database-level defense for the append-only evidence streams. The serving
-- role is also denied UPDATE/DELETE by docs/operations/audit-hardening.sql,
-- but the trigger keeps an accidental privileged application path from
-- rewriting the history that operators rely on for reversals and corrections.
CREATE OR REPLACE FUNCTION merlon_reject_append_only_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE = '42501';
END;
$$;

DROP TRIGGER IF EXISTS case_events_append_only ON case_events;
CREATE TRIGGER case_events_append_only
    BEFORE UPDATE OR DELETE ON case_events
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_append_only_mutation();

DROP TRIGGER IF EXISTS alert_decision_events_append_only ON alert_decision_events;
CREATE TRIGGER alert_decision_events_append_only
    BEFORE UPDATE OR DELETE ON alert_decision_events
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_append_only_mutation();
