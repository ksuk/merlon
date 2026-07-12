#!/usr/bin/env node
// Check 4: for each en/ja doc pair that is a real translation (same
// definition as check-i18n-parity: ja file exists, isn't a fallback copy,
// isn't byte-identical), the ja translation must not be older than the
// English source.
//
// "Newer" is compared via `git log -1 --format=%ct` (last commit time). If
// the English file also has uncommitted local modifications, it is treated
// as newer than any committed ja translation (a pending edit not yet
// reflected in the translation) -- UNLESS the ja counterpart also has
// uncommitted local modifications (per `git status --porcelain`, including
// untracked files), in which case the pair passes: the ja side is at least
// as fresh as the en side. A pair where BOTH sides have never been
// committed (a brand-new doc + its brand-new translation, added together)
// passes automatically -- there's nothing to compare yet.
//
// Exceptions: website/docs-lint/i18n-freshness-acks.json maps a docs/-
// relative path to the en file's current HEAD commit hash
// (`git log -1 --format=%H -- <file>`), acknowledging that the ja
// translation is intentionally not yet updated for that exact en revision.
// An ack whose hash doesn't match the en file's current commit hash is
// stale and fails the check (the ack must be re-confirmed after further en
// changes).

import { readFileSync } from "node:fs";
import path from "node:path";
import {
  DOCS_DIR,
  DOCS_LINT_DIR,
  JA_DOCS_DIR,
  fileExists,
  gitHasUncommittedChanges,
  gitLastCommitHash,
  gitLastCommitTime,
  listHandwrittenDocs,
  readFallbackManifest,
  readJson,
  sortViolations,
  toPosix,
} from "./lib.mjs";

const ACKS_PATH = path.join(DOCS_LINT_DIR, "i18n-freshness-acks.json");

export function run() {
  const acks = readJson(ACKS_PATH, {});
  const fallbackManifest = new Set(readFallbackManifest());
  const violations = [];

  for (const rel of listHandwrittenDocs()) {
    const displayRel = toPosix(path.join("docs", rel));
    const enAbs = path.join(DOCS_DIR, rel);
    const jaAbs = path.join(JA_DOCS_DIR, rel);

    if (!fileExists(jaAbs) || fallbackManifest.has(rel)) continue; // covered by check-i18n-parity
    const enContent = readFileSync(enAbs, "utf8");
    const jaContent = readFileSync(jaAbs, "utf8");
    if (enContent === jaContent) continue; // covered by check-i18n-parity

    const enUncommitted = gitHasUncommittedChanges(enAbs);
    const jaUncommitted = gitHasUncommittedChanges(jaAbs);
    const enCommitTime = gitLastCommitTime(enAbs);
    const jaCommitTime = gitLastCommitTime(jaAbs);

    const enNeverCommitted = enCommitTime === null;
    const jaNeverCommitted = jaCommitTime === null;

    if (enNeverCommitted && jaNeverCommitted) {
      continue; // brand-new pair, added together: passes
    }

    // If the ja file also has a pending uncommitted edit, treat it as at
    // least as fresh as the en side -- the translator is actively working on
    // (or has already updated) this exact pair, so there's nothing to flag.
    if (enUncommitted && jaUncommitted) continue;

    const enIsNewer = enUncommitted
      ? true
      : enNeverCommitted
        ? true
        : jaNeverCommitted
          ? true
          : enCommitTime > jaCommitTime;

    if (!enIsNewer) continue;

    const enHeadHash = gitLastCommitHash(enAbs);
    const ackedHash = acks[rel];
    if (enUncommitted || enHeadHash === null) {
      // A pending uncommitted en edit (or an en file with no commit yet)
      // cannot be acknowledged by a commit-hash ack: there is no stable
      // hash to pin the ack to yet.
      violations.push({
        file: displayRel,
        reason: "en file has uncommitted changes newer than the ja translation",
      });
      continue;
    }
    if (ackedHash === enHeadHash) {
      continue; // acknowledged for this exact en revision
    }
    if (ackedHash) {
      violations.push({
        file: displayRel,
        reason: `stale freshness ack: acked hash ${ackedHash} does not match en HEAD ${enHeadHash}`,
      });
      continue;
    }
    violations.push({
      file: displayRel,
      reason: `en translation is newer than ja (en HEAD ${enHeadHash}); add a freshness ack or update the ja translation`,
    });
  }

  // Stale acks for paths that are no longer out-of-date, not hand-written, or
  // don't exist would silently mask nothing -- but keep the ack list honest
  // by flagging entries for files that aren't tracked hand-written docs.
  const handwrittenSet = new Set(listHandwrittenDocs());
  for (const rel of Object.keys(acks)) {
    if (!handwrittenSet.has(rel)) {
      violations.push({
        file: toPosix(path.join("docs", rel)),
        reason: "stale i18n-freshness-acks entry: not a hand-written en doc",
      });
    }
  }

  return sortViolations(violations);
}

function main() {
  const violations = run();
  if (violations.length === 0) {
    console.log("check-i18n-freshness: PASS");
    process.exit(0);
  }
  console.log(`check-i18n-freshness: FAIL (${violations.length} violation(s))`);
  for (const v of violations) {
    console.log(`  ${v.file}: ${v.reason}`);
  }
  process.exit(1);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main();
}
