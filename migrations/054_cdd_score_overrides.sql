-- Maker-checker for CDD score overrides.
--
-- POST /customers/{id}/score accepted an arbitrary override_evidence object
-- and wrote it straight onto the score record, and the accompanying score
-- moved the customer's risk_tier immediately. One person could therefore
-- lower a customer out of High tier -- which is what decides EDD, monitoring
-- thresholds and rescreening frequency -- with no second signature and no
-- shape constraint on the justification they attached.
--
-- An override now becomes a proposal. The customer's tier does not move until
-- a different person approves it, exactly as whitelist_entries (migration 010)
-- already works for alert suppression.

CREATE TABLE IF NOT EXISTS cdd_score_overrides (
    id                UUID PRIMARY KEY,
    customer_id       UUID NOT NULL REFERENCES customers(id),
    score_record_id   TEXT,
    proposed_tier     TEXT NOT NULL,
    computed_tier     TEXT NOT NULL,
    computed_score    DOUBLE PRECISION NOT NULL,
    reason            TEXT NOT NULL,
    supporting_documents TEXT[] NOT NULL DEFAULT '{}',
    evidence          JSONB NOT NULL DEFAULT '{}'::jsonb,
    status            TEXT NOT NULL DEFAULT 'pending_approval'
                          CHECK (status IN ('pending_approval', 'approved', 'rejected')),
    requested_by      TEXT NOT NULL,
    requested_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_by        TEXT,
    decided_at        TIMESTAMPTZ,
    decision_rationale TEXT NOT NULL DEFAULT '',
    version           INTEGER NOT NULL DEFAULT 1,
    purge_marked_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_cdd_score_overrides_customer
    ON cdd_score_overrides (customer_id, requested_at DESC, id DESC);
-- The reviewer's working queue is "what is waiting on me", so it gets its own
-- partial index rather than scanning decided history.
CREATE INDEX IF NOT EXISTS idx_cdd_score_overrides_pending
    ON cdd_score_overrides (requested_at, id) WHERE status = 'pending_approval';
CREATE INDEX IF NOT EXISTS idx_cdd_score_overrides_purge_marked_at
    ON cdd_score_overrides (purge_marked_at) WHERE purge_marked_at IS NOT NULL;
