-- WS-11 Task4: joint account entity (data-model.md §1.1.3). account_id on
-- transactions is nullable and opt-in: existing single-account operation
-- (customer_id only) is unaffected.

CREATE TYPE account_type AS ENUM ('individual', 'joint');
CREATE TYPE account_role AS ENUM ('primary', 'co_holder');

CREATE TABLE accounts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id VARCHAR(255) NOT NULL,
    account_type account_type NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT accounts_external_id_unique UNIQUE (external_id)
);

CREATE TABLE account_customers (
    account_id  UUID NOT NULL REFERENCES accounts(id),
    customer_id UUID NOT NULL REFERENCES customers(id),
    role        account_role NOT NULL,
    PRIMARY KEY (account_id, customer_id)
);

ALTER TABLE transactions
    ADD COLUMN account_id UUID REFERENCES accounts(id);

CREATE INDEX idx_account_customers_customer ON account_customers (customer_id);
CREATE INDEX idx_transactions_account_id ON transactions (account_id) WHERE account_id IS NOT NULL;
