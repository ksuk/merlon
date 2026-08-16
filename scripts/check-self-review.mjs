#!/usr/bin/env node
// Verify that a pull request carries a self-review record bound to its current
// head commit.
//
// This repository has one maintainer, so no pull request is ever approved by a
// second person. The compensating control is that the author states, on the
// record, what the change does and what they did not verify. This script is
// what makes that a gate rather than a good intention: without a valid record
// the `Governance Required` status stays red. See ADR-0016 and
// .github/SELF_REVIEW_TEMPLATE.md.
//
// The record binds to two things, and it has to bind to both or it gates
// nothing:
//
//   * the head SHA, so pushing new commits invalidates it and a fresh record
//     is required — the same semantics as GitHub's
//     `dismiss_stale_reviews_on_push`, reproduced for a repository that has no
//     reviews to dismiss; and
//   * the pull request author, because this repository is public and anyone
//     can comment on a pull request. A head SHA is public too, so a record
//     checked for shape alone could be posted by a passer-by and would satisfy
//     the only review control this project has.
//
// Library:
//   import { checkSelfReview } from "./check-self-review.mjs";
//
// CLI (used by .github/workflows/governance.yml):
//   gh api ...comments > comments.json
//   node scripts/check-self-review.mjs <head-sha> <author-login> < comments.json
// Exits 0 when a valid record is present, 1 when it is not, 2 on bad usage.
// Comment bodies are read from stdin and never interpolated into a shell
// command, so a comment cannot inject anything into the workflow. No text a
// commenter controls reaches a returned `reason` either — the reasons quote
// only the fixed section titles, a regex-constrained hex SHA, and the author
// login the workflow passed in — so the reason is safe to publish verbatim in
// a commit status description.

import { readFileSync } from "node:fs";

const MARKER = /^##\s+Self-review record\s*$/im;
const HEAD_SHA = /^\s*[-*]?\s*\*\*Head SHA:\*\*\s*`?([0-9a-f]{7,40})`?\s*$/im;

// Every heading the template asks for. A record that drops one is not a
// record; "Not verified" in particular is the section that carries the
// information a second reviewer would otherwise have supplied.
export const REQUIRED_SECTIONS = [
  "Intent",
  "Blast radius",
  "Rollback",
  "Automated gates passed",
  "Not verified",
];

/** The body text under a `### <title>` heading, or null when absent. */
function sectionBody(body, title) {
  const escaped = title.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const heading = new RegExp(`^###\\s+${escaped}\\s*$`, "im");
  const match = heading.exec(body);
  if (!match) return null;
  const rest = body.slice(match.index + match[0].length);
  const next = /^#{2,3}\s+/m.exec(rest);
  return (next ? rest.slice(0, next.index) : rest).trim();
}

/**
 * Check one comment body against `headSha`.
 * Returns { ok: true } or { ok: false, reason } — `reason` is null when the
 * comment is simply not a self-review record, and a string when it is one but
 * an unusable one.
 */
export function checkComment(body, headSha) {
  if (!MARKER.test(body)) return { ok: false, reason: null };

  const sha = HEAD_SHA.exec(body);
  if (!sha) {
    return { ok: false, reason: "the record has no `**Head SHA:**` line" };
  }
  if (!headSha.startsWith(sha[1].toLowerCase())) {
    return {
      ok: false,
      reason:
        `the record covers ${sha[1]}, but the pull request head is ` +
        `${headSha.slice(0, 12)} — post a new record for the current head`,
    };
  }

  const missing = [];
  const empty = [];
  for (const title of REQUIRED_SECTIONS) {
    const section = sectionBody(body, title);
    if (section === null) missing.push(title);
    else if (section === "") empty.push(title);
  }
  if (missing.length > 0) {
    return { ok: false, reason: `the record is missing: ${missing.join(", ")}` };
  }
  if (empty.length > 0) {
    return { ok: false, reason: `these sections are empty: ${empty.join(", ")}` };
  }

  return { ok: true };
}

/**
 * Check every comment on a pull request. `comments` is the GitHub issue
 * comments array; only `body` and `user.login` are read. `authorLogin` is the
 * pull request author — comments by anyone else are not records and are
 * skipped before their shape is ever considered.
 *
 * The newest usable record wins, so a corrected re-post supersedes an earlier
 * malformed one. When no comment is a record at all the reason says so; when
 * some were records but none were usable, the most recent complaint is
 * returned, because that is the one the author needs to act on. A well-formed
 * record from somebody other than the author gets its own reason rather than
 * being reported as absence — it is the shape a forged record would take, and
 * it belongs in the run log.
 */
export function checkSelfReview(comments, headSha, authorLogin) {
  const sha = String(headSha).toLowerCase();
  if (!/^[0-9a-f]{7,40}$/.test(sha)) {
    return { ok: false, reason: `not a commit sha: ${headSha}` };
  }

  const author = String(authorLogin ?? "").toLowerCase();
  // GitHub logins are case-insensitive, so compare them that way. An empty
  // author would match nothing and silently fail the gate closed; that is the
  // safe direction, but it is a caller bug, so say which.
  if (!/^[a-z0-9](?:[a-z0-9]|-(?=[a-z0-9])){0,38}(?:\[bot\])?$/.test(author)) {
    return { ok: false, reason: `not a GitHub login: ${authorLogin}` };
  }

  let lastReason = null;
  let foreignRecord = false;
  for (const comment of comments) {
    const result = checkComment(String(comment?.body ?? ""), sha);
    const byAuthor = String(comment?.user?.login ?? "").toLowerCase() === author;
    if (!byAuthor) {
      if (result.ok || result.reason) foreignRecord = true;
      continue;
    }
    if (result.ok) return result;
    if (result.reason) lastReason = result.reason;
  }

  if (lastReason) return { ok: false, reason: lastReason };
  if (foreignRecord) {
    return {
      ok: false,
      reason:
        `a self-review record was posted by someone other than @${author} — ` +
        `only the author's own record satisfies this gate`,
    };
  }
  return {
    ok: false,
    reason:
      "no self-review record found — post one using .github/SELF_REVIEW_TEMPLATE.md",
  };
}

function readStdin() {
  return readFileSync(0, "utf8");
}

function main(argv) {
  const [headSha, authorLogin] = argv;
  if (!headSha || !authorLogin) {
    console.error(
      "usage: node scripts/check-self-review.mjs <head-sha> <author-login> < comments.json",
    );
    return 2;
  }

  let comments;
  try {
    comments = JSON.parse(readStdin() || "[]");
  } catch {
    console.error("stdin is not valid JSON (expected the GitHub comments array)");
    return 2;
  }
  if (!Array.isArray(comments)) {
    console.error("stdin must be a JSON array of comments");
    return 2;
  }

  const { ok, reason } = checkSelfReview(comments, headSha, authorLogin);
  if (ok) {
    console.log("Self-review record present for the current head commit.");
    return 0;
  }
  console.error(reason);
  return 1;
}

if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  process.exit(main(process.argv.slice(2)));
}
