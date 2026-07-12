#!/usr/bin/env node
// Generates Markdown reference pages from the rule-definition JSON Schemas
// under content/schema/ into docs/api/schema/ for the Docusaurus site.
//
// Why a dependency-free script instead of an existing generator:
// - json-schema-static-docs correctly resolves draft 2020-12 schemas, but its
//   only renderer emits raw HTML tables (<table>/<tr>/<td>) baked into its
//   Handlebars templates, with no built-in Markdown-table mode.
// - @adobe/jsonschema2md produces clean GFM Markdown, but it explodes every
//   nested property into its own linked file (dozens of files for our four
//   schemas) and documents itself as targeting draft 2019-09, not 2020-12.
// Our four schemas are simple (flat-ish, no $ref/allOf/oneOf), so a small
// purpose-built walker gives full control over layout (one page per schema),
// output safety (plain Markdown, no raw HTML/JSX-sensitive characters), and
// determinism, without taking on either library's mismatch.
//
// Run: node scripts/generate-schema-docs.mjs   (from website/)

import { readdirSync, readFileSync, writeFileSync, mkdirSync, rmSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(__dirname, "..", "..");
const SCHEMA_DIR = path.join(REPO_ROOT, "content", "schema");
const OUT_DIR = path.join(REPO_ROOT, "docs", "api", "schema");

// ---------------------------------------------------------------------------
// Markdown / MDX safety helpers
// ---------------------------------------------------------------------------

/**
 * Escape a plain-text string so it is safe to place inside a Markdown table
 * cell in an MDX document: neutralize the GFM table delimiter, MDX's JSX
 * opening character, and MDX expression braces, and collapse newlines.
 */
function escapeText(value) {
  if (value === undefined || value === null) return "";
  return String(value)
    .replace(/\r?\n/g, " ")
    .replace(/\|/g, "\\|")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\{/g, "&#123;")
    .replace(/\}/g, "&#125;");
}

