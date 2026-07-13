#!/usr/bin/env node
// Runs every documentation check gate and prints a pass/fail summary table.
// Exits 1 if any check fails. Each individual check module exports a `run()`
// that returns a sorted array of `{ file, reason }` violations, so this
// script never spawns child processes -- it stays deterministic and fast.
//
// Run: node website/scripts/checks/run-all.mjs   (from anywhere; paths are
// resolved relative to the repository root via checks/lib.mjs)

import { run as checkEnLanguage } from "./check-en-language.mjs";
import { run as checkTitleStyle } from "./check-title-style.mjs";
import { run as checkI18nParity } from "./check-i18n-parity.mjs";
import { run as checkI18nFreshness } from "./check-i18n-freshness.mjs";
import { run as checkUiTranslations } from "./check-ui-translations.mjs";
import { run as checkReadme } from "./check-readme.mjs";

const CHECKS = [
  { name: "check-en-language", run: checkEnLanguage },
  { name: "check-title-style", run: checkTitleStyle },
  { name: "check-i18n-parity", run: checkI18nParity },
  { name: "check-i18n-freshness", run: checkI18nFreshness },
  { name: "check-ui-translations", run: checkUiTranslations },
  { name: "check-readme", run: checkReadme },
];

function main() {
  const results = CHECKS.map(({ name, run }) => ({ name, violations: run() }));

  console.log("");
  console.log("Documentation check gates");
  console.log("==========================");
  const nameWidth = Math.max(...CHECKS.map((c) => c.name.length));
  for (const { name, violations } of results) {
    const status = violations.length === 0 ? "PASS" : `FAIL (${violations.length})`;
    console.log(`  ${name.padEnd(nameWidth)}  ${status}`);
  }
  console.log("");

  let anyFail = false;
  for (const { name, violations } of results) {
    if (violations.length === 0) continue;
    anyFail = true;
    console.log(`--- ${name} ---`);
    for (const v of violations) {
      console.log(`  ${v.file}: ${v.reason}`);
    }
    console.log("");
  }

  if (anyFail) {
    console.log("Result: FAIL");
    process.exit(1);
  }
  console.log("Result: PASS");
  process.exit(0);
}

main();
