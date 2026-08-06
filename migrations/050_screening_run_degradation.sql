-- Degraded screening evidence.
--
-- A screening run made while a required watchlist source is stale, failed or
-- never imported still produces rows, and those rows are indistinguishable
-- from a run made against complete lists. A later reviewer cannot then tell a
-- genuine "no sanctions hit" from "no hit, because the sanctions list was
-- three weeks old".
--
-- The Fail-Alert principle says the run should not be blocked -- halting
-- screening during a provider outage trades a missed detection for a halted
-- operation -- so the degradation is recorded instead, on the run and on each
-- result it produced. Results carry their own copy because they are read and
-- exported independently of the run that produced them.

ALTER TABLE screening_runs    ADD COLUMN IF NOT EXISTS degraded         BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE screening_runs    ADD COLUMN IF NOT EXISTS degraded_sources TEXT[]  NOT NULL DEFAULT '{}';
ALTER TABLE screening_results ADD COLUMN IF NOT EXISTS degraded         BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE screening_results ADD COLUMN IF NOT EXISTS degraded_sources TEXT[]  NOT NULL DEFAULT '{}';

-- Degraded runs are the exception, so both indexes are partial: they stay
-- small while still serving the "what did we screen against a broken list"
-- review query directly.
CREATE INDEX IF NOT EXISTS idx_screening_runs_degraded    ON screening_runs (created_at DESC, id DESC) WHERE degraded;
CREATE INDEX IF NOT EXISTS idx_screening_results_degraded ON screening_results (created_at DESC, id DESC) WHERE degraded;

-- Suppression is likewise sparse: a repeat hit on a list entry already ruled a
-- false positive for the same customer. The queue view filters it out by
-- default, so index the suppressed rows for the explicit "show suppressed"
-- read rather than the whole table.
CREATE INDEX IF NOT EXISTS idx_screening_results_suppressed ON screening_results (created_at DESC, id DESC) WHERE suppressed;

-- Pre-existing rows keep degraded = FALSE. That is not a claim that their
-- sources were healthy; it is the absence of a claim either way, which is the
-- only honest value for a run made before this evidence was captured.
