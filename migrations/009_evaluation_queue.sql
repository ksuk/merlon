-- M4.1: Fail-Alert queueing for engine outages (OPS-005)

CREATE TYPE pending_evaluation_status AS ENUM ('PENDING_REVIEW', 'PROCESSING', 'RESOLVED', 'FAILED');

CREATE TABLE pending_evaluations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id),
    transaction_ids UUID[] NOT NULL DEFAULT '{}',
    status          pending_evaluation_status NOT NULL DEFAULT 'PENDING_REVIEW',
    reason          TEXT NOT NULL,
    batch_run_id    UUID,
    retry_count     INTEGER NOT NULL DEFAULT 0,
    resolved_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_pending_evaluations_status ON pending_evaluations (status);
CREATE INDEX idx_pending_evaluations_customer ON pending_evaluations (customer_id);
CREATE INDEX idx_pending_evaluations_batch_run ON pending_evaluations (batch_run_id);