/** Render a value as inline code, safe inside a Markdown table cell. */
function code(value) {
  const text = String(value).replace(/\|/g, "\\|").replace(/`/g, "ˋ");
  return `\`${text}\``;
}

function jsonCode(value) {
  return code(JSON.stringify(value));
}

// ---------------------------------------------------------------------------
// Schema introspection helpers
// ---------------------------------------------------------------------------

function typeLabel(schema) {
  if (schema.const !== undefined) return `const`;
  if (schema.enum) return "enum";
  if (Array.isArray(schema.type)) return schema.type.join(" \\| ");
  if (schema.type) {
    if (schema.type === "array" && schema.items?.type) {
      return `array of ${typeLabel(schema.items)}`;
    }
    if (schema.type === "array") return "array";
    return schema.type;
  }
  if (schema.properties) return "object";
  if (schema.additionalProperties && typeof schema.additionalProperties === "object") {
    return "object (map)";
  }
  return "any";
}

/** Collect the "Constraints" rows applicable to a single (sub)schema. */
function constraintRows(schema) {
  const rows = [];
  const add = (label, value) => rows.push([label, value]);

  if (schema.const !== undefined) add("Const", jsonCode(schema.const));
  if (schema.enum) add("Enum", schema.enum.map((v) => jsonCode(v)).join(", "));
  if (schema.default !== undefined) add("Default", jsonCode(schema.default));
  if (schema.format) add("Format", code(schema.format));
  if (schema.pattern) add("Pattern", code(schema.pattern));
  if (schema.minimum !== undefined) add("Minimum", code(schema.minimum));
  if (schema.maximum !== undefined) add("Maximum", code(schema.maximum));
  if (schema.exclusiveMinimum !== undefined) add("Exclusive minimum", code(schema.exclusiveMinimum));
  if (schema.exclusiveMaximum !== undefined) add("Exclusive maximum", code(schema.exclusiveMaximum));
  if (schema.multipleOf !== undefined) add("Multiple of", code(schema.multipleOf));
  if (schema.minLength !== undefined) add("Min length", code(schema.minLength));
  if (schema.maxLength !== undefined) add("Max length", code(schema.maxLength));
  if (schema.minItems !== undefined) add("Min items", code(schema.minItems));
  if (schema.maxItems !== undefined) add("Max items", code(schema.maxItems));
  if (schema.uniqueItems !== undefined) add("Unique items", code(schema.uniqueItems));
  if (schema.minProperties !== undefined) add("Min properties", code(schema.minProperties));
  if (schema.maxProperties !== undefined) add("Max properties", code(schema.maxProperties));
  if (schema.propertyNames?.pattern) {
    add("Property name pattern", code(schema.propertyNames.pattern));
  }
  if (typeof schema.additionalProperties === "boolean") {
    add("Additional properties", schema.additionalProperties ? "allowed" : "forbidden");
  }
  // Surface `required` when the schema doesn't otherwise declare `properties`
  // for each key (e.g. tier_thresholds only lists which keys must be
  // present); when `properties` exists, membership is already shown via the
  // "Required" column of the properties table, so avoid repeating it here.
  if (!schema.properties && schema.required?.length) {
    add("Required keys", schema.required.map((r) => code(r)).join(", "));
  }
  return rows;
}

/** True if a schema is worth recursing into for a nested "Properties" table. */
function hasNestedShape(schema) {
  if (!schema || typeof schema !== "object") return false;
  if (schema.properties) return true;
  if (schema.additionalProperties && typeof schema.additionalProperties === "object") return true;
  if (schema.items && typeof schema.items === "object" && schema.items.properties) return true;
  return false;
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

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

/**
 * Render the "Properties" summary table for an object-shaped schema, plus
 * (recursively) a detail subsection for every member that itself has
 * constraints or a nested shape worth documenting. The caller is
 * responsible for printing the heading that introduces this schema's own
 * table; this function only prints headings for each *child* member.
 *
 * @param schema     the object schema whose `properties`/`additionalProperties`/
 *                    `items` describe the members to document
 * @param breadcrumb dotted-path label prefix for this schema's members
 *                    (empty string at the document root)
 * @param level      Markdown heading level to use for each child member's heading
 */
function renderMembers(schema, breadcrumb, level, out) {
  const members = [];

  if (schema.properties) {
    const required = new Set(schema.required || []);
    for (const [name, propSchema] of Object.entries(schema.properties)) {
      members.push({ name, schema: propSchema, required: required.has(name) });
    }
  }
  if (schema.additionalProperties && typeof schema.additionalProperties === "object") {
    members.push({ name: "*", schema: schema.additionalProperties, required: false, isMap: true });
  }
  if (schema.items && typeof schema.items === "object") {
    if (schema.items.properties || Object.keys(schema.items).length > 0) {
      members.push({ name: "[]", schema: schema.items, required: false, isItems: true });
    }
  }

  if (members.length === 0) return;

  out.push(
    renderTable(
      ["Name", "Type", "Required", "Description"],
      members.map((m) => [
        m.isMap ? code("(any key)") : m.isItems ? code("(array item)") : code(m.name),
        typeLabel(m.schema),
        m.isMap || m.isItems ? "—" : m.required ? "Yes" : "No",
        escapeText(m.schema.description),
      ])
    )
  );
  out.push("");

  for (const m of members) {
    const rows = constraintRows(m.schema);
    const nested = hasNestedShape(m.schema);
    if (rows.length === 0 && !nested) continue;

    const childLabel = m.isMap
      ? `${breadcrumb}.*`
      : m.isItems
        ? `${breadcrumb}[]`
        : breadcrumb
          ? `${breadcrumb}.${m.name}`
          : m.name;

    out.push(heading(level, code(childLabel)));
    out.push("");
    if (rows.length > 0) {
      out.push(renderTable(["Constraint", "Value"], rows));
      out.push("");
    }
    if (nested) {
      renderMembers(m.schema, childLabel, level + 1, out);
    }
  }
}

function renderSchemaPage(schema, fileName) {
  const out = [];
  const title = schema.title || fileName;

  out.push("---");
  out.push(`title: ${title}`);
  out.push(`sidebar_label: ${title}`);
  out.push("---");
  out.push("");
  out.push(heading(1, title));
  out.push("");
  if (schema.description) {
    out.push(escapeText(schema.description));
    out.push("");
  }

  const metaRows = [];
  if (schema.$id) metaRows.push(["Schema ID", code(schema.$id)]);
  if (schema.$schema) metaRows.push(["JSON Schema dialect", code(schema.$schema)]);
  metaRows.push(["Type", typeLabel(schema)]);
  if (schema.required?.length) {
    metaRows.push(["Required top-level fields", schema.required.map((r) => code(r)).join(", ")]);
  }
  out.push(renderTable(["Field", "Value"], metaRows));
  out.push("");

  out.push(heading(2, "Properties"));
  out.push("");
  renderMembers(schema, "", 3, out);

  out.push(heading(2, "Full schema"));
  out.push("");
  out.push("```json");
  out.push(JSON.stringify(schema, null, 2));
  out.push("```");
  out.push("");

  return out.join("\n");
}

function renderIndexPage(entries) {
  const out = [];
  out.push("---");
  out.push("title: Rule Definition Schemas");
  out.push("sidebar_label: Overview");
  out.push("sidebar_position: 0");
  out.push("---");
  out.push("");
  out.push(heading(1, "Rule Definition Schemas"));
  out.push("");
  out.push(
    "Merlon's CDD scoring weights, country risk tables, and transaction-monitoring " +
      "scenarios are all expressed as JSON documents validated against the schemas " +
      "below (see `content/schema/` in the repository). These pages are generated " +
      "automatically from those JSON Schema files; do not edit them directly."
  );
  out.push("");
  out.push(
    renderTable(
      ["Schema", "Title", "Description"],
      entries.map((e) => [
        `[${e.fileName}](./${e.fileName}.md)`,
        e.schema.title || e.fileName,
        escapeText(e.schema.description),
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
  const files = readdirSync(SCHEMA_DIR)
    .filter((f) => f.endsWith(".json"))
    .sort();

  if (files.length === 0) {
    throw new Error(`No schema files found in ${SCHEMA_DIR}`);
  }

  rmSync(OUT_DIR, { recursive: true, force: true });
  mkdirSync(OUT_DIR, { recursive: true });

  const entries = [];
  for (const file of files) {
    const fileName = file.replace(/\.json$/, "");
    const raw = readFileSync(path.join(SCHEMA_DIR, file), "utf8");
    const schema = JSON.parse(raw);
    entries.push({ fileName, schema });
  }

  for (let i = 0; i < entries.length; i++) {
    const { fileName, schema } = entries[i];
    const markdown = renderSchemaPage(schema, fileName);
    const outPath = path.join(OUT_DIR, `${fileName}.md`);
    writeFileSync(outPath, markdown, "utf8");
    console.log(`Wrote ${path.relative(REPO_ROOT, outPath)}`);
  }

  const indexPath = path.join(OUT_DIR, "index.md");
  writeFileSync(indexPath, renderIndexPage(entries), "utf8");
  console.log(`Wrote ${path.relative(REPO_ROOT, indexPath)}`);

  const categoryPath = path.join(OUT_DIR, "_category_.json");
  writeFileSync(
    categoryPath,
    JSON.stringify(
      {
        label: "Rule Schemas",
        position: 3,
        link: { type: "doc", id: "api/schema/index" },
      },
      null,
      2
    ) + "\n",
    "utf8"
  );
  console.log(`Wrote ${path.relative(REPO_ROOT, categoryPath)}`);

  console.log(`Generated ${entries.length} schema page(s) + index into ${path.relative(REPO_ROOT, OUT_DIR)}`);
}

main();
