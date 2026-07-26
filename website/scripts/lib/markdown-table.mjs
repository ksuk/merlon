/**
 * Escape Markdown table syntax without allowing an input backslash to consume
 * the escape added for a pipe. The order is significant: `\|` must become
 * `\\\|`, not `\\|`.
 */
function escapeTableSyntax(value) {
  return value.replace(/\\/g, "\\\\").replace(/\|/g, "\\|");
}

/**
 * Escape plain text for a Markdown table cell in an MDX document.
 */
export function escapeMarkdownTableText(value) {
  if (value === undefined || value === null) return "";

  return escapeTableSyntax(String(value).replace(/\r?\n/g, " "))
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\{/g, "&#123;")
    .replace(/\}/g, "&#125;");
}

/**
 * Render a value as inline code in a Markdown table cell.
 */
export function markdownTableInlineCode(value) {
  const text = escapeTableSyntax(String(value)).replace(/`/g, "ˋ");
  return `\`${text}\``;
}
