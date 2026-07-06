-- WS-11 Task5: transactions.counterparty travel-rule data (data-model.md
-- §1.3.1). Neither column existed yet in migrations/002_transactions_alerts.sql
-- (only the scalar counterparty_id/counterparty_country did), so both are
-- added here additively, nullable, no backfill required.

ALTER TABLE transactions
    ADD COLUMN counterparty JSONB,
    ADD COLUMN metadata JSONB;
