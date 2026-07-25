#!/usr/bin/env node
// CHANGELOG.md section parsing, shared by the release workflow and the docs
// site.
//
// The release workflow uses this to build a tag's release notes from
// CHANGELOG.md instead of from commit subjects. That is what keeps the
// changelog honest: if it is the source of the published release notes, a
// release cannot be cut without one, and it stops drifting from what shipped.
//
// The docs site uses the same parser to publish the changelog as a page, so
// the file stays the single source of truth rather than being duplicated.
//
// Library:
//   import { parseChangelog, findSection } from "./changelog.mjs";
//
// CLI (used by .github/workflows/release.yml):
//   node scripts/changelog.mjs v1.2.3 > notes.md
// Exits non-zero if the version has no section, so a release stops rather
// than publishing empty notes.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
export const CHANGELOG_PATH = path.join(__dirname, "..", "CHANGELOG.md");

/**
 * Split a Keep a Changelog document into its version sections.
 *
 * Recognizes `## [1.2.3] - 2026-08-01`, `## [1.2.3]`, `## [Unreleased]`, and
 * the same headings without brackets. Returns sections in document order:
 *   { version, heading, date, body }
 * `body` excludes the heading itself and is trimmed.
 */
export function parseChangelog(text) {
  const lines = text.split(/\r?\n/);
  const sections = [];
  let current = null;

  for (const line of lines) {
    // A level-2 heading starts a new version section. Deeper headings
    // (### Added) belong to the section body.
    const match = /^##\s+\[?([^\]\s]+)\]?\s*(?:[-–—]\s*(.+?))?\s*$/.exec(line);
    if (match) {
      if (current) sections.push(current);
      current = {
        version: match[1],
        heading: line.trim(),
        date: match[2] ? match[2].trim() : "",
        bodyLines: [],
      };
      continue;
    }
    if (current) current.bodyLines.push(line);
  }
  if (current) sections.push(current);

  return sections.map(({ bodyLines, ...rest }) => ({
    ...rest,
    body: bodyLines.join("\n").trim(),
  }));
}

/** Compare two version strings ignoring a leading `v`, case-insensitively. */
function versionMatches(sectionVersion, wanted) {
  const normalize = (v) => v.replace(/^v/i, "").toLowerCase();
  return normalize(sectionVersion) === normalize(wanted);
}

/**
 * Find the section for `version`, or return null. `version` may be given with
 * or without a leading `v` (`v1.2.3` and `1.2.3` both match `## [1.2.3]`).
 */
export function findSection(text, version) {
  return parseChangelog(text).find((s) => versionMatches(s.version, version)) ?? null;
}

/** Read CHANGELOG.md from the repository root. */
export function readChangelog(changelogPath = CHANGELOG_PATH) {
  return readFileSync(changelogPath, "utf8");
}

function main(argv) {
  const version = argv[0];
  if (!version) {
    console.error("usage: node scripts/changelog.mjs <version>");
    console.error("  e.g. node scripts/changelog.mjs v1.2.3");
    return 2;
  }

  const text = readChangelog();
  const section = findSection(text, version);

  if (!section) {
    const available = parseChangelog(text).map((s) => s.version).join(", ");
    console.error(
      `CHANGELOG.md has no section for ${version}.\n` +
        `Add a "## [${version.replace(/^v/i, "")}] - YYYY-MM-DD" section ` +
        `describing this release before tagging.\n` +
        `Sections present: ${available || "(none)"}`
    );
    return 1;
  }
  if (!section.body) {
    console.error(`CHANGELOG.md section for ${version} is empty.`);
    return 1;
  }

  process.stdout.write(`${section.body}\n`);
  return 0;
}

// Run as a CLI only when invoked directly, so importing this module is free
// of side effects.
if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  process.exit(main(process.argv.slice(2)));
}
