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

Reading the rulesets API needs repository Administration (read), which the
Actions `permissions:` block cannot grant to `GITHUB_TOKEN`. If the workflow
reports that it cannot read them, store a fine-grained PAT with that permission
as the `RULESET_READ_TOKEN` secret. The job fails rather than reporting a clean
comparison it never made.

```bash
for id in $(gh api repos/ksuk/merlon/rulesets --jq '.[].id'); do
  name=$(gh api "repos/ksuk/merlon/rulesets/$id" --jq '.name')
  gh api "repos/ksuk/merlon/rulesets/$id" \
    | jq -S 'del(._links, .node_id, .created_at, .updated_at, .current_user_can_bypass)' \
    > ".github/rulesets/${name}.json"
done
```

The deleted fields are per-request or per-viewer values that would produce a
diff on every export. Everything that determines what the ruleset enforces is
kept, including `enforcement`, `bypass_actors`, and the required check
contexts.

An unexplained diff on `bypass_actors`, `enforcement`, or
`required_status_checks` is the case this baseline exists to catch. Investigate
before restoring it — a silently reverted diff destroys the evidence of what
happened.

This baseline is public. `bypass_actors` and `required_reviewers` are empty
today; if either becomes populated, the export will publish the actor and team
identifiers it contains. Check the diff before committing it. The export needs
only read access to repository administration — do not run it with a token
scoped more broadly than that.

The drift job clears this directory of `*.json` before exporting, so a ruleset
deleted from the live configuration appears as a deleted file. Exporting over
the existing files would have left the most serious weakening — the protection
removed outright — as the one case producing no diff at all.

What the baseline cannot detect is a change made to the live configuration and
to this directory in the same act. That limit is stated in
[repository governance](../../docs/development/repository-governance.md) rather
than papered over.
