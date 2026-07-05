-- WS-11 Task1: customers.status lifecycle (data-model.md §1.1, §1.1.2)

CREATE TYPE customer_status AS ENUM ('active', 'dormant', 'frozen', 'closed');

ALTER TABLE customers
    ADD COLUMN status customer_status NOT NULL DEFAULT 'active';

CREATE INDEX idx_customers_status ON customers (status);
