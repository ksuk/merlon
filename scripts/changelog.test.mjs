// Tests for scripts/changelog.mjs.
//
// The release workflow builds every published release note from this parser,
// so a regression here either blocks a release or publishes the wrong notes.
// The cases below pin the one rule that is easy to get wrong: a tag resolves
// only to its own section, and nothing falls back.
//
// Run with: node --test scripts/

import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { findSection, parseChangelog, readChangelog } from "./changelog.mjs";

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

// The load-bearing case: if a tag ever fell back, the gate that keeps
// CHANGELOG.md honest would stop gating anything.
test("a tag never falls back to a neighbouring section", () => {
  assert.equal(findSection(CHANGELOG, "v1.3.0"), null);
});

test("a tag never falls back to Unreleased", () => {
  assert.equal(findSection(CHANGELOG, "v2.0.0"), null);
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

test("the CLI resolves a section that exists in the repository CHANGELOG", () => {
  const [first] = parseChangelog(readChangelog());
  const { code, stdout } = runCLI(first.version);
  assert.equal(code, 0);
  assert.ok(stdout.trim().length > 0, "release notes must not be empty");
});

test("the CLI rejects a tag with no section", () => {
  assert.equal(runCLI("v99.99.99").code, 1);
});

// A pre-release tag has no section of its own and no longer falls back to
// anything, so it fails here as well as at the release workflow's SemVer gate.
// The project publishes one channel; both layers have to agree on that.
test("the CLI rejects a pre-release tag", () => {
  for (const tag of ["v0.1.0-rc.1", "v0.1.0-beta.1", "v0.1.0-alpha.0"]) {
    assert.equal(runCLI(tag).code, 1, tag);
  }
});

test("the CLI rejects a missing argument", () => {
  assert.equal(runCLI().code, 2);
});
