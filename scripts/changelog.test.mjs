// Tests for scripts/changelog.mjs.
//
// The release workflow builds every published release note from this parser,
// so a regression here either blocks a release or publishes the wrong notes.
// The cases below pin the one rule that is easy to get wrong: production tags
// resolve only to their own section, pre-release tags fall back.
//
// Run with: node --test scripts/

import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  findReleaseNotesSection,
  findSection,
  parseChangelog,
} from "./changelog.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const CLI = path.join(__dirname, "changelog.mjs");

const CHANGELOG = `# Changelog

## [Unreleased]

Work on main.

## [1.2.0] - 2026-08-01

The 1.2.0 release.

## [1.1.0] - 2026-07-01

The 1.1.0 release.
`;

test("parseChangelog returns sections in document order", () => {
  const versions = parseChangelog(CHANGELOG).map((s) => s.version);
  assert.deepEqual(versions, ["Unreleased", "1.2.0", "1.1.0"]);
});

test("findSection matches with or without a leading v", () => {
  assert.equal(findSection(CHANGELOG, "1.2.0").body, "The 1.2.0 release.");
  assert.equal(findSection(CHANGELOG, "v1.2.0").body, "The 1.2.0 release.");
  assert.equal(findSection(CHANGELOG, "9.9.9"), null);
});

test("a production tag resolves to its own section", () => {
  const resolved = findReleaseNotesSection(CHANGELOG, "v1.2.0");
  assert.equal(resolved.matched, "exact");
  assert.equal(resolved.section.body, "The 1.2.0 release.");
});

// The load-bearing case: if a production tag ever fell back, the gate that
// keeps CHANGELOG.md honest would stop gating anything.
test("a production tag never falls back", () => {
  assert.equal(findReleaseNotesSection(CHANGELOG, "v1.3.0"), null);
});

test("a pre-release tag prefers its own section when one exists", () => {
  const withRC = CHANGELOG.replace(
    "## [1.2.0] - 2026-08-01",
    "## [1.3.0-rc.1]\n\nThe first 1.3.0 candidate.\n\n## [1.2.0] - 2026-08-01"
  );
  const resolved = findReleaseNotesSection(withRC, "v1.3.0-rc.1");
  assert.equal(resolved.matched, "exact");
  assert.equal(resolved.section.body, "The first 1.3.0 candidate.");
});

test("a pre-release tag falls back to the release it is a candidate for", () => {
  const resolved = findReleaseNotesSection(CHANGELOG, "v1.2.0-rc.3");
  assert.equal(resolved.matched, "base");
  assert.equal(resolved.section.body, "The 1.2.0 release.");
});

test("a pre-release tag falls back to Unreleased when the base has no section", () => {
  const resolved = findReleaseNotesSection(CHANGELOG, "v2.0.0-beta.1");
  assert.equal(resolved.matched, "unreleased");
  assert.equal(resolved.section.body, "Work on main.");
});

test("every pre-release suffix the release gate accepts is recognized", () => {
  for (const tag of ["v2.0.0-alpha.0", "v2.0.0-beta.12", "v2.0.0-rc.1"]) {
    assert.equal(findReleaseNotesSection(CHANGELOG, tag).matched, "unreleased", tag);
  }
});

test("a pre-release tag with nothing to fall back to resolves to null", () => {
  const noUnreleased = "# Changelog\n\n## [1.1.0] - 2026-07-01\n\nThe 1.1.0 release.\n";
  assert.equal(findReleaseNotesSection(noUnreleased, "v2.0.0-rc.1"), null);
});

// The CLI is what the release workflow actually invokes, against the real
// CHANGELOG.md. Exit codes are the contract; asserting them here is what
// catches "the parser is fine but the release still fails".
function runCLI(...args) {
  try {
    const stdout = execFileSync(process.execPath, [CLI, ...args], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
    return { code: 0, stdout };
  } catch (err) {
    return { code: err.status, stdout: err.stdout ?? "" };
  }
}

test("the CLI resolves a pre-release tag against the repository CHANGELOG", () => {
  const { code, stdout } = runCLI("v0.1.0-rc.1");
  assert.equal(code, 0);
  assert.ok(stdout.trim().length > 0, "release notes must not be empty");
});

test("the CLI rejects a production tag with no section", () => {
  assert.equal(runCLI("v99.99.99").code, 1);
});

test("the CLI rejects a missing argument", () => {
  assert.equal(runCLI().code, 2);
});
