-- PR #112 review remediation: immutable completed reviews and durable failed
-- import lifecycle evidence.

ALTER TABLE import_runs
    ADD COLUMN IF NOT EXISTS error_message text NOT NULL DEFAULT '';

ALTER TABLE coverage_analyses
    ADD COLUMN IF NOT EXISTS period_from timestamptz,
    ADD COLUMN IF NOT EXISTS period_to timestamptz,
    ADD COLUMN IF NOT EXISTS rule_set_id text NOT NULL DEFAULT 'active';

UPDATE coverage_analyses
SET period_to = COALESCE(period_to, snapshot_at),
    period_from = COALESCE(period_from, snapshot_at - interval '30 days')
WHERE period_from IS NULL OR period_to IS NULL;

ALTER TABLE coverage_analyses
    ALTER COLUMN period_from SET NOT NULL,
    ALTER COLUMN period_to SET NOT NULL;

ALTER TABLE backtest_outcome_details
    ADD COLUMN IF NOT EXISTS change_kind text NOT NULL DEFAULT ''
    CHECK (change_kind IN ('', 'added', 'removed', 'changed'));

ALTER TABLE adapter_sync_runs
    ADD COLUMN IF NOT EXISTS error text NOT NULL DEFAULT '';

CREATE OR REPLACE FUNCTION reject_completed_customer_review_update()
RETURNS trigger AS $$
BEGIN
    IF OLD.status = 'completed' THEN
        RAISE EXCEPTION 'completed customer review % is immutable', OLD.id
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS customer_reviews_completed_immutable ON customer_reviews;
CREATE TRIGGER customer_reviews_completed_immutable
BEFORE UPDATE ON customer_reviews
FOR EACH ROW EXECUTE FUNCTION reject_completed_customer_review_update();
