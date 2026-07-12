#!/usr/bin/env node
// Check 5: Docusaurus UI-string translation files must exist, be valid
// JSON, and have every `message` value either contain Japanese characters
// or be entirely composed of allowlisted tokens (brand names, acronyms,
// numerals, and punctuation) from website/docs-lint/ui-string-allowlist.json.
//
// These files are generated/maintained by another in-flight workstream and
// may not exist yet; a missing-file failure here is expected until that
// lands.

import { readFileSync } from "node:fs";
import path from "node:path";
import { existsSync } from "node:fs";
import {
  DOCS_LINT_DIR,
  WEBSITE_DIR,
  containsJapanese,
  readJson,
  sortViolations,
} from "./lib.mjs";

const ALLOWLIST_PATH = path.join(DOCS_LINT_DIR, "ui-string-allowlist.json");

const TARGET_FILES = [
  "i18n/ja/code.json",
  "i18n/ja/docusaurus-plugin-content-docs/current.json",
  "i18n/ja/docusaurus-theme-classic/navbar.json",
  "i18n/ja/docusaurus-theme-classic/footer.json",
];

/** Strip allowlisted tokens (as whole-word matches) plus numerals/punctuation; true if nothing meaningful remains. */
function fullyAllowlisted(message, allowlist) {
  let remainder = message;
  // Longer tokens first so e.g. "REST API" is consumed before "API".
  const tokens = [...allowlist].sort((a, b) => b.length - a.length);
  for (const token of tokens) {
    remainder = remainder.split(token).join(" ");
  }
  // Numerals and punctuation/whitespace don't count against the message.
  remainder = remainder.replace(/[0-9\s.,;:!?()[\]{}'"\-_/\\|&%$#@*+=~`^<>]/g, "");
  return remainder.length === 0;
}

export function run() {
  const allowlist = readJson(ALLOWLIST_PATH, []);
  const violations = [];

  for (const relFile of TARGET_FILES) {
    const abs = path.join(WEBSITE_DIR, relFile);
    if (!existsSync(abs)) {
      violations.push({ file: relFile, reason: "file does not exist" });
      continue;
    }
    let json;
    try {
      json = JSON.parse(readFileSync(abs, "utf8"));
    } catch (err) {
      violations.push({ file: relFile, reason: `invalid JSON: ${err.message}` });
      continue;
    }
    const keys = Object.keys(json).sort();
    for (const key of keys) {
      const entry = json[key];
      const message = entry && typeof entry === "object" ? entry.message : undefined;
      if (typeof message !== "string") continue;
      if (containsJapanese(message)) continue;
      if (fullyAllowlisted(message, allowlist)) continue;
      violations.push({
        file: relFile,
        reason: `key "${key}" message "${message}" has no Japanese and is not fully allowlisted`,
      });
    }
  }

  return sortViolations(violations);
}

function main() {
  const violations = run();
  if (violations.length === 0) {
    console.log("check-ui-translations: PASS");
    process.exit(0);
  }
  console.log(`check-ui-translations: FAIL (${violations.length} violation(s))`);
  for (const v of violations) {
    console.log(`  ${v.file}: ${v.reason}`);
  }
  process.exit(1);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main();
}
