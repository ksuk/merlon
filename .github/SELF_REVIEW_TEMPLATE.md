# Self-review record

Post this as a comment on your pull request before merging. The
`Governance Required` check reads it and stays red until it is present and
bound to the current head commit — see `.github/workflows/governance.yml` and
`scripts/check-self-review.mjs`.

This repository has one maintainer, so no pull request is approved by a second
person. This record is what stands in its place. It is a compensating control
and is never described as an approval. ADR-0016 records why.

Pushing new commits invalidates the record; post a fresh one for the new head.
Bot pull requests are exempt — a bot cannot write one.

The checker requires the `## Self-review record` heading, a `**Head SHA:**`
line matching the pull request head, and all five `###` sections present and
non-empty. Copy the block below verbatim and fill it in.

---

```markdown
## Self-review record

**Head SHA:** <full or 7+ character sha of the current head>

### Intent
<What this change is for, in the terms of the issue it closes.>

### Blast radius
<What breaks if this is wrong. Name the data, the endpoint, the migration, the
gate. "Nothing" is an answer that needs a reason.>

### Rollback
<The exact revert path. For migrations, state forward-only handling explicitly.>

### Automated gates passed
<Required checks, plus anything path-specific: migration replay, integration,
docs, CodeQL, license, SBOM.>

### Not verified
<The honest part, and the reason the checker refuses an empty one. What no gate
covered and no second reader checked: manual paths not exercised, assumptions
taken from the issue, behaviour only observed locally.>
```
