# Merlon demo seed

This directory holds the PH7 recorded-demo dataset: ~1,000 synthetic
customers plus their accounts, CDD score history, transactions, alerts,
cases, screening hits, rule definitions, and audit log entries.

## Generating the dataset

The dataset itself (`*.json` below) is **not committed** (see `.gitignore`;
it's ~15-20MB). Generate it with:

```sh
make demogen
```

which runs `api/cmd/merlon-demogen` with its default deterministic seed
(`20260701`) and anchor date (`2026-07-01`), writing:

- `customers.json`, `accounts.json`, `score_history.json`,
  `transactions.json`, `alerts.json`, `cases.json`, `case_notes.json`,
  `screening_results.json`, `rule_definitions.json`, `audit_logs.json` —
  generated, gitignored.
- `STORY_IDS.md` — **committed**. The fixed IDs of the six scripted "story"
  customers (structuring, high-frequency/mule, high-risk-country transfer,
  rapid movement/pass-through, dormant reactivation, and the compound case)
  and the screening-hit customers, for the demo tour (T5'/T6') to link to
  directly. Each row shows both a human-readable **label** (this generator's
  internal name, e.g. `demo-story-04`) and the **UUID** that is the entity's
  actual primary key (`api/internal/demogen/ids.go`'s `uuidFor(label)`, a
  deterministic RFC 4122 v5 UUID) — use the UUID to build a working URL.
  Regenerating the dataset reproduces the same label and therefore the same
  UUID every time.
- `screening_lists/*.yaml` — **committed**. Small, fully synthetic
  sanctions/PEP lists (`DEMO-SANCTIONS-*`, `DEMO-PEP-*`); see
  `screening_lists/README.md`.

`api/cmd/merlon-demogen`'s Docker build target (see below) runs the same
generator, so a from-source `make demogen` and a Docker image build produce
byte-identical data.

## Loading the dataset

`api/internal/seed` loads this dataset at API startup when both are set:

- `MERLON_SEED=true`
- `MERLON_DEMO_DATA_DIR=<this directory>` (or wherever you ran
  `make demogen -out ...` / the Docker image's baked-in copy)

Loading goes through the same repository interfaces
(`api/internal/domain`) regardless of backend — PostgreSQL or the
in-memory store behave identically. IDs from the JSON files are preserved
as-is (they're already the deterministic UUIDs demogen derived from each
entity's label), so the UUID column in `STORY_IDS.md` resolves directly via
the HTTP API (e.g. `GET /api/v1/customers/{uuid}`) against either backend.

If `MERLON_DEMO_DATA_DIR` is unset, or the directory is missing/incomplete,
the API falls back to the small 5-customer hardcoded sample
(`api/internal/seed`'s pre-PH7 behavior) — this is what most `go test`/local
dev runs without `make demogen` still get. A directory that exists but has a
**corrupt** file (valid path, invalid content) is treated differently: the
seed fails loudly instead of silently falling back, so a broken dataset is
never mistaken for a clean 5-customer start.

PostgreSQL demo loading is transactional: a corrupt file or insert/constraint
failure rolls back the dataset and its `seed_state` completion marker, so the
next startup can retry safely. A successful load records the dataset
provenance; subsequent restarts reuse it for the synthetic-data indicator
without re-inserting rows. An existing database without a completion marker
is treated as operator-owned and is never deleted automatically. If it is a
volume left partially seeded by an older release, stop the demo stack and
recreate only that demo volume (`docker compose -f docker-compose.demo.yml
down -v`) before starting it again.

`docker-compose.demo.yml` builds the `api` service from `api/Dockerfile`'s
`demo` target, which layers the generated dataset into the image at
`/app/demo-data` (on top of the standard runtime image, with no other
change), and sets `MERLON_DEMO_DATA_DIR=/app/demo-data` accordingly.

## Why the dataset's IDs are UUIDs, not the labels you see in the source

`customers`, `transactions`, `alerts`, `accounts`, `rule_definitions`, and
`customer_score_history` have `UUID` primary key columns
(migrations/001, 002, 004, 011, 020), which reject arbitrary strings —
`cases`, `case_notes`, and `screening_results` are `TEXT` and have no such
constraint, but get UUIDs too for a uniform ID scheme across the dataset.
`api/internal/demogen`'s own source and self-checks work with
human-readable labels throughout generation (e.g. `demo-story-01`,
`demo-txn-0000001` — see `cases.go`, `story.go`, `storyids.go`); the very
last step of `Generate` (`remap.go`'s `remapIDsToUUIDs`) rewrites every
entity's ID and cross-reference to `uuidFor(label)`, a deterministic
RFC 4122 v5 UUID (`ids.go`). The same label always yields the same UUID, so
regenerating the dataset reproduces byte-identical output, and
`STORY_IDS.md` documents the label/UUID mapping for every fixed demo-tour ID.

Confirmed end-to-end against a real PostgreSQL instance (not just the
in-memory store): all ten JSON files load through the same
`api/internal/seed` loader, and the fixed story-04 customer/alert/case UUIDs
in `STORY_IDS.md` resolve via the HTTP API. Established on PostgreSQL 16 and
repeated on 18.4 when the pin moved.
