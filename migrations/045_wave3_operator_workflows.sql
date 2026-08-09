-- Wave 3 operator workflows (#69-#78).
-- This is deliberately forward-only.  Migrations 001-044 are immutable.

-- Durable screening runs/results.  The original screening_results columns are
-- retained for the 12-month compatibility window; these columns add the
-- workflow identity, suppression and optimistic-lock state.
CREATE TABLE IF NOT EXISTS screening_runs (
    id                  UUID PRIMARY KEY,
    customer_id         UUID NOT NULL REFERENCES customers(id),
    list_ids            JSONB NOT NULL DEFAULT '[]'::jsonb,
    config_digests       JSONB NOT NULL DEFAULT '{}'::jsonb,
    status              TEXT NOT NULL DEFAULT 'running'
                        CHECK (status IN ('running','completed','failed','partial')),
    result_count        INTEGER NOT NULL DEFAULT 0,
    error               TEXT,
    actor               TEXT NOT NULL DEFAULT '',
    started_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_screening_runs_customer_created
    ON screening_runs(customer_id, created_at DESC, id DESC);

ALTER TABLE screening_results ADD COLUMN IF NOT EXISTS run_id UUID REFERENCES screening_runs(id);
ALTER TABLE screening_results ADD COLUMN IF NOT EXISTS suppressed BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE screening_results ADD COLUMN IF NOT EXISTS suppression_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE screening_results ADD COLUMN IF NOT EXISTS match_evidence JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE screening_results ADD COLUMN IF NOT EXISTS case_id TEXT REFERENCES cases(id);
ALTER TABLE screening_results ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE screening_results ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE INDEX IF NOT EXISTS idx_screening_results_run_id ON screening_results(run_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_screening_results_customer_status ON screening_results(customer_id, status, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS screening_result_history (
    id                   UUID PRIMARY KEY,
    screening_result_id  TEXT NOT NULL REFERENCES screening_results(id),
    from_status          TEXT NOT NULL,
    to_status            TEXT NOT NULL,
    rationale            TEXT NOT NULL DEFAULT '',
    actor                TEXT NOT NULL DEFAULT '',
    version              INTEGER NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_screening_result_history_result
    ON screening_result_history(screening_result_id, created_at, id);

-- Import attempts are operational state, not evidence.  Keeping the safe
-- diagnostic bounded and separate from the immutable list snapshot prevents
-- an outage from making the source directory look complete.
ALTER TABLE screening_list_failures ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ;
ALTER TABLE screening_list_failures ADD COLUMN IF NOT EXISTS last_failure_at TIMESTAMPTZ;
ALTER TABLE screening_list_failures ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';

-- Backtest request metadata is separate from the existing job result schema so
-- old workers can still read jobs created before Wave 3.
CREATE TABLE IF NOT EXISTS backtest_job_metadata (
    job_id              UUID PRIMARY KEY REFERENCES backtest_jobs(id),
    rationale           TEXT NOT NULL DEFAULT '',
    cohort_preview      JSONB NOT NULL DEFAULT '{}'::jsonb,
    baseline_snapshot   JSONB NOT NULL DEFAULT '{}'::jsonb,
    candidate_snapshot  JSONB NOT NULL DEFAULT '{}'::jsonb,
    rerun_of            UUID REFERENCES backtest_jobs(id),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A target manifest is the immutable resolved population.  Confirmation only
-- changes operational status/timestamps; the selected IDs and filter are
-- never recomputed from live customer data.
CREATE TABLE IF NOT EXISTS target_manifests (
    id                  UUID PRIMARY KEY,
    operation           TEXT NOT NULL,
    target_mode         TEXT NOT NULL CHECK (target_mode IN ('selected','filter','all')),
    customer_ids        JSONB NOT NULL DEFAULT '[]'::jsonb,
    filter              JSONB NOT NULL DEFAULT '{}'::jsonb,
    sample_customer_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    target_count        INTEGER NOT NULL DEFAULT 0,
    criteria            TEXT NOT NULL DEFAULT '',
    rule_set_id         TEXT NOT NULL DEFAULT '',
    rule_set_version    INTEGER NOT NULL DEFAULT 0,
    config_digests      JSONB NOT NULL DEFAULT '{}'::jsonb,
    token_hash          TEXT NOT NULL UNIQUE,
    idempotency_key     TEXT,
    rationale           TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'preview'
                        CHECK (status IN ('preview','confirmed','expired','consumed')),
    version             INTEGER NOT NULL DEFAULT 1,
    expires_at          TIMESTAMPTZ NOT NULL,
    created_by          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    confirmed_at        TIMESTAMPTZ,
    run_id              UUID REFERENCES batch_runs(id),
    UNIQUE(operation, idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_target_manifests_status_expiry
    ON target_manifests(status, expires_at);

ALTER TABLE batch_runs DROP CONSTRAINT IF EXISTS batch_runs_status_check;
ALTER TABLE batch_runs ADD CONSTRAINT batch_runs_status_check
    CHECK (status IN ('running','completed','failed','partial','cancelled'));
ALTER TABLE batch_runs ADD COLUMN IF NOT EXISTS operation TEXT NOT NULL DEFAULT 'tm_batch_evaluation';
ALTER TABLE batch_runs ADD COLUMN IF NOT EXISTS parameters JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE batch_runs ADD COLUMN IF NOT EXISTS target_manifest_id UUID REFERENCES target_manifests(id);
ALTER TABLE batch_runs ADD COLUMN IF NOT EXISTS config_digests JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE batch_runs ADD COLUMN IF NOT EXISTS actor TEXT NOT NULL DEFAULT '';
ALTER TABLE batch_runs ADD COLUMN IF NOT EXISTS result_counts JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE batch_runs ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT '';
ALTER TABLE batch_runs ADD COLUMN IF NOT EXISTS rerun_of UUID REFERENCES batch_runs(id);
ALTER TABLE batch_runs ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE INDEX IF NOT EXISTS idx_batch_runs_operation_created
    ON batch_runs(operation, started_at DESC, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_batch_runs_operation_idempotency
    ON batch_runs(operation, (parameters->>'idempotency_key'))
    WHERE NULLIF(parameters->>'idempotency_key', '') IS NOT NULL;

ALTER TABLE pending_evaluations ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ;
ALTER TABLE pending_evaluations ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ;
ALTER TABLE pending_evaluations ADD COLUMN IF NOT EXISTS escalated_at TIMESTAMPTZ;
ALTER TABLE pending_evaluations ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;
CREATE TABLE IF NOT EXISTS pending_evaluation_history (
    id                    UUID PRIMARY KEY,
    pending_evaluation_id UUID NOT NULL REFERENCES pending_evaluations(id),
    from_status           TEXT NOT NULL,
    to_status             TEXT NOT NULL,
    action                TEXT NOT NULL,
    reason                TEXT NOT NULL DEFAULT '',
    actor                 TEXT NOT NULL DEFAULT '',
    retry_count           INTEGER NOT NULL DEFAULT 0,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_pending_evaluation_history_item
    ON pending_evaluation_history(pending_evaluation_id, created_at, id);

-- KYC identity changes and explainable CDD additions are append-only evidence
-- alongside the existing customer/score rows.
CREATE TABLE IF NOT EXISTS customer_identity_history (
    id              UUID PRIMARY KEY,
    customer_id     UUID NOT NULL REFERENCES customers(id),
    changed_fields  JSONB NOT NULL DEFAULT '{}'::jsonb,
    actor           TEXT NOT NULL DEFAULT '',
    rationale       TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_customer_identity_history_customer
    ON customer_identity_history(customer_id, created_at, id);
ALTER TABLE customer_score_history ADD COLUMN IF NOT EXISTS rationale TEXT NOT NULL DEFAULT '';
ALTER TABLE customer_score_history ADD COLUMN IF NOT EXISTS actor TEXT NOT NULL DEFAULT '';
ALTER TABLE customer_score_history ADD COLUMN IF NOT EXISTS override_evidence JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE customer_score_history ADD COLUMN IF NOT EXISTS factor_explanations JSONB NOT NULL DEFAULT '[]'::jsonb;

-- Travel Rule parity is additive and nullable so legacy rows continue to read
-- as "unknown/not yet assessed" rather than being treated as complete.
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS travel_rule_applicable BOOLEAN;
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS travel_rule_evidence JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS travel_rule_not_applicable_reason TEXT NOT NULL DEFAULT '';

-- Append-only database protection for Wave 3 evidence streams.
DROP TRIGGER IF EXISTS screening_result_history_append_only ON screening_result_history;
CREATE TRIGGER screening_result_history_append_only
    BEFORE UPDATE OR DELETE ON screening_result_history
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_append_only_mutation();
DROP TRIGGER IF EXISTS pending_evaluation_history_append_only ON pending_evaluation_history;
CREATE TRIGGER pending_evaluation_history_append_only
    BEFORE UPDATE OR DELETE ON pending_evaluation_history
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_append_only_mutation();
DROP TRIGGER IF EXISTS customer_identity_history_append_only ON customer_identity_history;
CREATE TRIGGER customer_identity_history_append_only
    BEFORE UPDATE OR DELETE ON customer_identity_history
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_append_only_mutation();
