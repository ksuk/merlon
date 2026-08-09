-- Screening true-positive cases use the domain's explicit critical priority
-- and the canonical new status.  The original case migration pre-dates those
-- values, so extend its checks in a forward-only migration.
ALTER TABLE cases DROP CONSTRAINT IF EXISTS cases_status_check;
ALTER TABLE cases ADD CONSTRAINT cases_status_check
    CHECK (status IN ('open', 'new', 'investigating', 'escalated', 'closed', 'reopened', 'str_filed'));

ALTER TABLE cases DROP CONSTRAINT IF EXISTS cases_priority_check;
ALTER TABLE cases ADD CONSTRAINT cases_priority_check
    CHECK (priority IN ('low', 'medium', 'high', 'critical'));
