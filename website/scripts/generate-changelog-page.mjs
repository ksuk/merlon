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
// committed and `--check` compares it with a fresh rendering. General
// documentation checks skip it because the generator validates both locales.
//
// Run: node scripts/generate-changelog-page.mjs   (from website/)

import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
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

export function renderPages(changelogText) {
  const sections = parseChangelog(changelogText).filter((section) =>
    /^\d+\.\d+\.\d+$/.test(section.version)
  );

  return Object.fromEntries(
    Object.entries(LOCALES).map(([locale, strings]) => [
      locale,
      renderPage(sections, strings),
    ])
  );
}

function normalizeLineEndings(text) {
  return text.replace(/\r\n?/g, "\n");
}

export function findPageDrift(expectedPages, actualPages) {
  return Object.keys(expectedPages).filter(
    (locale) =>
      typeof actualPages[locale] !== "string" ||
      normalizeLineEndings(actualPages[locale]) !==
        normalizeLineEndings(expectedPages[locale])
  );
}

function main(args) {
  const pages = renderPages(readChangelog());

  if (args.length > 1 || (args.length === 1 && args[0] !== "--check")) {
    console.error("usage: node scripts/generate-changelog-page.mjs [--check]");
    return 2;
  }

  if (args[0] === "--check") {
    const actualPages = Object.fromEntries(
      Object.entries(OUT_DIRS).map(([locale, outDir]) => {
        const outPath = path.join(outDir, OUT_FILE);
        try {
          return [locale, readFileSync(outPath, "utf8")];
        } catch (error) {
          if (error.code === "ENOENT") return [locale, null];
          throw error;
        }
      })
    );
    const drift = findPageDrift(pages, actualPages);
    if (drift.length > 0) {
      console.error(
        `Generated release notes are stale for: ${drift.join(", ")}. ` +
          "Run `cd website && npm run gen:changelog`."
      );
      return 1;
    }
    console.log("Generated release notes are current");
    return 0;
  }

  for (const [locale, outDir] of Object.entries(OUT_DIRS)) {
    mkdirSync(outDir, { recursive: true });
    const outPath = path.join(outDir, OUT_FILE);
    writeFileSync(outPath, pages[locale], "utf8");
    console.log(`Wrote ${path.relative(REPO_ROOT, outPath)}`);
  }
  console.log(`Generated release notes for ${Object.keys(pages).length} locale(s)`);
  return 0;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  process.exitCode = main(process.argv.slice(2));
}
