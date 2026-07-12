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

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(__dirname, "..", "..");
const OUT_DIR = path.join(REPO_ROOT, "docs", "api");
const OPENAPI_JSON_PATH = path.join(OUT_DIR, "openapi.json");

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

function renderOpenApiPage(spec) {
  const info = spec.info || {};
  const title = info.title || "REST API Reference";
  const version = info.version || "unversioned";
  const server = spec.servers?.[0]?.url;
  const { counts, total } = countByMethod(spec.paths);

  const out = [];
  out.push("---");
  out.push("title: REST API Reference");
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
  out.push(
    "This page describes the Go API's REST surface, exported directly from its " +
      "route definitions as an [OpenAPI 3.0](https://spec.openapis.org/oas/v3.0.3) " +
      "document. It is generated automatically (`make generate-openapi`); do not " +
      "edit it directly."
  );
  out.push("");

  const metaRows = [
    ["OpenAPI version", `\`${spec.openapi || "unknown"}\``],
    ["API version", `\`${version}\``],
  ];
  if (server) metaRows.push(["Base path", `\`${server}\``]);
  metaRows.push(["Endpoints", String(total)]);
  out.push("| Field | Value |");
  out.push("|---|---|");
  for (const [k, v] of metaRows) out.push(`| ${k} | ${v} |`);
  out.push("");

  if (total > 0) {
    out.push("## Endpoints by method");
    out.push("");
    out.push("| Method | Count |");
    out.push("|---|---|");
    for (const method of Object.keys(counts).sort()) {
      out.push(`| \`${method}\` | ${counts[method]} |`);
    }
    out.push("");
  }

  out.push("## Full specification");
  out.push("");
  out.push(
    "The complete machine-readable spec is available at " +
      "[`openapi.json`](./openapi.json). Load it into any OpenAPI-compatible " +
      "tool (Swagger UI, Postman, Insomnia, `openapi-generator`, ...) to " +
      "explore or exercise the API interactively."
  );
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

  mkdirSync(OUT_DIR, { recursive: true });

  const pagePath = path.join(OUT_DIR, "openapi.md");
  writeFileSync(pagePath, renderOpenApiPage(spec), "utf8");
  console.log(`Wrote ${path.relative(REPO_ROOT, pagePath)}`);

  const categoryPath = path.join(OUT_DIR, "_category_.json");
  writeFileSync(
    categoryPath,
    JSON.stringify(
      {
        label: "API Reference",
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

main();
