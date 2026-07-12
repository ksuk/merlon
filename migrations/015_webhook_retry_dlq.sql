-- Webhook exponential-backoff retry state and Dead Letter Queue
-- (the HTTP API contract §3.1 "配信失敗時は指数バックオフ（初回30秒、最大6時間、最大10回）で
-- 再送する。最大再送回数を超過したイベントは Dead Letter Queue（DLQ）に退避する").
CREATE TABLE IF NOT EXISTS webhook_dlq (
    id            TEXT PRIMARY KEY,
    webhook_id    TEXT NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event_id      TEXT NOT NULL,
    event         TEXT NOT NULL,
    payload       TEXT NOT NULL,
    attempt_count INTEGER NOT NULL,
    last_error    TEXT,
    failed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reprocessed_at TIMESTAMPTZ
);

ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS event_id TEXT NOT NULL DEFAULT '';
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 1;
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_webhook_dlq_webhook_id ON webhook_dlq(webhook_id);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_next_attempt_at ON webhook_deliveries(next_attempt_at) WHERE next_attempt_at IS NOT NULL;
