-- Wave 1 lifecycle invariants for operator alerts and cases.
--
-- The statements are intentionally re-runnable. Existing rows are repaired
-- before the checks are installed so an upgrade cannot be blocked by the
-- legacy behavior that populated resolution metadata on every alert update
-- or left str_filed cases in the open queue.

-- Active alerts never carry terminal resolution metadata.
UPDATE alerts
SET resolved_at = NULL,
    resolved_by = NULL
WHERE status IN ('open', 'investigating', 'escalated');

-- Preserve the historical timestamp for terminal rows and provide a
-- migration actor where old rows did not record one. Later operator updates
-- use the authenticated actor supplied by the API.
UPDATE alerts
SET resolved_at = COALESCE(resolved_at, updated_at, created_at, NOW()),
    resolved_by = COALESCE(NULLIF(resolved_by, ''), 'migration-036')
WHERE status IN ('closed_true_positive', 'closed_false_positive');

ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_lifecycle_resolution_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_lifecycle_resolution_check
    CHECK (
        (status IN ('open', 'investigating', 'escalated')
            AND resolved_at IS NULL
            AND COALESCE(resolved_by, '') = '')
        OR
        (status IN ('closed_true_positive', 'closed_false_positive')
            AND resolved_at IS NOT NULL)
    );

CREATE INDEX IF NOT EXISTS idx_alerts_unresolved_lifecycle
    ON alerts (created_at DESC, id DESC)
    WHERE status IN ('open', 'investigating', 'escalated');

-- Case queue membership and closed_at follow the same explicit active versus
-- terminal split. "open" remains a backward-compatible active alias for
-- "new".
UPDATE cases
SET closed_at = NULL
WHERE status IN ('open', 'new', 'investigating', 'escalated', 'reopened');

UPDATE cases
SET closed_at = COALESCE(closed_at, updated_at, created_at, NOW())
WHERE status IN ('closed', 'str_filed');

ALTER TABLE cases DROP CONSTRAINT IF EXISTS cases_lifecycle_closed_at_check;
ALTER TABLE cases ADD CONSTRAINT cases_lifecycle_closed_at_check
    CHECK (
        (status IN ('open', 'new', 'investigating', 'escalated', 'reopened')
            AND closed_at IS NULL)
        OR
        (status IN ('closed', 'str_filed')
            AND closed_at IS NOT NULL)
    );

CREATE INDEX IF NOT EXISTS idx_cases_unresolved_lifecycle
    ON cases (created_at DESC, id DESC)
    WHERE status IN ('open', 'new', 'investigating', 'escalated', 'reopened');
