#!/usr/bin/env node
// Renders Markdown reference pages for the gRPC contract under proto/merlon/v1/
// into docs/api/proto/ for the Docusaurus site, from the JSON description
// that `buf generate --template buf.gen.docs.yaml` (pseudomuto-doc's "json"
// template) writes to docs/api/proto/reference.json.
//
// Why a dependency-free renderer instead of pseudomuto-doc's own "markdown"
// template: that template's output is usable Markdown but leans on raw HTML
// for in-page navigation (`<a name="...">` anchors, `<p align="right">` back-
// to-top links) and bundles every .proto file into one long page with its own
// hand-rolled table of contents, which duplicates and fights Docusaurus's
// sidebar/TOC. It technically compiles as MDX, but it's not the "sane
// titles/front matter, one or more pages" shape this site wants. The "json"
// template instead hands us a structured description with none of that, so
// this script (mirroring generate-schema-docs.mjs) walks it into plain,
// MDX-safe Markdown: one page per .proto file, plus an index and a scalar
// value type reference, each with real Docusaurus front matter.
//
// Run: node scripts/generate-proto-docs.mjs   (from website/, after
//      `cd proto && buf generate --template buf.gen.docs.yaml` has produced
//      docs/api/proto/reference.json)

import { readFileSync, writeFileSync, mkdirSync, rmSync, existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(__dirname, "..", "..");
const OUT_DIR = path.join(REPO_ROOT, "docs", "api", "proto");
const REFERENCE_JSON = path.join(OUT_DIR, "reference.json");

// ---------------------------------------------------------------------------
// Markdown / MDX safety helpers (same rules as generate-schema-docs.mjs)
// ---------------------------------------------------------------------------

/**
 * Escape a plain-text string so it is safe to place inside a Markdown table
 * cell in an MDX document: neutralize the GFM table delimiter, MDX's JSX
 * opening character, and MDX expression braces, and collapse newlines.
 */
function escapeText(value) {
  if (value === undefined || value === null) return "";
  return String(value)
    .replace(/\r\n/g, "\n")
    .replace(/\r?\n/g, " ")
    .trim()
    .replace(/\|/g, "\\|")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\{/g, "&#123;")
    .replace(/\}/g, "&#125;");
}

