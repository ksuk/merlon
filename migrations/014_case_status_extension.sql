-- Extends cases.status with new/reopened/str_filed (case-management.md
-- §ケースのステータス遷移). Additive only: existing values (open,
-- investigating, escalated, closed) are kept for Contract Stability; "open"
-- remains valid and is treated as an alias of "new" by ValidCaseStatusTransition.
ALTER TABLE cases DROP CONSTRAINT IF EXISTS cases_status_check;
ALTER TABLE cases ADD CONSTRAINT cases_status_check
    CHECK (status IN ('open', 'new', 'investigating', 'escalated', 'closed', 'reopened', 'str_filed'));

ALTER TABLE cases ADD COLUMN IF NOT EXISTS reopen_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE cases ADD COLUMN IF NOT EXISTS related_case_ids TEXT[] NOT NULL DEFAULT '{}';

ALTER TABLE cases ALTER COLUMN status SET DEFAULT 'new';
