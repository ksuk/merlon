-- WS-5: TM enhancement — alert deduplication, transaction-monitoring.md
-- 「アラート統合ロジック」/「バッチ/リアルタイム評価の重複アラート防止」
--
-- Migration number 012 (not 011, which WS-7 already used for
-- screening_results.sql) to avoid collisions with WS-8, developed in
-- parallel from the same 011_screening_results.sql head.

ALTER TABLE alerts ADD COLUMN IF NOT EXISTS aggregation_window_start TIMESTAMPTZ;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS batch_run_id UUID;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS batch_reviewed_at TIMESTAMPTZ;

-- Enforces that a batch/realtime pair (or two batch runs) never produce two
-- alerts for the same customer/scenario/aggregation window. Scenarios with
-- no aggregation window (aggregation_window_start IS NULL) are exempt, since
-- there is nothing to dedupe against.
CREATE UNIQUE INDEX IF NOT EXISTS idx_alerts_dedup
    ON alerts (customer_id, scenario_id, aggregation_window_start)
    WHERE aggregation_window_start IS NOT NULL;
