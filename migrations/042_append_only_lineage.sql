-- Immutable history projections for Wave 2 relationship and STR lifecycle
-- corrections. The mutable relationship/report rows remain query projections;
-- these streams are the reproducible audit source for every change.
CREATE TABLE IF NOT EXISTS case_relationship_events (
    id TEXT PRIMARY KEY,
    relationship_id TEXT NOT NULL,
    case_id TEXT NOT NULL REFERENCES cases(id),
    related_case_id TEXT NOT NULL REFERENCES cases(id),
    event_type TEXT NOT NULL,
    actor TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    before_state JSONB NOT NULL DEFAULT '{}',
    after_state JSONB NOT NULL DEFAULT '{}',
    correlation_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_case_relationship_events_case_created
    ON case_relationship_events (case_id, created_at, id);

CREATE TABLE IF NOT EXISTS str_report_events (
    id TEXT PRIMARY KEY,
    report_id TEXT NOT NULL REFERENCES str_reports(id),
    event_type TEXT NOT NULL,
    actor TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    before_state JSONB NOT NULL DEFAULT '{}',
    after_state JSONB NOT NULL DEFAULT '{}',
    correlation_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_str_report_events_report_created
    ON str_report_events (report_id, created_at, id);

DROP TRIGGER IF EXISTS case_relationship_events_append_only ON case_relationship_events;
CREATE TRIGGER case_relationship_events_append_only
    BEFORE UPDATE OR DELETE ON case_relationship_events
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_append_only_mutation();

DROP TRIGGER IF EXISTS str_report_events_append_only ON str_report_events;
CREATE TRIGGER str_report_events_append_only
    BEFORE UPDATE OR DELETE ON str_report_events
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_append_only_mutation();
