-- WS-6: whitelist (conditional suppression), whitelist.md
--
-- Migration number 010 is reserved for WS-6 to avoid collisions with WS-4
-- and WS-7, which are developed in parallel worktrees branched from the same
-- 008_rule_definitions_country_risk.sql head (009/011 are intentionally
-- skipped here). The alerts.suppressed/suppression_reason columns (WL-004)
-- are additive-only and included in this same file rather than a separate
-- migration number, for the same collision-avoidance reason.

CREATE TABLE IF NOT EXISTS whitelist_entries (
    id                 TEXT PRIMARY KEY,
    customer_id        TEXT NOT NULL REFERENCES customers(id),
    status             TEXT NOT NULL DEFAULT 'pending_approval'
                        CHECK (status IN ('pending_approval', 'active', 'expired', 'revoked')),
    reason             TEXT NOT NULL,
    excluded_rule_ids  TEXT[],
    valid_from         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until        TIMESTAMPTZ NOT NULL,
    requested_by       TEXT NOT NULL,
    approved_by        TEXT,
    approved_at        TIMESTAMPTZ,
    revoked_by         TEXT,
    revoked_at         TIMESTAMPTZ,
    version            INTEGER NOT NULL DEFAULT 1,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Enforces at most one active whitelist entry per customer (whitelist.md
-- §3.1 "同時実行制御"); the approve handler catches this as domain.ErrConflict.
CREATE UNIQUE INDEX IF NOT EXISTS idx_whitelist_entries_active_customer
    ON whitelist_entries (customer_id) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_whitelist_entries_customer_id ON whitelist_entries(customer_id);
CREATE INDEX IF NOT EXISTS idx_whitelist_entries_status ON whitelist_entries(status);
CREATE INDEX IF NOT EXISTS idx_whitelist_entries_valid_until ON whitelist_entries(valid_until);

CREATE TABLE IF NOT EXISTS whitelist_reviews (
    id                  TEXT PRIMARY KEY,
    whitelist_entry_id  TEXT NOT NULL REFERENCES whitelist_entries(id),
    reviewed_by         TEXT NOT NULL,
    decision            TEXT NOT NULL CHECK (decision IN ('renewed', 'revoked')),
    review_notes        TEXT,
    next_review_date    DATE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_whitelist_reviews_entry_id ON whitelist_reviews(whitelist_entry_id);

-- WL-004: alert suppression columns (additive only, Contract Stability).
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS suppressed BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS suppression_reason TEXT;
CREATE INDEX IF NOT EXISTS idx_alerts_suppressed ON alerts(suppressed);
