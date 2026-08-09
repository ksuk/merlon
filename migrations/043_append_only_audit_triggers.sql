-- Forward-only append-only protection for the two historical streams that
-- predate the Wave 2 investigation migrations. Migration 036 is intentionally
-- unchanged; this migration is safe for databases that already applied 037-042.

-- Audit retention is the one documented lifecycle exception: the retention
-- owner may set purge_marked_at and later delete a marked row. No business
-- column can be rewritten, and an unmarked row cannot be deleted.
CREATE OR REPLACE FUNCTION merlon_reject_audit_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.purge_marked_at IS DISTINCT FROM OLD.purge_marked_at
           AND NEW.id = OLD.id
           AND NEW.user_id IS NOT DISTINCT FROM OLD.user_id
           AND NEW.action IS NOT DISTINCT FROM OLD.action
           AND NEW.resource_type IS NOT DISTINCT FROM OLD.resource_type
           AND NEW.resource_id IS NOT DISTINCT FROM OLD.resource_id
           AND NEW.details IS NOT DISTINCT FROM OLD.details
           AND NEW.ip_address IS NOT DISTINCT FROM OLD.ip_address
           AND NEW.user_agent IS NOT DISTINCT FROM OLD.user_agent
           AND NEW.created_at IS NOT DISTINCT FROM OLD.created_at THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'audit_logs is append-only; only purge_marked_at may change'
            USING ERRCODE = '42501';
    ELSIF TG_OP = 'DELETE' THEN
        IF OLD.purge_marked_at IS NOT NULL THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'unmarked audit_logs rows are append-only'
            USING ERRCODE = '42501';
    END IF;
    RAISE EXCEPTION 'audit_logs is append-only' USING ERRCODE = '42501';
END;
$$;

DROP TRIGGER IF EXISTS audit_logs_append_only ON audit_logs;
CREATE TRIGGER audit_logs_append_only
    BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_audit_mutation();

DROP TRIGGER IF EXISTS rule_activation_events_append_only ON rule_activation_events;
CREATE TRIGGER rule_activation_events_append_only
    BEFORE UPDATE OR DELETE ON rule_activation_events
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_append_only_mutation();
