// Tests for scripts/check-self-review.mjs.
//
// This gate is the whole of the compensating control that stands in for
// independent review, so the cases that matter are the ones where it could
// silently stop gating: a record that no longer matches the head commit, and a
// record whose "Not verified" section is blank.
//
// Run with: node --test scripts/

import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { checkComment, checkSelfReview } from "./check-self-review.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const CLI = path.join(__dirname, "check-self-review.mjs");

const HEAD = "abc1234def5678901234567890123456789abcde";
const AUTHOR = "ksuk";

/** A comment in the shape the workflow projects out of the GitHub API. */
function by(login, body) {
  return { body, user: { login } };
}

function record({ sha = HEAD, drop = null, blank = null } = {}) {
  const sections = {
    Intent: "Adds the governance gate.",
    "Blast radius": "Pull request merges; nothing at runtime.",
    Rollback: "Revert the workflow file.",
    "Automated gates passed": "CI Required, Security Required.",
    "Not verified": "The scheduled trigger has not fired yet.",
  };
  const body = Object.entries(sections)
    .filter(([title]) => title !== drop)
    .map(([title, text]) => `### ${title}\n\n${title === blank ? "" : text}\n`)
    .join("\n");
  return `## Self-review record\n\n**Head SHA:** ${sha}\n\n${body}`;
}

test("a complete record bound to the head commit passes", () => {
  assert.deepEqual(checkComment(record(), HEAD), { ok: true });
});

test("a short sha prefix is accepted", () => {
  assert.deepEqual(checkComment(record({ sha: HEAD.slice(0, 7) }), HEAD), { ok: true });
});

// The load-bearing case: if a stale record kept passing, a maintainer could
// post one record and then push anything.
test("a record for an older head commit is rejected", () => {
  const stale = checkComment(record({ sha: "0000000" }), HEAD);
  assert.equal(stale.ok, false);
  assert.match(stale.reason, /pull request head/);
});

test("a missing section is rejected", () => {
  const result = checkComment(record({ drop: "Rollback" }), HEAD);
  assert.equal(result.ok, false);
  assert.match(result.reason, /missing: Rollback/);
});

// "Not verified" is the section a second reviewer would otherwise have
// produced. An empty one is the failure mode this gate exists to catch.
test("an empty section is rejected", () => {
  const result = checkComment(record({ blank: "Not verified" }), HEAD);
  assert.equal(result.ok, false);
  assert.match(result.reason, /empty: Not verified/);
});

test("an unrelated comment is not treated as a record", () => {
  assert.deepEqual(checkComment("Looks good to me", HEAD), { ok: false, reason: null });
});

test("the newest usable record wins over an earlier malformed one", () => {
  const comments = [by(AUTHOR, record({ drop: "Intent" })), by(AUTHOR, record())];
  assert.deepEqual(checkSelfReview(comments, HEAD, AUTHOR), { ok: true });
});

test("with no record at all the reason points at the template", () => {
  const result = checkSelfReview([by(AUTHOR, "ping")], HEAD, AUTHOR);
  assert.equal(result.ok, false);
  assert.match(result.reason, /SELF_REVIEW_TEMPLATE\.md/);
});

test("a malformed record explains itself rather than reporting absence", () => {
  const result = checkSelfReview([by(AUTHOR, record({ drop: "Rollback" }))], HEAD, AUTHOR);
  assert.equal(result.ok, false);
  assert.match(result.reason, /missing: Rollback/);
});

// The forgery case. This repository is public, the head SHA is public, and
// anyone can comment on a pull request — so a record that is checked for shape
// alone is a control anyone can satisfy.
test("a well-formed record from someone other than the author is rejected", () => {
  const result = checkSelfReview([by("passer-by", record())], HEAD, AUTHOR);
  assert.equal(result.ok, false);
  assert.match(result.reason, /someone other than @ksuk/);
});

test("a foreign record does not satisfy the gate even alongside the author's stale one", () => {
  const comments = [by(AUTHOR, record({ sha: "0000000" })), by("passer-by", record())];
  const result = checkSelfReview(comments, HEAD, AUTHOR);
  assert.equal(result.ok, false);
  assert.match(result.reason, /pull request head/);
});

// GitHub logins are case-insensitive, and the API does not promise a
// particular case for either the comment author or the pull request author.
test("author matching ignores case", () => {
  const comments = [by("KSuk", record())];
  assert.deepEqual(checkSelfReview(comments, HEAD, "ksuk"), { ok: true });
  assert.deepEqual(checkSelfReview(comments, HEAD, "KSUK"), { ok: true });
});

test("a comment with no author is never a record", () => {
  const result = checkSelfReview([{ body: record() }], HEAD, AUTHOR);
  assert.equal(result.ok, false);
});

test("a non-sha argument is rejected", () => {
  assert.equal(checkSelfReview([], "not-a-sha", AUTHOR).ok, false);
});

test("a missing or malformed author is rejected rather than matching nothing", () => {
  assert.match(checkSelfReview([], HEAD, "").reason, /not a GitHub login/);
  assert.match(checkSelfReview([], HEAD, undefined).reason, /not a GitHub login/);
  assert.match(checkSelfReview([], HEAD, "bad login").reason, /not a GitHub login/);
});

function runCLI(args, stdin) {
  try {
    const stdout = execFileSync(process.execPath, [CLI, ...args], {
      encoding: "utf8",
      input: stdin,
      stdio: ["pipe", "pipe", "pipe"],
    });
    return { code: 0, stdout };
  } catch (err) {
    return { code: err.status, stdout: err.stdout ?? "" };
  }
}

test("the CLI accepts a valid record on stdin", () => {
  const stdin = JSON.stringify([by(AUTHOR, record())]);
  assert.equal(runCLI([HEAD, AUTHOR], stdin).code, 0);
});

test("the CLI rejects a record posted by anyone but the author", () => {
  const stdin = JSON.stringify([by("passer-by", record())]);
  assert.equal(runCLI([HEAD, AUTHOR], stdin).code, 1);
});

test("the CLI rejects an empty comment list", () => {
  assert.equal(runCLI([HEAD, AUTHOR], "[]").code, 1);
});

test("the CLI rejects malformed stdin and a missing argument", () => {
  assert.equal(runCLI([HEAD, AUTHOR], "not json").code, 2);
  assert.equal(runCLI([HEAD], "[]").code, 2);
  assert.equal(runCLI([], "[]").code, 2);
});
