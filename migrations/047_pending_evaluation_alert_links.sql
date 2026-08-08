-- Wave 3 #73: persist the alerts produced by a recovered pending evaluation.
-- This is additive so legacy pending rows remain valid and old migrations stay
-- immutable.
ALTER TABLE pending_evaluations
    ADD COLUMN IF NOT EXISTS alert_ids text[] NOT NULL DEFAULT '{}';
