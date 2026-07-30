#!/usr/bin/env node
// Generates a lightweight landing page for the REST API reference into
// docs/api/openapi.md + docs/api/_category_.json, from the OpenAPI document
// that `make generate-openapi` (api/cmd/openapi-export, a Go program) writes
// to docs/api/openapi.json.
//
// Why a link page instead of an embedded API explorer: a full renderer
// (e.g. redocusaurus) pulls in a heavy dependency tree for a single spec
// file. docs/api/openapi.json is already served as a static asset by the
// Docusaurus docs plugin (any non-.md/.mdx file referenced by a relative
// link from a doc gets copied into the build and the link rewritten), so a
// short page summarizing the API and linking to the raw spec covers the
// requirement without the extra install/build cost. Revisit if the site
// grows a need for in-browser "try it" request building.
//
// Wired into the website `prebuild` script (package.json), after gen:schema
// and before sync:i18n, so it runs on every `npm run build`. Requires
// docs/api/openapi.json to already exist -- run `make generate-openapi`
// (or `make docs-build`) first; see the error below if it doesn't.
//
// Run: node scripts/generate-openapi-page.mjs   (from website/)

import { readFileSync, writeFileSync, existsSync, mkdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { LOCALES } from "./lib/locales.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(__dirname, "..", "..");
const OUT_DIR_EN = path.join(REPO_ROOT, "docs", "api");
const OUT_DIR_JA = path.join(
  REPO_ROOT,
  "website",
  "i18n",
  "ja",
  "docusaurus-plugin-content-docs",
  "current",
  "api"
);
const OUT_DIRS = { en: OUT_DIR_EN, ja: OUT_DIR_JA };
const OPENAPI_JSON_PATH = path.join(OUT_DIR_EN, "openapi.json");

/** Escape text for safe placement in Markdown/MDX prose (see generate-schema-docs.mjs). */
function escapeText(value) {
  if (value === undefined || value === null) return "";
  return String(value)
    .replace(/\r?\n/g, " ")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\{/g, "&#123;")
    .replace(/\}/g, "&#125;");
}

function countByMethod(paths) {
  const counts = {};
  let total = 0;
  for (const item of Object.values(paths || {})) {
    for (const method of Object.keys(item)) {
      const m = method.toUpperCase();
      if (!["GET", "PUT", "POST", "DELETE", "PATCH", "OPTIONS", "HEAD", "TRACE"].includes(m)) continue;
      counts[m] = (counts[m] || 0) + 1;
      total++;
    }
  }
  return { counts, total };
}

/**
 * The mount point most endpoints in the document share, e.g. "/api/v1".
 *
 * This is the base path a reader needs, and it is not `servers[0].url`: the
 * path keys already carry the mount point, so that field is "/" and printing
 * it verbatim tells a reader nothing.
 *
 * Not a prefix common to *every* path, because the probes are deliberately
 * mounted at the root (/healthz) alongside the API, and requiring universality
 * would yield "" for exactly the documents this is meant to describe. Instead
 * the paths are grouped by first segment and the largest group's shared prefix
 * wins, so the handful of root-mounted endpoints do not erase the answer for
 * the rest. Returns "" only when the document has no paths at all.
 */
export function dominantPathPrefix(paths) {
  const keys = Object.keys(paths || {}).filter((key) => key.startsWith("/"));
  if (keys.length === 0) return "";

  const groups = new Map();
  for (const key of keys) {
    const segments = key.split("/").filter(Boolean);
    if (segments.length === 0) continue;
    const group = groups.get(segments[0]) ?? [];
    group.push(segments);
    groups.set(segments[0], group);
  }
  if (groups.size === 0) return "";

  // Insertion order breaks ties, so the result does not depend on Map ordering
  // details when two groups are the same size.
  let largest = [];
  for (const group of groups.values()) {
    if (group.length > largest.length) largest = group;
  }

  let prefix = largest[0];
  for (const segments of largest.slice(1)) {
    let shared = 0;
    while (
      shared < prefix.length &&
      shared < segments.length &&
      prefix[shared] === segments[shared]
    ) {
      shared++;
    }
    prefix = prefix.slice(0, shared);
    if (prefix.length === 0) return "";
  }
  return `/${prefix.join("/")}`;
}

