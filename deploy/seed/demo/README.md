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
  directly. Regenerating the dataset reproduces these same IDs.
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
as-is wherever the destination column allows it, so the direct links in
`STORY_IDS.md` resolve via the HTTP API (e.g. `GET /api/v1/customers/demo-story-04`).

If `MERLON_DEMO_DATA_DIR` is unset, or the directory is missing/incomplete,
the API falls back to the small 5-customer hardcoded sample
(`api/internal/seed`'s pre-PH7 behavior) — this is what most `go test`/local
dev runs without `make demogen` still get. A directory that exists but has a
**corrupt** file (valid path, invalid content) is treated differently: the
seed fails loudly instead of silently falling back, so a broken dataset is
never mistaken for a clean 5-customer start.

Re-running with existing data present (e.g. `docker compose restart` without
`down -v`) skips seeding entirely rather than re-inserting rows.

`docker-compose.demo.yml` builds the `api` service from `api/Dockerfile`'s
`demo` target, which layers the generated dataset into the image at
`/app/demo-data` (on top of the standard runtime image, with no other
change), and sets `MERLON_DEMO_DATA_DIR=/app/demo-data` accordingly.

## Known limitation (PostgreSQL)

`customers`, `transactions`, `alerts`, `accounts`, `rule_definitions`, and
`customer_score_history` have `UUID` primary key columns. The dataset's
human-readable IDs (`demo-story-01`, `demo-alert-00008`, ...) are not valid
UUID literals, so loading this dataset against PostgreSQL currently fails on
the first `customers.json` row. The in-memory store has no such constraint
and loads the full dataset correctly (that's how `api/internal/seed`'s
loader is tested today). Fixing this for the PostgreSQL-backed
`docker-compose.demo.yml` topology needs either a migration moving those
columns to `TEXT` (matching `cases`/`case_notes`/`screening_results`, which
already are) or `demogen` emitting UUID-format primary IDs with the
human-readable label carried in a separate field — both out of scope for
the change that introduced this loader.
