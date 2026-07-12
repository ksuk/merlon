-- Full content digest for the CDD configuration used to calculate a score.
-- Existing history predates digest capture and therefore remains NULL rather
-- than receiving an invented value.
ALTER TABLE customer_score_history
    ADD COLUMN IF NOT EXISTS rule_set_sha256 CHAR(64);

ALTER TABLE customer_score_history
    ADD CONSTRAINT customer_score_history_rule_set_sha256_format
    CHECK (rule_set_sha256 IS NULL OR rule_set_sha256 ~ '^[0-9a-f]{64}$');
