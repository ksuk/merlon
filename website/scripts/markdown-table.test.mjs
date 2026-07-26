import assert from "node:assert/strict";
import test from "node:test";

import {
  escapeMarkdownTableText,
  markdownTableInlineCode,
} from "./lib/markdown-table.mjs";

test("plain table text escapes existing backslashes before pipes", () => {
  assert.equal(
    escapeMarkdownTableText(String.raw`\|`),
    String.raw`\\\|`
  );
  assert.equal(
    escapeMarkdownTableText("\\\\" + "||"),
    "\\".repeat(5) + "|" + String.raw`\|`
  );
});

test("inline code escapes existing backslashes before pipes", () => {
  assert.equal(
    markdownTableInlineCode(String.raw`\|`),
    "`" + String.raw`\\\|` + "`"
  );
  assert.equal(
    markdownTableInlineCode("\\\\" + "||"),
    "`" + "\\".repeat(5) + "|" + String.raw`\|` + "`"
  );
});

test("plain table text preserves newline and MDX escaping behavior", () => {
  assert.equal(
    escapeMarkdownTableText("<Widget>{value}\nnext\r\nrow"),
    "&lt;Widget&gt;&#123;value&#125; next row"
  );
  assert.equal(escapeMarkdownTableText(null), "");
});

test("inline code preserves its existing literal and backtick behavior", () => {
  assert.equal(
    markdownTableInlineCode("<Widget>{value}\nnext`"),
    "`<Widget>{value}\nnextˋ`"
  );
});
