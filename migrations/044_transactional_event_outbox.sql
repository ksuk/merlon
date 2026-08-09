-- Transactional domain-event outbox. Business rows and required event intent
-- are committed together; a worker publishes the intent and records retries.
-- This is a delivery queue, not an audit stream: published_at and retry
-- metadata are operational state, while the payload and sequence remain
-- immutable evidence of what was requested.
CREATE TABLE IF NOT EXISTS domain_event_outbox (
    sequence_num   BIGSERIAL PRIMARY KEY,
    id             TEXT NOT NULL UNIQUE,
    topic          TEXT NOT NULL,
    payload        JSONB NOT NULL,
    chain_id       TEXT NOT NULL DEFAULT '',
    chain_hop_count INTEGER NOT NULL DEFAULT 0 CHECK (chain_hop_count >= 0),
    attempts       INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error     TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    next_attempt_at TIMESTAMPTZ,
    published_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_domain_event_outbox_pending
    ON domain_event_outbox (next_attempt_at, sequence_num)
    WHERE published_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_domain_event_outbox_topic_sequence
    ON domain_event_outbox (topic, sequence_num);
