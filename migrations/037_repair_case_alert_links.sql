-- Repair legacy case/alert link inconsistencies without changing migration
-- 036, whose checksum may already be recorded in schema_migrations.

-- Missing or cross-customer links cannot be repaired without guessing which
-- regulated record the operator intended. Fail before changing any rows and
-- report the first incompatible pair.
DO $$
DECLARE
    invalid_case_id TEXT;
    invalid_alert_id TEXT;
    invalid_reason TEXT;
BEGIN
    -- cases.alert_ids predates the UUID foreign key and contains both the
    -- compact representation emitted by the API and PostgreSQL's canonical
    -- hyphenated representation. Reject anything else explicitly: silently
    -- skipping an unparseable link would make the upgrade non-reproducible.
    SELECT c.id, linked.alert_id
    INTO invalid_case_id, invalid_alert_id
    FROM cases c
    CROSS JOIN LATERAL unnest(c.alert_ids) AS linked(alert_id)
    WHERE c.purge_marked_at IS NULL
      AND (linked.alert_id IS NULL
        OR (linked.alert_id !~* '^[0-9a-f]{32}$'
        AND linked.alert_id !~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'))
    ORDER BY c.id, linked.alert_id
    LIMIT 1;

    IF invalid_case_id IS NOT NULL THEN
        RAISE EXCEPTION 'case/alert lifecycle repair blocked: case %, alert %: invalid UUID',
            invalid_case_id, invalid_alert_id;
    END IF;

    SELECT c.id,
           linked.alert_id,
           CASE
               WHEN a.id IS NULL THEN 'missing alert'
               WHEN a.customer_id <> c.customer_id THEN 'different customer'
               ELSE 'inactive alert'
           END
    INTO invalid_case_id, invalid_alert_id, invalid_reason
    FROM cases c
    CROSS JOIN LATERAL unnest(c.alert_ids) AS linked(alert_id)
    LEFT JOIN alerts a ON a.id = replace(linked.alert_id, '-', '')::uuid AND a.purge_marked_at IS NULL
    WHERE c.purge_marked_at IS NULL
      AND (a.id IS NULL OR a.customer_id <> c.customer_id OR (
          a.status NOT IN ('open', 'investigating', 'escalated')
          AND NOT (c.status = 'str_filed' AND a.status = 'closed_true_positive' AND a.resolved_by = 'migration-037')
      ))
    ORDER BY c.id, linked.alert_id
    LIMIT 1;

    IF invalid_case_id IS NOT NULL THEN
        RAISE EXCEPTION 'case/alert lifecycle repair blocked: case %, alert %: %',
            invalid_case_id, invalid_alert_id, invalid_reason;
    END IF;
END $$;

-- Normalize every valid legacy reference to the API's compact form. Duplicate
-- array entries carry no additional evidence, so preserve the first
-- occurrence and append one queryable repair event for each changed case.
WITH normalized_cases AS (
    SELECT c.id,
           c.alert_ids AS old_alert_ids,
           ARRAY(
               SELECT item.normalized_alert_id
               FROM (
                   SELECT lower(replace(raw.alert_id, '-', '')) AS normalized_alert_id,
                          MIN(raw.ordinal) AS ordinal
                   FROM unnest(c.alert_ids) WITH ORDINALITY AS raw(alert_id, ordinal)
                   GROUP BY lower(replace(raw.alert_id, '-', ''))
               ) AS item
               ORDER BY item.ordinal
           ) AS new_alert_ids
    FROM cases c
    WHERE c.purge_marked_at IS NULL
), repaired AS (
    UPDATE cases c
    SET alert_ids = d.new_alert_ids,
        updated_at = NOW()
    FROM normalized_cases d
    WHERE c.id = d.id
      AND c.alert_ids IS DISTINCT FROM d.new_alert_ids
    RETURNING c.id, d.old_alert_ids, d.new_alert_ids
)
INSERT INTO audit_logs (user_id, action, resource_type, resource_id, details, created_at)
SELECT 'migration-037',
       'repair_case_alert_links',
       'cases',
       id,
       jsonb_build_object(
           'migration', '037',
           'reason', 'normalize_alert_ids',
           'before_alert_ids', array_to_string(old_alert_ids, ','),
           'after_alert_ids', array_to_string(new_alert_ids, ',')
       ),
       NOW()
FROM repaired;

-- STR filing is positive-disposition evidence. Any still-active linked alert
-- is therefore closed true-positive and attributed to this repair.
WITH affected_alerts AS (
    SELECT a.id,
           a.status::text AS old_status,
           array_agg(DISTINCT c.id ORDER BY c.id) AS case_ids,
           COALESCE(MAX(c.closed_at), MAX(c.updated_at), NOW()) AS repaired_at
    FROM alerts a
    JOIN cases c ON EXISTS (
        SELECT 1
        FROM unnest(c.alert_ids) AS linked(alert_id)
        WHERE a.id = replace(linked.alert_id, '-', '')::uuid
    )
    WHERE a.purge_marked_at IS NULL
      AND c.purge_marked_at IS NULL
      AND c.status = 'str_filed'
      AND a.status IN ('open', 'investigating', 'escalated')
    GROUP BY a.id, a.status
), repaired AS (
    UPDATE alerts a
    SET status = 'closed_true_positive',
        resolved_at = affected.repaired_at,
        resolved_by = 'migration-037',
        updated_at = NOW()
    FROM affected_alerts affected
    WHERE a.id = affected.id
    RETURNING a.id, affected.old_status, affected.case_ids
)
INSERT INTO audit_logs (user_id, action, resource_type, resource_id, details, created_at)
SELECT 'migration-037',
       'repair_case_alert_lifecycle',
       'alerts',
       id::text,
       jsonb_build_object(
           'migration', '037',
           'reason', 'str_filed_case_requires_terminal_alert',
           'from_status', old_status,
           'to_status', 'closed_true_positive',
           'case_ids', array_to_string(case_ids, ',')
       ),
       NOW()
FROM repaired;

-- A plain closed case does not reveal a defensible alert disposition. Prefer
-- reopening the case over inventing a true/false-positive decision.
WITH affected_cases AS (
    SELECT DISTINCT c.id, c.status::text AS old_status
    FROM cases c
    JOIN alerts a ON EXISTS (
        SELECT 1
        FROM unnest(c.alert_ids) AS linked(alert_id)
        WHERE a.id = replace(linked.alert_id, '-', '')::uuid
    )
    WHERE c.purge_marked_at IS NULL
      AND a.purge_marked_at IS NULL
      AND c.status = 'closed'
      AND a.status IN ('open', 'investigating', 'escalated')
), repaired AS (
    UPDATE cases c
    SET status = 'reopened',
        closed_at = NULL,
        reopen_reason = CASE
            WHEN btrim(c.reopen_reason) = '' THEN 'migration-037: linked alert remained unresolved'
            ELSE c.reopen_reason || E'\n' || 'migration-037: linked alert remained unresolved'
        END,
        updated_at = NOW()
    FROM affected_cases affected
    WHERE c.id = affected.id
    RETURNING c.id, affected.old_status
)
INSERT INTO audit_logs (user_id, action, resource_type, resource_id, details, created_at)
SELECT 'migration-037',
       'repair_case_alert_lifecycle',
       'cases',
       id,
       jsonb_build_object(
           'migration', '037',
           'reason', 'closed_case_had_unresolved_alert',
           'from_status', old_status,
           'to_status', 'reopened'
       ),
       NOW()
FROM repaired;
