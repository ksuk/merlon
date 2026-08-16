-- Additive TM transaction vocabulary. NULL preserves rows written before the
-- configuration contract began interpreting transaction_type; the engine
-- supplies a deterministic direction-based compatibility value for those rows.
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS transaction_type VARCHAR(64);
CREATE INDEX IF NOT EXISTS idx_transactions_customer_type_executed
    ON transactions (customer_id, transaction_type, executed_at);
