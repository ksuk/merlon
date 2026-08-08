-- Account for targets a bulk run cannot act on.
--
-- target_count was the number of customers left in the manifest after
-- resolution, with no record of how many had been dropped. An operator
-- confirming "1,204 customers" could not tell that from a selection of 9,000
-- that was mostly closed accounts, and the manifest -- which is the pinned
-- evidence of what was approved -- did not carry the difference either.
--
-- excluded_reasons is keyed by customer status rather than a fixed column per
-- reason, so a new eligibility rule does not need a schema change to be
-- recorded.
--
-- expected_side_effects is deliberately NOT stored. It describes what the
-- operation does today, not a fact about this manifest; freezing it here would
-- let a manifest keep asserting behaviour the code no longer has.

ALTER TABLE target_manifests
    ADD COLUMN IF NOT EXISTS excluded_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE target_manifests
    ADD COLUMN IF NOT EXISTS excluded_reasons JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Existing rows keep 0 and '{}': the exclusion was never computed for them, and
-- back-filling a count from today's rules would attribute a decision to a
-- preview that never made it.
