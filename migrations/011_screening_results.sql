-- WS-7: screening_results persistence (the screening workflow §7 data model /
-- §スクリーニングヒット後の調査ワークフロー). Additive only.
CREATE TABLE IF NOT EXISTS screening_results (
    id                    TEXT PRIMARY KEY,
    customer_id           UUID NOT NULL REFERENCES customers(id),
    list_id               TEXT NOT NULL,
    list_type             TEXT NOT NULL,
    entry_id              TEXT NOT NULL,
    matched_name          TEXT NOT NULL,
    similarity            DOUBLE PRECISION NOT NULL,
    status                TEXT NOT NULL DEFAULT 'NEW'
                           CHECK (status IN ('NEW', 'REVIEWING', 'TRUE_POSITIVE', 'FALSE_POSITIVE')),
    false_positive_reason TEXT,
    reviewed_by           TEXT,
    reviewed_at           TIMESTAMPTZ,
    screened_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_screening_results_customer_id ON screening_results(customer_id);
CREATE INDEX IF NOT EXISTS idx_screening_results_status ON screening_results(status);
CREATE INDEX IF NOT EXISTS idx_screening_results_screened_at ON screening_results(screened_at);
CREATE INDEX IF NOT EXISTS idx_screening_results_entry_id ON screening_results(entry_id);
