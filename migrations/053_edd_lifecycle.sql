-- An explicit end to an EDD window.
--
-- EDD could be requested and escalated but never finished. The only states a
-- customer record could express were "requested" and "escalated to stage 3",
-- so an operator who had completed the enhanced due diligence had nowhere to
-- record that fact: the window stayed open, the escalation job kept counting,
-- and the read model kept reporting a customer as outstanding indefinitely.
--
-- Worse, a tier downgrade nulled the four stage timestamps outright. The
-- evidence that EDD had been requested, and how far it had escalated before
-- the customer's risk fell, was destroyed by a routine rescore.

ALTER TABLE customers ADD COLUMN IF NOT EXISTS edd_completed_at  TIMESTAMPTZ;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS edd_closed_at     TIMESTAMPTZ;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS edd_close_reason  TEXT;
-- edd_case_id records the case the escalation job opened. It was previously
-- discoverable only by string-matching a marker inside a case summary.
ALTER TABLE customers ADD COLUMN IF NOT EXISTS edd_case_id       UUID;

CREATE INDEX IF NOT EXISTS idx_customers_edd_open
    ON customers (edd_requested_at)
    WHERE edd_requested_at IS NOT NULL AND edd_closed_at IS NULL;

-- The lifecycle of a window, kept as evidence rather than as state. A closed
-- window's history is the only thing that can answer "was EDD ever completed
-- for this customer, by whom, and on what grounds" after the customer's tier
-- has moved on.
CREATE TABLE IF NOT EXISTS customer_edd_events (
    id              UUID PRIMARY KEY,
    customer_id     UUID NOT NULL REFERENCES customers(id),
    event_type      TEXT NOT NULL CHECK (event_type IN ('requested', 'stage_escalated', 'completed', 'reopened', 'closed_on_downgrade')),
    stage           TEXT,
    rationale       TEXT NOT NULL DEFAULT '',
    case_id         UUID,
    actor           TEXT NOT NULL,
    policy_version  TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    purge_marked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_customer_edd_events_customer
    ON customer_edd_events (customer_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_customer_edd_events_purge_marked_at
    ON customer_edd_events (purge_marked_at) WHERE purge_marked_at IS NOT NULL;

-- Append-only, with the single purge exception migration 049 established for
-- every Wave 3 evidence table.
DROP TRIGGER IF EXISTS trg_customer_edd_events_append_only ON customer_edd_events;
CREATE TRIGGER trg_customer_edd_events_append_only
    BEFORE UPDATE OR DELETE ON customer_edd_events
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_append_only_mutation_purgeable();