/** Render a value as inline code, safe inside a Markdown table cell. */
function code(value) {
  if (value === undefined || value === null || value === "") return "";
  const text = String(value).replace(/\|/g, "\\|").replace(/`/g, "ˋ");
  return `\`${text}\``;
}

function heading(level, text) {
  return `${"#".repeat(Math.min(level, 6))} ${text}`;
}

function renderTable(headerCells, rows) {
  const lines = [];
  lines.push(`| ${headerCells.join(" | ")} |`);
  lines.push(`|${headerCells.map(() => "---").join("|")}|`);
  for (const row of rows) {
    lines.push(`| ${row.join(" | ")} |`);
  }
  return lines.join("\n");
}

// ---------------------------------------------------------------------------
// Proto-description helpers
// ---------------------------------------------------------------------------

/** "merlon/v1/types.proto" -> "types" */
function baseName(protoFileName) {
  return path.basename(protoFileName, ".proto");
}

/** "types" -> "Types"; "monitoring" -> "Monitoring" */
function titleCase(slug) {
  return slug.charAt(0).toUpperCase() + slug.slice(1);
}

function fieldTypeCell(field) {
  const type = field.ismap ? `map<${escapeText(field.keyType || "")}, ${code(field.type)}>` : code(field.type);
  return type;
}

function renderMessage(message, level, out) {
  out.push(heading(level, code(message.longName || message.name)));
  out.push("");
  if (message.description) {
    out.push(escapeText(message.description));
    out.push("");
  }
  if (!message.fields || message.fields.length === 0) {
    out.push("_No fields._");
    out.push("");
    return;
  }
  out.push(
    renderTable(
      ["Field", "Type", "Label", "Description"],
      message.fields.map((f) => [
        code(f.name),
        fieldTypeCell(f),
        f.label ? code(f.label) : "—",
        escapeText(f.description) || "—",
      ])
    )
  );
  out.push("");
}

function renderEnum(en, level, out) {
  out.push(heading(level, code(en.longName || en.name)));
  out.push("");
  if (en.description) {
    out.push(escapeText(en.description));
    out.push("");
  }
  out.push(
    renderTable(
      ["Value", "Number", "Description"],
      (en.values || []).map((v) => [code(v.name), code(v.number), escapeText(v.description) || "—"])
    )
  );
  out.push("");
}

function renderService(service, level, out) {
  out.push(heading(level, code(service.longName || service.name)));
  out.push("");
  if (service.description) {
    out.push(escapeText(service.description));
    out.push("");
  }
  out.push(
    renderTable(
      ["Method", "Request", "Response", "Description"],
      (service.methods || []).map((m) => {
        const request = `${code(m.requestFullType)}${m.requestStreaming ? " (streaming)" : ""}`;
        const response = `${code(m.responseFullType)}${m.responseStreaming ? " (streaming)" : ""}`;
        const deprecated = m.options?.deprecated ? "**Deprecated.** " : "";
        return [code(m.name), request, response, deprecated + (escapeText(m.description) || "—")];
      })
    )
  );
  out.push("");
}

function renderFilePage(file) {
  const slug = baseName(file.name);
  const title = titleCase(slug);
  const out = [];

  out.push("---");
  out.push(`title: ${title}`);
  out.push(`sidebar_label: ${title}`);
  out.push("---");
  out.push("");
  out.push(heading(1, title));
  out.push("");
  out.push(`Source: \`${file.name}\` (package \`${file.package}\`)`);
  out.push("");
  if (file.description) {
    out.push(escapeText(file.description));
    out.push("");
  }

  if (file.services?.length) {
    out.push(heading(2, "Services"));
    out.push("");
    for (const service of file.services) renderService(service, 3, out);
  }

  if (file.messages?.length) {
    out.push(heading(2, "Messages"));
    out.push("");
    for (const message of file.messages) renderMessage(message, 3, out);
  }

  if (file.enums?.length) {
    out.push(heading(2, "Enums"));
    out.push("");
    for (const en of file.enums) renderEnum(en, 3, out);
  }

  return { slug, title, markdown: out.join("\n") };
}

function renderIndexPage(pages, files) {
  const out = [];
  out.push("---");
  out.push("title: gRPC Protocol Reference");
  out.push("sidebar_label: Overview");
  out.push("sidebar_position: 0");
  out.push("---");
  out.push("");
  out.push(heading(1, "gRPC Protocol Reference"));
  out.push("");
  out.push(
    "Merlon's Go API and Rust Engine communicate over the gRPC contract defined in " +
      "`proto/merlon/v1/` (managed by [buf](https://buf.build)). These pages are " +
      "generated automatically from those `.proto` files; do not edit them directly."
  );
  out.push("");
  out.push(
    renderTable(
      ["Proto file", "Package", "Services", "Messages", "Enums"],
      files.map((f, i) => [
        `[${f.name}](./${pages[i].slug}.md)`,
        code(f.package),
        String(f.services?.length || 0),
        String(f.messages?.length || 0),
        String(f.enums?.length || 0),
      ])
    )
  );
  out.push("");
  out.push(`See also the [scalar value type reference](./scalar-value-types.md).`);
  out.push("");
  return out.join("\n");
}

function renderScalarTypesPage(scalarValueTypes) {
  const out = [];
  out.push("---");
  out.push("title: Scalar Value Types");
  out.push("sidebar_label: Scalar Value Types");
  out.push("sidebar_position: 99");
  out.push("---");
  out.push("");
  out.push(heading(1, "Scalar Value Types"));
  out.push("");
  out.push("How each protobuf scalar type maps onto common language types.");
  out.push("");
  out.push(
    renderTable(
      ["Proto Type", "Notes", "C++", "C#", "Go", "Java", "PHP", "Python", "Ruby"],
      (scalarValueTypes || []).map((t) => [
        code(t.protoType),
        escapeText(t.notes) || "—",
        code(t.cppType),
        code(t.csType),
        code(t.goType),
        code(t.javaType),
        code(t.phpType),
        code(t.pythonType),
        code(t.rubyType),
      ])
    )
  );
  out.push("");
  return out.join("\n");
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

function main() {
  if (!existsSync(REFERENCE_JSON)) {
    throw new Error(
      `${path.relative(REPO_ROOT, REFERENCE_JSON)} not found. Run ` +
        `"cd proto && buf generate --template buf.gen.docs.yaml" first.`
    );
  }
  const raw = readFileSync(REFERENCE_JSON, "utf8");
  const description = JSON.parse(raw);
  const files = description.files || [];
  if (files.length === 0) {
    throw new Error(`No proto files found in ${REFERENCE_JSON}`);
  }

  // Wipe and recreate OUT_DIR: this also removes the intermediate
  // reference.json emitted by buf, which is not meant to be served.
  rmSync(OUT_DIR, { recursive: true, force: true });
  mkdirSync(OUT_DIR, { recursive: true });

  const pages = files.map(renderFilePage);
  for (const page of pages) {
    const outPath = path.join(OUT_DIR, `${page.slug}.md`);
    writeFileSync(outPath, page.markdown, "utf8");
    console.log(`Wrote ${path.relative(REPO_ROOT, outPath)}`);
  }

  const indexPath = path.join(OUT_DIR, "index.md");
  writeFileSync(indexPath, renderIndexPage(pages, files), "utf8");
  console.log(`Wrote ${path.relative(REPO_ROOT, indexPath)}`);

  const scalarPath = path.join(OUT_DIR, "scalar-value-types.md");
  writeFileSync(scalarPath, renderScalarTypesPage(description.scalarValueTypes), "utf8");
  console.log(`Wrote ${path.relative(REPO_ROOT, scalarPath)}`);

  const categoryPath = path.join(OUT_DIR, "_category_.json");
  writeFileSync(
    categoryPath,
    JSON.stringify(
      {
        label: "gRPC Protocol Reference",
        position: 2,
        link: { type: "doc", id: "api/proto/index" },
      },
      null,
      2
    ) + "\n",
    "utf8"
  );
  console.log(`Wrote ${path.relative(REPO_ROOT, categoryPath)}`);

  console.log(`Generated ${pages.length} proto page(s) + index + scalar types into ${path.relative(REPO_ROOT, OUT_DIR)}`);
}

main();
