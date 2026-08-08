-- Protect the evidence inventory from in-place rewrites. Corrections are new
-- versioned rows plus a case_events entry, so the prior reviewed artifact
-- remains available to a reviewer.
ALTER TABLE case_evidence ADD COLUMN IF NOT EXISTS root_id TEXT;
ALTER TABLE case_evidence ADD COLUMN IF NOT EXISTS supersedes_id TEXT;
UPDATE case_evidence SET root_id = id WHERE root_id IS NULL;
ALTER TABLE case_evidence ALTER COLUMN root_id SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_case_evidence_root_version
    ON case_evidence (root_id, version);
CREATE INDEX IF NOT EXISTS idx_case_evidence_supersedes
    ON case_evidence (supersedes_id);
DROP TRIGGER IF EXISTS case_evidence_append_only ON case_evidence;
CREATE TRIGGER case_evidence_append_only
    BEFORE UPDATE OR DELETE ON case_evidence
    FOR EACH ROW EXECUTE FUNCTION merlon_reject_append_only_mutation();