export function renderOpenApiPage(spec, L) {
  const info = spec.info || {};
  const title = "REST API Reference";
  const version = info.version || "unversioned";
  const declaredServer = spec.servers?.[0]?.url;
  // A declared server of "/" (or nothing) is not a usable base path, so prefer
  // what the paths themselves say and keep the declared value only as a
  // fallback for a document whose paths share no prefix.
  const basePrefix = dominantPathPrefix(spec.paths);
  const server =
    basePrefix ||
    (declaredServer && declaredServer !== "/" ? declaredServer : "");
  const { counts, total } = countByMethod(spec.paths);

  const out = [];
  out.push("---");
  out.push(`title: ${L.restApiReferenceTitle}`);
  out.push("sidebar_label: REST API");
  out.push("sidebar_position: 1");
  out.push("---");
  out.push("");
  out.push(`# ${escapeText(title)}`);
  out.push("");
  if (info.description) {
    out.push(escapeText(info.description));
    out.push("");
  }
  out.push(L.openapiIntro);
  out.push("");

  const metaRows = [
    [L.openapiVersion, `\`${spec.openapi || "unknown"}\``],
    [L.apiVersion, `\`${version}\``],
  ];
  if (server) metaRows.push([L.basePath, `\`${server}\``]);
  metaRows.push([L.endpoints, String(total)]);
  out.push(`| ${L.field} | ${L.value} |`);
  out.push("|---|---|");
  for (const [k, v] of metaRows) out.push(`| ${k} | ${v} |`);
  out.push("");

  if (total > 0) {
    out.push(`## ${L.endpointsByMethod}`);
    out.push("");
    out.push(`| ${L.method} | ${L.count} |`);
    out.push("|---|---|");
    for (const method of Object.keys(counts).sort()) {
      out.push(`| \`${method}\` | ${counts[method]} |`);
    }
    out.push("");
  }

  out.push(`## ${L.fullSpecification}`);
  out.push("");
  out.push(L.openapiFullSpec("./openapi.json"));
  out.push("");

  return out.join("\n");
}

function main() {
  if (!existsSync(OPENAPI_JSON_PATH)) {
    throw new Error(
      `${path.relative(REPO_ROOT, OPENAPI_JSON_PATH)} not found. Run ` +
        `"make generate-openapi" (or "make docs-build") before building the site.`
    );
  }
  const spec = JSON.parse(readFileSync(OPENAPI_JSON_PATH, "utf8"));
  const rawJson = readFileSync(OPENAPI_JSON_PATH, "utf8");

  for (const locale of Object.keys(OUT_DIRS)) {
    const L = LOCALES[locale];
    const OUT_DIR = OUT_DIRS[locale];

    mkdirSync(OUT_DIR, { recursive: true });

    const pagePath = path.join(OUT_DIR, "openapi.md");
    writeFileSync(pagePath, renderOpenApiPage(spec, L), "utf8");
    console.log(`Wrote ${path.relative(REPO_ROOT, pagePath)}`);

    // The ja page also links to ./openapi.json; the spec itself isn't
    // localized, so mirror the same JSON alongside the ja page so that
    // relative link resolves under the ja locale route too.
    if (locale !== "en") {
      writeFileSync(path.join(OUT_DIR, "openapi.json"), rawJson, "utf8");
      console.log(`Wrote ${path.relative(REPO_ROOT, path.join(OUT_DIR, "openapi.json"))}`);
    }

    const categoryPath = path.join(OUT_DIR, "_category_.json");
    writeFileSync(
      categoryPath,
      JSON.stringify(
        {
          label: L.categoryApiReference,
          position: 4,
          link: { type: "doc", id: "api/openapi" },
        },
        null,
        2
      ) + "\n",
      "utf8"
    );
    console.log(`Wrote ${path.relative(REPO_ROOT, categoryPath)}`);
  }
}

// Run as a CLI only when invoked directly, so the renderer can be imported
// by tests without writing files.
if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  main();
}
