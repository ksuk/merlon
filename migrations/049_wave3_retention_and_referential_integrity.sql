-- Wave 3 evidence retention, and the referential-integrity defects that
-- migration 045 introduced into the retention purger.
--
-- 045 added three foreign keys onto tables the purger deletes from, without
-- adding matching guards or child-first deletion:
--
--   customer_identity_history.customer_id -> customers(id)
--   screening_runs.customer_id            -> customers(id)
--   screening_result_history.result_id    -> screening_results(id)
--
-- Every customer created after 045 gets an identity-history row on create, so
-- DELETE FROM customers raised foreign_key_violation (23503) and aborted the
-- whole purge transaction. The same shape applied to screening results and to
-- cases, whose deletion is blocked by screening_results.case_id.
--
-- This migration gives every Wave 3 evidence table a purge lifecycle, teaches
-- the append-only guard to permit exactly that lifecycle and nothing else,
-- and relaxes the one foreign key that should never hold a parent hostage.

-- 1. Purge lifecycle columns.
--
-- Each Wave 3 evidence table gains the same purge_marked_at marker the
-- pre-Wave-3 tables carry, plus the matching partial index from migration 025.

ALTER TABLE screening_runs             ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;
ALTER TABLE screening_result_history   ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;
ALTER TABLE customer_identity_history  ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;
ALTER TABLE pending_evaluations        ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;
ALTER TABLE pending_evaluation_history ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;
ALTER TABLE batch_runs                 ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;
ALTER TABLE target_manifests           ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;
ALTER TABLE backtest_jobs              ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;
ALTER TABLE backtest_job_metadata      ADD COLUMN IF NOT EXISTS purge_marked_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_screening_runs_purge_marked_at             ON screening_runs (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_screening_result_history_purge_marked_at   ON screening_result_history (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_customer_identity_history_purge_marked_at  ON customer_identity_history (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_pending_evaluations_purge_marked_at        ON pending_evaluations (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_pending_evaluation_history_purge_marked_at ON pending_evaluation_history (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_batch_runs_purge_marked_at                 ON batch_runs (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_target_manifests_purge_marked_at           ON target_manifests (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_backtest_jobs_purge_marked_at              ON backtest_jobs (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_backtest_job_metadata_purge_marked_at      ON backtest_job_metadata (purge_marked_at) WHERE purge_marked_at IS NOT NULL;

-- 2. A purge-aware append-only guard.
--
-- merlon_reject_append_only_mutation() (045) rejects every UPDATE and DELETE,
-- so a table carrying it can never be purged: the retention obligation and
-- the append-only obligation were mutually unsatisfiable. This sibling grants
-- the same single exception merlon_reject_audit_mutation() (043) already
-- grants for audit_logs -- set purge_marked_at, later delete a marked row --
-- and nothing else.
--
-- Unlike the audit function it compares whole rows rather than a fixed column
-- list, so one function serves every history table and cannot fall out of
-- date when a column is added.
CREATE OR REPLACE FUNCTION merlon_reject_append_only_mutation_purgeable()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.purge_marked_at IS DISTINCT FROM OLD.purge_marked_at
           AND (to_jsonb(NEW) - 'purge_marked_at') = (to_jsonb(OLD) - 'purge_marked_at') THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION '% is append-only; only purge_marked_at may change', TG_TABLE_NAME
            USING ERRCODE = '42501';
    ELSIF TG_OP = 'DELETE' THEN
        IF OLD.purge_marked_at IS NOT NULL THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'unmarked % rows are append-only', TG_TABLE_NAME
            USING ERRCODE = '42501';
    END IF;
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE = '42501';
END;
$$;

DROP TRIGGER IF EXISTS screening_result_history_append_only ON screening_result_history;
CREATE TRIGGER screening_result_history_append_only
    BEFORE UPDATE OR DELETE ON screening_result_history
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_append_only_mutation_purgeable();

DROP TRIGGER IF EXISTS pending_evaluation_history_append_only ON pending_evaluation_history;
CREATE TRIGGER pending_evaluation_history_append_only
    BEFORE UPDATE OR DELETE ON pending_evaluation_history
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_append_only_mutation_purgeable();

DROP TRIGGER IF EXISTS customer_identity_history_append_only ON customer_identity_history;
CREATE TRIGGER customer_identity_history_append_only
    BEFORE UPDATE OR DELETE ON customer_identity_history
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_append_only_mutation_purgeable();

-- 3. The one foreign key that must not block a parent.
--
-- screening_results.case_id -> cases(id) made a purged case undeletable for as
-- long as any screening result referenced it. The case is the parent being
-- retired; the surviving result should lose the link, not veto the purge.
ALTER TABLE screening_results DROP CONSTRAINT IF EXISTS screening_results_case_id_fkey;
ALTER TABLE screening_results
    ADD CONSTRAINT screening_results_case_id_fkey
    FOREIGN KEY (case_id) REFERENCES cases(id) ON DELETE SET NULL;

-- The remaining two keep RESTRICT deliberately. Identity history and
-- screening runs are evidence about a customer: they must be purged
-- explicitly, in their own marked-and-graced step, never swept away as a
-- side effect of deleting the customer row. The purger now deletes them
-- first (see api/internal/retention/postgres_purger.go).

-- 4. Retention categories for the new evidence streams.
INSERT INTO retention_policies (data_category, retention_days, min_retention_days) VALUES
    ('pending_evaluation_data', 2555, NULL),
    ('backtest_data', 2555, NULL)
ON CONFLICT (data_category) DO NOTHING;
