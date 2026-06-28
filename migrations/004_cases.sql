CREATE TABLE IF NOT EXISTS cases (
    id          TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL REFERENCES customers(id),
    alert_ids   TEXT[] NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'investigating', 'escalated', 'closed')),
    priority    TEXT NOT NULL DEFAULT 'medium' CHECK (priority IN ('low', 'medium', 'high')),
    assigned_to TEXT,
    summary     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at   TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS case_notes (
    id        TEXT PRIMARY KEY,
    case_id   TEXT NOT NULL REFERENCES cases(id),
    author    TEXT NOT NULL DEFAULT '',
    content   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cases_customer_id ON cases(customer_id);
CREATE INDEX idx_cases_status ON cases(status);
CREATE INDEX idx_case_notes_case_id ON case_notes(case_id);
