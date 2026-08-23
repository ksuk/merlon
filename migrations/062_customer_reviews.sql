-- P08 / issues #103-104: durable periodic CDD review queue and evidence.
-- customer_reviews is the source of truth for review history. The current
-- schedule is projected onto customers for cheap customer-detail reads; a
-- projection can always be rebuilt from this table.
ALTER TABLE customers ADD COLUMN IF NOT EXISTS next_review_at timestamptz;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS last_review_at timestamptz;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS review_tier risk_tier;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS review_policy_version text NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS review_policy_digest text NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS customer_reviews (
    id text PRIMARY KEY,
    customer_id uuid NOT NULL REFERENCES customers(id),
    cycle integer NOT NULL CHECK (cycle > 0),
    status text NOT NULL CHECK (status IN ('scheduled', 'due', 'overdue', 'in_progress', 'blocked', 'completed')),
    outcome text CHECK (outcome IS NULL OR outcome IN ('rating_unchanged', 'rating_changed', 'escalated_to_edd', 'unable_to_complete')),
    tier risk_tier NOT NULL,
    previous_tier risk_tier,
    resulting_tier risk_tier,
    assigned_to text NOT NULL DEFAULT '',
    assigned_team text NOT NULL DEFAULT '',
    priority text NOT NULL DEFAULT 'medium' CHECK (priority IN ('low', 'medium', 'high', 'critical')),
    due_at timestamptz NOT NULL,
    grace_until timestamptz NOT NULL,
    overdue_at timestamptz,
    policy_version text NOT NULL,
    policy_digest text NOT NULL,
    scope jsonb NOT NULL DEFAULT '{}',
    rationale text NOT NULL DEFAULT '',
    evidence_refs jsonb NOT NULL DEFAULT '[]',
    previous_score_id text NOT NULL DEFAULT '',
    resulting_score_id text NOT NULL DEFAULT '',
    actor text NOT NULL DEFAULT '',
    scheduled_at timestamptz NOT NULL,
    started_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE(customer_id, cycle)
);

CREATE INDEX IF NOT EXISTS customer_reviews_queue_idx
    ON customer_reviews(due_at, id);
CREATE INDEX IF NOT EXISTS customer_reviews_status_idx
    ON customer_reviews(status, due_at, id);
CREATE INDEX IF NOT EXISTS customer_reviews_customer_idx
    ON customer_reviews(customer_id, cycle DESC);

-- A review cycle may move through its operational statuses, but the cycle
-- itself is evidence and must never be removed or replaced by a later cycle.
-- There is no business delete path; keep the database guard in place for
-- privileged or ad-hoc SQL callers as well.
CREATE OR REPLACE FUNCTION merlon_reject_customer_review_delete()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'customer_reviews is append-only; cycle % cannot be deleted', OLD.id
        USING ERRCODE = '42501';
END;
$$;

DROP TRIGGER IF EXISTS customer_reviews_append_only ON customer_reviews;
CREATE TRIGGER customer_reviews_append_only
    BEFORE DELETE ON customer_reviews
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_customer_review_delete();
