-- Operator-initiated revival of a failed monitoring gap.
--
-- retry_count is the automatic retry budget: the recovery job increments it
-- and, once maxPendingRetries is spent, marks the record FAILED so it stops
-- being retried forever. An operator reviving that record is making a
-- different claim -- "the underlying problem is fixed, try again" -- and
-- counting it in the same column would let a manual revival silently consume
-- or reset the automatic budget, and would hide from the audit trail how many
-- times a person has pushed a record that keeps failing.

ALTER TABLE pending_evaluations ADD COLUMN IF NOT EXISTS manual_retry_count INTEGER NOT NULL DEFAULT 0;

-- The queue read is "oldest PENDING_REVIEW first, excluding purged rows". The
-- recovery job pages through it, so the index carries the ordering as well as
-- the predicate.
CREATE INDEX IF NOT EXISTS idx_pending_evaluations_queue
    ON pending_evaluations (status, created_at, id)
    WHERE purge_marked_at IS NULL;
