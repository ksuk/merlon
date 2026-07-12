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

function renderOpenApiPage(spec, L) {
  const info = spec.info || {};
  const title = "REST API Reference";
  const version = info.version || "unversioned";
  const server = spec.servers?.[0]?.url;
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

main();
