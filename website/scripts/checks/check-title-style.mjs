#!/usr/bin/env node
// Check 2: title conventions for hand-written English docs (docs/**/*.md(x),
// excluding docs/api/** and docs/decisions/**) and their _category_.json
// labels.
//
// Rules:
//  - frontmatter `title` must exist; if the doc has an H1, `title` must
//    equal it exactly.
//  - `title` and `sidebar_label` (frontmatter) and `label` (_category_.json)
//    must be Title Case: every word capitalized except the minor words
//    (a an the and or nor but for of in on to at by with vs via), unless
//    that word is the first or last word of the title.
//  - words listed in website/docs-lint/proper-nouns.json must appear
//    exactly as listed (case-sensitive) wherever they occur in a title.

import { readFileSync } from "node:fs";
import path from "node:path";
import {
  DOCS_LINT_DIR,
  DOCS_DIR,
  firstH1,
  listHandwrittenCategoryFiles,
  listHandwrittenDocs,
  parseFrontmatterFields,
  readJson,
  splitFrontmatter,
  sortViolations,
  toPosix,
} from "./lib.mjs";

const PROPER_NOUNS_PATH = path.join(DOCS_LINT_DIR, "proper-nouns.json");

const MINOR_WORDS = new Set([
  "a", "an", "the", "and", "or", "nor", "but", "for", "of", "in", "on",
  "to", "at", "by", "with", "vs", "via",
]);

function properNounViolations(title, properNouns) {
  const problems = [];
  for (const noun of properNouns) {
    const re = new RegExp(`\\b${noun}\\b`, "gi");
    let match;
    while ((match = re.exec(title))) {
      if (match[0] !== noun) {
        problems.push(`word "${match[0]}" should be written as "${noun}"`);
      }
    }
  }
  return problems;
}

function isTitleCaseWord(word, isEdge, properNouns) {
  // Strip surrounding punctuation for the capitalization check itself, but
  // keep hyphenated compounds as a single unit (each side checked).
  const parts = word.split("-");
  return parts.every((part) => {
    const bare = part.replace(/^[^A-Za-z0-9]+|[^A-Za-z0-9]+$/g, "");
    if (!bare) return true;
    if (properNouns.includes(bare)) return true;
    const lower = bare.toLowerCase();
    if (!isEdge && MINOR_WORDS.has(lower)) {
      return bare === lower;
    }
    // Must start with an uppercase letter (if it starts with a letter at all).
    if (!/[A-Za-z]/.test(bare[0])) return true;
    return bare[0] === bare[0].toUpperCase();
  });
}

function titleCaseViolations(title, properNouns) {
  const words = title.split(/\s+/).filter(Boolean);
  const problems = [];
  words.forEach((word, i) => {
    const isEdge = i === 0 || i === words.length - 1;
    if (!isTitleCaseWord(word, isEdge, properNouns)) {
      problems.push(`word "${word}" is not Title Case`);
    }
  });
  return problems;
}

export function run() {
  const properNouns = readJson(PROPER_NOUNS_PATH, []);
  const violations = [];

  for (const rel of listHandwrittenDocs()) {
    const displayRel = toPosix(path.join("docs", rel));
    const absPath = path.join(DOCS_DIR, rel);
    const content = readFileSync(absPath, "utf8");
    const { frontmatter, body } = splitFrontmatter(content);
    const fields = parseFrontmatterFields(frontmatter);
    const h1 = firstH1(body);

    if (!fields.title) {
      violations.push({ file: displayRel, reason: "missing frontmatter title" });
    } else {
      if (h1 && fields.title !== h1) {
        violations.push({
          file: displayRel,
          reason: `frontmatter title "${fields.title}" does not match H1 "${h1}"`,
        });
      }
      for (const problem of titleCaseViolations(fields.title, properNouns)) {
        violations.push({ file: displayRel, reason: `title: ${problem}` });
      }
      for (const problem of properNounViolations(fields.title, properNouns)) {
        violations.push({ file: displayRel, reason: `title: ${problem}` });
      }
    }

    if (fields.sidebar_label) {
      for (const problem of titleCaseViolations(fields.sidebar_label, properNouns)) {
        violations.push({ file: displayRel, reason: `sidebar_label: ${problem}` });
      }
      for (const problem of properNounViolations(fields.sidebar_label, properNouns)) {
        violations.push({ file: displayRel, reason: `sidebar_label: ${problem}` });
      }
    }
  }

  for (const rel of listHandwrittenCategoryFiles()) {
    const displayRel = toPosix(path.join("docs", rel));
    const absPath = path.join(DOCS_DIR, rel);
    let json;
    try {
      json = JSON.parse(readFileSync(absPath, "utf8"));
    } catch (err) {
      violations.push({ file: displayRel, reason: `invalid JSON: ${err.message}` });
      continue;
    }
    if (typeof json.label === "string") {
      for (const problem of titleCaseViolations(json.label, properNouns)) {
        violations.push({ file: displayRel, reason: `label: ${problem}` });
      }
      for (const problem of properNounViolations(json.label, properNouns)) {
        violations.push({ file: displayRel, reason: `label: ${problem}` });
      }
    }
  }

  return sortViolations(violations);
}

function main() {
  const violations = run();
  if (violations.length === 0) {
    console.log("check-title-style: PASS");
    process.exit(0);
  }
  console.log(`check-title-style: FAIL (${violations.length} violation(s))`);
  for (const v of violations) {
    console.log(`  ${v.file}: ${v.reason}`);
  }
  process.exit(1);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main();
}
