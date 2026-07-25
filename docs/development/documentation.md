---
title: Documentation Guide
sidebar_position: 12
---

# Documentation Guide

This guide explains how Merlon's documentation is organized, translated, and
checked, and how to run the checks locally.

## Canonical language and layout

English is the canonical language for all hand-written documentation, under
`docs/` in the repository root. Every other locale is a translation of that
source.

- `docs/decisions/**` (Architecture Decision Records) is excluded from the
  built site entirely and is written in Japanese; it is not translated and
  the documentation checks skip it.
- `docs/standards/**` (internal audit and review standards) is likewise
  excluded from the built site and written in Japanese; it is not translated
  and the documentation checks skip it.
- `docs/api/**` is generated reference output (OpenAPI and JSON
  Schema reference pages) and is gitignored. Never edit it directly — it is
  overwritten by `make generate-openapi` and the website's schema-doc
  generator.
- `docs/release-notes.md` is generated from the repository-root `CHANGELOG.md`
  by `website/scripts/generate-changelog-page.mjs` and is likewise gitignored.
  Edit `CHANGELOG.md` instead; it is also the source of every GitHub release's
  notes.

## Adding a page to the sidebar

The sidebar is defined explicitly in `website/sidebars.ts`, grouped by
audience rather than by directory. **A new page does not appear on the site
until it is listed there**, and `sidebar_position` frontmatter is ignored
outside the generated `api/schema` subtree. This is deliberate: adding a page
means deciding who it is for. Add the doc ID to the category that matches its
reader, and add a `sidebar.docsSidebar.category.<label>` entry to each
locale's `current.json` if you introduce a new category.

## ja translations

Japanese translations live at
`website/i18n/ja/docusaurus-plugin-content-docs/current/`, mirroring the
relative path of each English doc under `docs/`. The reference generators
(OpenAPI, schema) emit both the `en` and `ja` reference pages directly
from their source comments/schemas, so `docs/api/**` and its ja counterpart
are kept in sync automatically and are out of scope for translation checks.

Any hand-written English doc that does not yet have a real translation is
filled in at build time with an English fallback copy by
`website/scripts/sync-i18n-fallback.mjs`, so relative links between pages
never 404 in the ja locale. Fallback copies are recorded in
`website/.i18n-fallback-manifest.json` and are **not** real translations —
the checks below treat them the same as a missing translation.

## Translation workflow rule

Any pull request that changes a hand-written English doc under `docs/` (i.e.
not `docs/api/**` or `docs/decisions/**`) must do one of:

1. Update the corresponding ja file under
   `website/i18n/ja/docusaurus-plugin-content-docs/current/` in the same PR, or
2. Add an entry to `website/docs-lint/i18n-freshness-acks.json` acknowledging
   that the ja translation is intentionally not yet updated for this en
   revision (see below).

This keeps translations from silently rotting behind the English source.

## The five checks

`node website/scripts/checks/run-all.mjs` runs all five checks below and
prints a pass/fail table; it exits non-zero if any check fails. Run it with
`make docs-check`.

### 1. `check-en-language`

Fails if Japanese characters appear anywhere in a hand-written English doc.
Allowed exceptions (e.g. Japanese statute
names quoted intentionally in prose) are listed in
`website/docs-lint/jp-allowlist.json`, an array of exact-substring entries:

```json
[{ "file": "docs/compliance/data-retention.md", "text": "<exact substring to allow>" }]
```

**Stale-entry rule**: if an allowlist entry's `text` no longer appears in its
`file`, the check fails — the allowlist must always describe text that
actually exists, or it silently stops guarding anything.

### 2. `check-title-style`

For hand-written docs and `_category_.json` labels: frontmatter `title` must
exist and, if the doc has an H1, must equal it exactly. Titles, frontmatter
`sidebar_label`s, and category `label`s must be Title Case (every word
capitalized except minor words — a, an, the, and, or, nor, but, for, of, in,
on, to, at, by, with, vs, via — unless first or last). Proper nouns listed in
`website/docs-lint/proper-nouns.json` (OpenAPI, API, CDD, JWT, ...) must
appear exactly as spelled there.

### 3. `check-i18n-parity`

Every hand-written English doc must have a ja counterpart at the same
relative path that is a **real** translation: it must exist, must not be
byte-identical to the English source, must not be a fallback copy per the
manifest, and its first H1 must contain Japanese characters. Exceptions
(docs intentionally left untranslated) go in
`website/docs-lint/i18n-exceptions.json`, an array of `docs/`-relative paths.

**Stale-entry rule**: an exception for a path that is no longer a
hand-written English doc fails the check.

### 4. `check-i18n-freshness`

For each real en/ja translation pair, the ja file must not be older than the
English source, compared by last-commit time (`git log -1 --format=%ct`). If
the English file has uncommitted local changes, it is always treated as
newer than any committed ja translation. A brand-new pair where both sides
are uncommitted (added together in one PR) passes.

An out-of-date pair can be acknowledged in
`website/docs-lint/i18n-freshness-acks.json`, mapping the `docs/`-relative
path to the English file's current HEAD commit hash
(`git log -1 --format=%H -- <file>`):

```json
{ "getting-started.md": "a1b2c3d4..." }
```

If the English file has uncommitted local modifications there is no commit
hash to pin to; use a content-hash ack instead, prefixed with `sha256:`:

```json
{ "getting-started.md": "sha256:<sha256 hex digest of the en file content>" }
```

**Stale-entry rule**: an ack whose hash does not match the English file's
current HEAD hash (or, for `sha256:` acks, the current file content) fails
the check — it must be re-confirmed (or the translation updated) after any
further English change.

### 5. `check-ui-translations`

Checks that the Docusaurus UI-string translation files exist and are valid
JSON: `website/i18n/ja/code.json`,
`website/i18n/ja/docusaurus-plugin-content-docs/current.json`,
`website/i18n/ja/docusaurus-theme-classic/navbar.json`, and
`.../footer.json`. Every `message` value must contain Japanese characters, or
be entirely composed of tokens allowlisted in
`website/docs-lint/ui-string-allowlist.json` (brand names and acronyms like
GitHub, Merlon, REST API; numerals and punctuation never count against a
message).

## Running the checks

```bash
make docs-check
```

This is equivalent to `node website/scripts/checks/run-all.mjs` and requires
no npm install — the checks are dependency-free Node ESM scripts. CI runs
the same command in the "Docs checks" step of `docs-deploy.yml`, before the
site build, using full git history (`fetch-depth: 0`) so the freshness check
can compare commit timestamps.
