-- Server-side Travel Rule assessment.
--
-- travel_rule_applicable was taken verbatim from whatever the client asserted,
-- and a transaction that arrived without a counterparty block left it NULL
-- forever. The UI rendered those as "legacy", which made a transaction the
-- system had never assessed indistinguishable from one predating the field.
--
-- The server now assesses every transaction against the travel_rule policy and
-- records the verdict: which policy version decided, on what threshold, with
-- what missing evidence, and whether the client's own assertion disagreed.
-- Under the default assertion_authority: client the client's claim is not
-- overwritten -- both are kept, and the disagreement is the finding.

ALTER TABLE transactions ADD COLUMN IF NOT EXISTS travel_rule_assessment JSONB;
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS travel_rule_status TEXT;
-- A closed reason code alongside the existing free-text reason. The free text
-- stays and keeps being accepted; a code is what makes "why was this exempt"
-- answerable across a whole book rather than one transaction at a time.
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS travel_rule_not_applicable_reason_code TEXT;

-- The review queries are "which transactions are missing Travel Rule
-- evidence" and "where did the client and the server disagree". Both are
-- sparse, so both indexes are partial.
CREATE INDEX IF NOT EXISTS idx_transactions_travel_rule_incomplete
    ON transactions (executed_at DESC, id DESC)
    WHERE travel_rule_status = 'incomplete';
CREATE INDEX IF NOT EXISTS idx_transactions_travel_rule_conflict
    ON transactions (executed_at DESC, id DESC)
    WHERE (travel_rule_assessment->>'conflict') = 'true';

-- Existing rows keep NULL. That is not "not applicable" and not "unknown": it
-- is the absence of an assessment, because these transactions were accepted
-- before the policy existed. There is deliberately no backfill -- assessing a
-- historical transaction against today's policy would manufacture a verdict
-- nobody made at the time.
