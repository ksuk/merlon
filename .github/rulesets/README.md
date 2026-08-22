# Ruleset baseline

The committed state of the GitHub rulesets that protect `main` and release
tags. These files are the reference a drift check compares the live
configuration against.

The repository has one active maintainer, who is also the only Admin. Every
required check is therefore administered by the person it checks — a
compensating control, not separation of duties. Weakening a ruleset cannot be
prevented by a second pair of eyes here, so it is made visible instead: any
change to the live configuration that is not reflected in a committed diff to
this directory is drift, and drift is a finding.

See [Single-Maintainer Operating Mode](../../docs/development/repository-governance.md)
and ADR-0016.

## Files

| File | Ruleset | Applies to |
|---|---|---|
| `main-release-governance.json` | `main-release-governance` | `refs/heads/main` |
| `release-tag-governance.json` | `release-tag-governance` | `v*.*.*` tags |

## Refreshing after an intentional change

Ruleset changes are made by `scripts/configure-github-ruleset.sh --apply`, and
the baseline is refreshed in the same pull request.
`.github/workflows/ruleset-drift.yml` compares the two weekly and on
`workflow_dispatch`.

`--apply` refreshes this directory itself. To re-export without changing
anything:

```bash
REPO=ksuk/merlon bash scripts/ruleset-baseline.sh --export-all .github/rulesets
```

`scripts/ruleset-baseline.sh` is the only place that defines what a canonical
baseline looks like. The drift workflow, `configure-github-ruleset.sh`, and this
document are all callers. The filter used to be copied by hand into each of
them, which is a canonical form maintained in three places and therefore a
canonical form waiting to disagree with itself.

The stripped fields (`_links`, `node_id`, `created_at`, `updated_at`,
`current_user_can_bypass`) are per-request or per-viewer values that would
produce a diff on every export. Everything that determines what the ruleset
enforces is kept, including `enforcement`, `bypass_actors`, and the required
check contexts. `current_user_can_bypass` is asserted to be `never` at export
time, before it is stripped — it answers for the identity making the request,
so it is checked on every run rather than committed here.

## The token, and why this is not a 403

Reading administration fields needs **write access to the ruleset**, not read.
GitHub's wording is exact: "the `bypass_actors` property is only returned if the
user making the API request has write access to the ruleset." The Actions
`permissions:` block has no `administration` scope to grant at any level, so
`GITHUB_TOKEN` cannot see the field however it is configured.

That makes the obvious remedy worse than it looks. A `RULESET_READ_TOKEN` that
could actually read `bypass_actors` would need Administration **write** — a
credential able to delete every ruleset in this repository, stored as an Actions
secret, in order to verify that nobody can bypass those rulesets. The credential
would itself be a bypass mechanism, living in Actions secrets instead of in the
`bypass_actors` list. This project does not store one; see the verification
section below for what is checked instead.

The failure mode is quieter than a permission error, and it already happened
once. Listing and fetching rulesets **succeed** for a caller without ruleset
write access; `bypass_actors` is simply omitted from the response rather than
refused. On this job's first real run that produced a diff claiming the live
rulesets had changed, when nothing had. An omitted key is indistinguishable
from an empty one to anything comparing values.

**Never resolve that failure by committing the export, and never delete
`bypass_actors` from these files to make a comparison pass.** A baseline
without the key would report green forever while being structurally unable to
show a bypass actor being added — the single weakening this baseline exists to
catch. The export is now validated for the fields themselves before any
comparison is made, and `make verify-ruleset-baseline` fails on every pull
request if a baseline here is missing one.

That check is in the `release-dry-run` job of `.github/workflows/ci.yml`, which
feeds the already-required `CI Required` context. Its being required depends on
three properties of the current setup: no merge queue is configured, the `main`
ruleset forces changes through pull requests, and `bypass_actors` is empty. If
a merge queue is ever introduced, `ci.yml` needs a `merge_group` trigger or the
check stops running on the path that merges.

## What is verified, and what is not

The weekly job verifies everything `GITHUB_TOKEN` can actually see: `enforcement`,
`rules` including `required_status_checks`, `conditions`, and whether each ruleset
still exists. Those cover the weakening that happens by accident — `enforcement`
dropped to `evaluate` to unblock something and never restored, a required context
removed, a ruleset deleted.

`bypass_actors` is not in that set and cannot be, for the reason above. It is
verified instead when an administrator runs the export with their own token, which
`--apply` does as part of every intentional ruleset change. Adding an actor is a
deliberate act rather than a slip, so it is checked at the moments a person is
already acting rather than continuously.

`bypass_actors` has three states, and the reason to name them is that
collapsing any two is how this check goes blind:

| State | Meaning |
|---|---|
| `verified-empty` | an administrator's export saw `[]` |
| `verified-nonempty` | an administrator's export saw actors listed — a finding |
| `unverifiable` | the caller could not see the field |

The weekly job holds `unverifiable`, says so in its output, and prints the last
administrator-verified value for each ruleset with the commit that recorded it.
It compares a rendering that omits the field from **both** sides
(`--comparable`), so the omission cannot masquerade as agreement, and every
other field is still required — a response degraded in any other way still
fails rather than narrowing the comparison further.

`--check` refuses `--comparable`. A committed baseline is written by an
administrator and must carry `bypass_actors`; relaxing the audit would let a
degraded baseline pass the guard that exists to catch it.

An unexplained diff on `bypass_actors`, `enforcement`, or
`required_status_checks` is the case this baseline exists to catch. Investigate
before restoring it — a silently reverted diff destroys the evidence of what
happened.

This baseline is public. `bypass_actors` and `required_reviewers` are empty
today; if either becomes populated, the export will publish the actor and team
identifiers it contains — information GitHub deliberately withholds from callers
without ruleset write access. Check the diff before committing it, and decide
whether publishing those identifiers is acceptable rather than assuming it.

An export that includes `bypass_actors` necessarily runs with Administration
write. Run it interactively as an administrator; do not store that credential
anywhere a workflow can reach it.

The drift job clears this directory of `*.json` before exporting, so a ruleset
deleted from the live configuration appears as a deleted file. Exporting over
the existing files would have left the most serious weakening — the protection
removed outright — as the one case producing no diff at all.

What the baseline cannot detect is a change made to the live configuration and
to this directory in the same act. That limit is stated in
[repository governance](../../docs/development/repository-governance.md) rather
than papered over.
