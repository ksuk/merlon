-- Atomic maker-checker evidence for database-backed rule state changes.
-- The serving role receives INSERT/SELECT only through the audit hardening
-- procedure; production startup rejects ownership or UPDATE/DELETE grants.
CREATE TABLE IF NOT EXISTS rule_activation_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_definition_id UUID NOT NULL REFERENCES rule_definitions(id),
    rule_name VARCHAR(255) NOT NULL,
    rule_version INTEGER NOT NULL,
    requested_active BOOLEAN NOT NULL,
    rule_created_by VARCHAR(255) NOT NULL,
    approved_by VARCHAR(255) NOT NULL,
    changed BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT rule_activation_events_independent_approver
        CHECK (rule_created_by <> approved_by)
);

CREATE INDEX IF NOT EXISTS idx_rule_activation_events_rule_created
    ON rule_activation_events (rule_name, created_at DESC);
