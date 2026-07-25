#!/usr/bin/env node
// Generates the Release Notes page (docs/release-notes.md and its ja
// counterpart) from the repository-root CHANGELOG.md.
//
// The changelog is published rather than duplicated: CHANGELOG.md stays the
// single source of truth, and the same file also produces the GitHub release
// notes via scripts/changelog.mjs, so the page, the release, and the
// repository can never disagree.
//
// Only the page's own frontmatter and intro are localized. The changelog body
// is emitted as-is in both locales, the same rule the schema and OpenAPI
// generators follow for source-derived content.
//
// Wired into the website `prebuild` script (package.json). The output is
// gitignored, like docs/api/**, and the documentation checks skip it.
//
// Run: node scripts/generate-changelog-page.mjs   (from website/)

import { writeFileSync, mkdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { LOCALES } from "./lib/locales.mjs";
import { readChangelog, parseChangelog } from "../../scripts/changelog.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(__dirname, "..", "..");
const OUT_DIRS = {
  en: path.join(REPO_ROOT, "docs"),
  ja: path.join(
    REPO_ROOT,
    "website",
    "i18n",
    "ja",
    "docusaurus-plugin-content-docs",
    "current"
  ),
};
const OUT_FILE = "release-notes.md";

function renderPage(sections, L) {
  const out = [];
  out.push("---");
  out.push(`title: ${L.releaseNotesTitle}`);
  out.push("---");
  out.push("");
  out.push(`# ${L.releaseNotesTitle}`);
  out.push("");
  out.push(L.releaseNotesIntro);
  out.push("");

  if (sections.length === 0) {
    out.push(L.releaseNotesEmpty);
    out.push("");
    return out.join("\n");
  }

  for (const section of sections) {
    const dateSuffix = section.date ? ` — ${section.date}` : "";
    out.push(`## ${section.version}${dateSuffix}`);
    out.push("");
    out.push(section.body);
    out.push("");
  }
  return out.join("\n");
}

function main() {
  const sections = parseChangelog(readChangelog());

  for (const [locale, outDir] of Object.entries(OUT_DIRS)) {
    mkdirSync(outDir, { recursive: true });
    const outPath = path.join(outDir, OUT_FILE);
    writeFileSync(outPath, renderPage(sections, LOCALES[locale]), "utf8");
    console.log(`Wrote ${path.relative(REPO_ROOT, outPath)}`);
  }
  console.log(`Generated release notes from ${sections.length} changelog section(s)`);
}

main();
