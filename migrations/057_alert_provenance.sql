-- Immutable per-alert detection provenance.
--
-- An alert recorded its scenario, severity, score and description, but nothing
-- about the logic that produced it. The monitoring request already carried the
-- configuration digests -- realtime, batch and recovery all pass them -- and the
-- engine discarded them when it built the alert. After a rule changed, nobody
-- could show which version had been effective at detection time, so the
-- investigation record could not be reproduced or independently explained.
--
-- What is deliberately NOT done here:
--
-- Existing alerts are not backfilled. Writing the current configuration onto a
-- past detection would produce a record that looks like evidence and is not;
-- provenance_captured stays false and the API reports those alerts as
-- not_captured. That is a smaller loss than a fabricated one.
--
-- The rule body is not copied. rule_definitions already holds immutable version
-- rows (001_init.sql: a new version is always an INSERT, never an UPDATE), so
-- provenance points at them by name and digest instead of duplicating content
-- that would then need its own authorization and purge rules (ADR-0025, DR-19).
--
-- No foreign key to rule_definitions is added. The scenario an alert names is
-- not always a stored rule -- the native engine also loads scenarios from the
-- configuration root -- so a constraint would refuse legitimate alerts. The
-- retention invariant is enforced by machine verification instead, the same
-- judgement migration 049 made for the customer purge guard.

ALTER TABLE alerts ADD COLUMN IF NOT EXISTS provenance_captured BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS provenance_config_digests JSONB;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS provenance_engine_version TEXT;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS provenance_evaluation_mode TEXT;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS provenance_evaluated_at TIMESTAMPTZ;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS provenance_window_from TIMESTAMPTZ;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS provenance_window_to TIMESTAMPTZ;
-- The threshold that actually applied to this customer type and risk tier. It
-- is one number the alert description already states in prose, kept here in a
-- form that can be queried, and is not the rule body.
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS provenance_applied_threshold DOUBLE PRECISION;

-- Finding an alert by the configuration that produced it is the reviewer's
-- question ("which alerts did this rule set generate"), so it gets the index.
CREATE INDEX IF NOT EXISTS idx_alerts_provenance_captured
    ON alerts (provenance_captured)
    WHERE provenance_captured;

-- Provenance is immutable once captured. Alerts themselves are not: status,
-- assignment and disposition all change over their lifetime, so the whole-row
-- append-only trigger from migration 049 does not fit. This guards the
-- provenance columns specifically and leaves the rest of the row alone.
CREATE OR REPLACE FUNCTION merlon_reject_alert_provenance_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.provenance_captured IS DISTINCT FROM NEW.provenance_captured
        OR OLD.provenance_config_digests IS DISTINCT FROM NEW.provenance_config_digests
        OR OLD.provenance_engine_version IS DISTINCT FROM NEW.provenance_engine_version
        OR OLD.provenance_evaluation_mode IS DISTINCT FROM NEW.provenance_evaluation_mode
        OR OLD.provenance_evaluated_at IS DISTINCT FROM NEW.provenance_evaluated_at
        OR OLD.provenance_window_from IS DISTINCT FROM NEW.provenance_window_from
        OR OLD.provenance_window_to IS DISTINCT FROM NEW.provenance_window_to
        OR OLD.provenance_applied_threshold IS DISTINCT FROM NEW.provenance_applied_threshold
    THEN
        -- The only exception is a row that never carried provenance being left
        -- alone: an alert created before this migration keeps provenance_captured
        -- false forever, and no later write may turn it true.
        RAISE EXCEPTION 'alert provenance is immutable after creation (alert %)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_alerts_provenance_immutable ON alerts;
CREATE TRIGGER trg_alerts_provenance_immutable
    BEFORE UPDATE ON alerts
    FOR EACH ROW
    EXECUTE FUNCTION merlon_reject_alert_provenance_mutation();
