/**
 * Helpers for stubbing API responses in the shape the server actually returns.
 */

/**
 * A list-endpoint response body.
 *
 * Every list route answers with the `{"data": [...], "pagination": {...}}`
 * envelope (`writePaginatedJSON` in `api/internal/server/helpers.go`), which
 * the HTTP API contract §1.1 specifies. Stubbing `fetch` with a bare array
 * instead produces a test that passes while the page it exercises renders
 * nothing against a real server — that is exactly how every list view came to
 * be broken. Build list responses with this rather than by hand.
 *
 * Detail routes and the handful of list routes served with plain `writeJSON`
 * (customer score history, related cases) return bare arrays or objects and
 * should not use this.
 */
export function paginatedResponse<T>(items: T[], hasMore = false): Response {
  return new Response(
    JSON.stringify({ data: items, pagination: { has_more: hasMore } }),
    { status: 200, headers: { "Content-Type": "application/json" } },
  )
}
