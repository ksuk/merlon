#!/usr/bin/env node
// Check 3: every hand-written English doc (docs/**/*.md(x), excluding
// docs/api/** and docs/decisions/**) must have a real ja translation at the
// same relative path under
// website/i18n/ja/docusaurus-plugin-content-docs/current/.
//
// "Real" means: the file exists, is not byte-identical to the English
// source, is not one of the build-time fallback copies recorded in
// website/.i18n-fallback-manifest.json, and its first H1 contains Japanese
// characters.
//
// Exceptions (docs intentionally not translated) go in
// website/docs-lint/i18n-exceptions.json, an array of docs/-relative paths.
// An exception entry for a path that is not currently a hand-written en doc
// is a stale entry and fails the check.

import { readFileSync } from "node:fs";
import path from "node:path";
import {
  DOCS_DIR,
  DOCS_LINT_DIR,
  JA_DOCS_DIR,
  containsJapanese,
  fileExists,
  firstH1,
  listHandwrittenDocs,
  readFallbackManifest,
  readJson,
  splitFrontmatter,
  sortViolations,
  toPosix,
} from "./lib.mjs";

const EXCEPTIONS_PATH = path.join(DOCS_LINT_DIR, "i18n-exceptions.json");

export function run() {
  const exceptions = readJson(EXCEPTIONS_PATH, []);
  const fallbackManifest = new Set(readFallbackManifest());
  const violations = [];
  const usedExceptions = new Set();

  const handwritten = listHandwrittenDocs();
  const handwrittenSet = new Set(handwritten);

  for (const rel of handwritten) {
    if (exceptions.includes(rel)) {
      usedExceptions.add(rel);
      continue;
    }
    const displayRel = toPosix(path.join("docs", rel));
    const enAbs = path.join(DOCS_DIR, rel);
    const jaAbs = path.join(JA_DOCS_DIR, rel);

    if (!fileExists(jaAbs)) {
      violations.push({ file: displayRel, reason: "no ja translation found" });
      continue;
    }
    if (fallbackManifest.has(rel)) {
      violations.push({
        file: displayRel,
        reason: "ja file is a build-time fallback copy, not a real translation",
      });
      continue;
    }

    const enContent = readFileSync(enAbs, "utf8");
    const jaContent = readFileSync(jaAbs, "utf8");
    if (enContent === jaContent) {
      violations.push({
        file: displayRel,
        reason: "ja translation is byte-identical to the English source",
      });
      continue;
    }

    const { body: jaBody } = splitFrontmatter(jaContent);
    const jaH1 = firstH1(jaBody);
    if (!jaH1 || !containsJapanese(jaH1)) {
      violations.push({
        file: displayRel,
        reason: "ja translation's first H1 contains no Japanese characters",
      });
    }
  }

  for (const rel of exceptions) {
    if (!usedExceptions.has(rel) && !handwrittenSet.has(rel)) {
      violations.push({
        file: toPosix(path.join("docs", rel)),
        reason: "stale i18n-exceptions entry: not a hand-written en doc",
      });
    }
  }

  return sortViolations(violations);
}

function main() {
  const violations = run();
  if (violations.length === 0) {
    console.log("check-i18n-parity: PASS");
    process.exit(0);
  }
  console.log(`check-i18n-parity: FAIL (${violations.length} violation(s))`);
  for (const v of violations) {
    console.log(`  ${v.file}: ${v.reason}`);
  }
  process.exit(1);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main();
}
