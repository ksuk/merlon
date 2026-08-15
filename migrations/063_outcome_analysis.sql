-- P10 / issues #108-109: durable backtest outcome and known-matter coverage.
-- Outcome summaries are additive JSON on the existing job; detail and
-- coverage rows are independently paginated evidence streams.
ALTER TABLE backtest_jobs ADD COLUMN IF NOT EXISTS outcome_analysis jsonb;

CREATE TABLE IF NOT EXISTS backtest_outcome_details (
    id text PRIMARY KEY,
    job_id uuid NOT NULL REFERENCES backtest_jobs(id) ON DELETE CASCADE,
    variant text NOT NULL CHECK (variant IN ('baseline', 'candidate', 'delta')),
    candidate_id text NOT NULL,
    reference_id text NOT NULL DEFAULT '',
    customer_id uuid NOT NULL,
    scenario_id text NOT NULL DEFAULT '',
    label text NOT NULL CHECK (label IN ('TP', 'FP', 'unlabeled', 'unevaluable')),
    metric text NOT NULL DEFAULT '',
    score double precision NOT NULL DEFAULT 0,
    investigated boolean NOT NULL DEFAULT false,
    matched_alert_id text NOT NULL DEFAULT '',
    matched_case_id text NOT NULL DEFAULT '',
    matcher_version text NOT NULL,
    assumptions jsonb NOT NULL DEFAULT '[]',
    snapshot_at timestamptz NOT NULL,
    provenance jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(job_id, variant, candidate_id)
);
CREATE INDEX IF NOT EXISTS backtest_outcome_details_queue_idx
    ON backtest_outcome_details(job_id, variant, scenario_id, created_at, id);
CREATE INDEX IF NOT EXISTS backtest_outcome_details_label_idx
    ON backtest_outcome_details(job_id, label, created_at, id);

CREATE TABLE IF NOT EXISTS coverage_analyses (
    id text PRIMARY KEY,
    kind text NOT NULL CHECK (kind = 'comparison/known_matter_coverage'),
    status text NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed')),
    scenario_ids jsonb NOT NULL DEFAULT '[]',
    customer_ids jsonb NOT NULL DEFAULT '[]',
    snapshot_at timestamptz NOT NULL,
    matcher_version text NOT NULL,
    assumptions jsonb NOT NULL DEFAULT '[]',
    summary jsonb NOT NULL DEFAULT '{}',
    by_scenario jsonb NOT NULL DEFAULT '{}',
    error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS coverage_analyses_status_idx ON coverage_analyses(status, created_at, id);

CREATE TABLE IF NOT EXISTS coverage_analysis_matters (
    id text PRIMARY KEY,
    analysis_id text NOT NULL REFERENCES coverage_analyses(id) ON DELETE CASCADE,
    matter_id text NOT NULL,
    customer_id uuid NOT NULL,
    scenario_ids jsonb NOT NULL DEFAULT '[]',
    source text NOT NULL,
    label text NOT NULL CHECK (label IN ('TP', 'FP', 'unlabeled', 'unevaluable')),
    covered boolean NOT NULL DEFAULT false,
    unevaluable boolean NOT NULL DEFAULT false,
    matched_alert_id text NOT NULL DEFAULT '',
    matcher_version text NOT NULL,
    assumptions jsonb NOT NULL DEFAULT '[]',
    snapshot_at timestamptz NOT NULL,
    provenance jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(analysis_id, matter_id)
);
CREATE INDEX IF NOT EXISTS coverage_analysis_matters_page_idx
    ON coverage_analysis_matters(analysis_id, created_at, id);
CREATE INDEX IF NOT EXISTS coverage_analysis_matters_scenario_idx
    ON coverage_analysis_matters(analysis_id, scenario_ids);
