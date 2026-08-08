-- Wave 2 STR correction/amendment lineage. Reports are never overwritten;
-- each amendment points to the durable report it corrects and/or supersedes.
ALTER TABLE str_reports ADD COLUMN IF NOT EXISTS corrects_report_id TEXT REFERENCES str_reports(id);
ALTER TABLE str_reports ADD COLUMN IF NOT EXISTS supersedes_report_id TEXT REFERENCES str_reports(id);
CREATE INDEX IF NOT EXISTS idx_str_reports_corrects_report_id ON str_reports (corrects_report_id);
CREATE INDEX IF NOT EXISTS idx_str_reports_supersedes_report_id ON str_reports (supersedes_report_id);
