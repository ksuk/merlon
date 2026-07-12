-- Idempotency-Key header support for POST /api/v1/transactions (the HTTP API contract §4.1).
-- Nullable and additive: a resend using an already-used key is rejected via
-- the partial unique index (only enforced when the header was actually
-- supplied), independent of the pre-existing external_id UNIQUE constraint.

ALTER TABLE transactions
    ADD COLUMN idempotency_key TEXT;

CREATE UNIQUE INDEX transactions_idempotency_key_idx
    ON transactions (idempotency_key)
    WHERE idempotency_key IS NOT NULL;
