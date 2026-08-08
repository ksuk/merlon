import { fireEvent, screen, within } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter, Route, Routes } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { CustomerDetailPage } from "./customer-detail"

// customer-detail carries #75, #76 and #77 in one 906-line page and had two
// pre-existing tests, neither of which touched a control. These cover the
// paths where getting it wrong changes a regulated decision: closing an EDD
// window, choosing the rule set a score is computed under, approving someone
// else's tier override, and being told the factors do not add up.

const customer = {
  id: "c1",
  external_id: "EXT-001",
  customer_type: "individual",
  country_code: "JP",
  product_types: [],
  attributes: { name: "Test Customer" },
  status: "active",
  risk_score: 72,
  risk_tier: "high",
  created_at: "2025-01-01T00:00:00Z",
  updated_at: "2025-01-15T00:00:00Z",
}

const eddPanel = {
  required: true,
  requested_at: "2025-01-01T00:00:00Z",
  current_stage: "stage2",
  next_stage: "stage3",
  elapsed_days: 65,
  remaining_days: 0,
  overdue_days: 5,
  due_at: "2025-01-10T00:00:00Z",
  completion_status: "overdue",
  policy_version: "2026-08-06-default",
}

const investigation = {
  customer,
  counts: {},
  transactions: [],
  alerts: [],
  cases: [],
  screening_results: [],
  score_history: [],
  timeline: [],
  edd: eddPanel,
  partial_failures: [],
  freshness: "2025-01-15T00:00:00Z",
}

const ruleSets = {
  data: [
    { id: "cdd_basic", name: "cdd_basic", version: 2, is_active: true, digest: "abc123def456789", recommended: true, matched_on: "customer_type=individual" },
    { id: "cdd_strict", name: "cdd_strict", version: 1, is_active: true, digest: "999888777666555", recommended: false },
  ],
  policy_version: "2026-08-06-default",
  selection_authority: true,
}

const pendingOverride = {
  id: "ovr-1",
  customer_id: "c1",
  score_record_id: "score-1",
  proposed_tier: "medium",
  computed_tier: "high",
  computed_score: 72,
  reason: "documented mitigating controls",
  status: "pending_approval",
  requested_by: "analyst-a",
  requested_at: "2025-01-14T00:00:00Z",
  version: 1,
}

const explanation = {
  score_id: "score-1",
  score: { id: "score-1", customer_id: "c1", score: 72, tier: "high", factors: [], rule_set_id: "cdd_basic", rule_set_version: 2, scored_at: "2025-01-15T00:00:00Z" },
  total_reconciled: 65,
  reconciled: false,
  reconciliation_delta: 7,
  tier_reason: "high band 60-100",
  tier_thresholds: { low: [0, 30], medium: [30, 60], high: [60, 100] },
  rule_set_id: "cdd_basic",
  rule_set_sha256: "abc123def456789000",
  deterministic: true,
}

const posted: { url: string; body: unknown }[] = []

function mockAPI(overrides: Record<string, unknown> = {}) {
  posted.length = 0
  vi.spyOn(globalThis, "fetch").mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url
    if (init?.method && init.method !== "GET") {
      posted.push({ url, body: init.body ? JSON.parse(String(init.body)) : null })
    }
    for (const [fragment, body] of Object.entries(overrides)) {
      if (url.includes(fragment)) return Promise.resolve(new Response(JSON.stringify(body)))
    }
    if (url.includes("/edd/")) return Promise.resolve(new Response(JSON.stringify({ ...eddPanel, completion_status: "completed" })))
    if (url.includes("/score-overrides/")) return Promise.resolve(new Response(JSON.stringify({ ...pendingOverride, status: "approved" })))
    if (url.includes("/score-overrides")) return Promise.resolve(new Response(JSON.stringify([pendingOverride])))
    if (url.includes("/cdd-rule-sets")) return Promise.resolve(new Response(JSON.stringify(ruleSets)))
    if (url.includes("/score-explanation")) return Promise.resolve(new Response(JSON.stringify(explanation)))
    if (url.includes("/investigation")) return Promise.resolve(new Response(JSON.stringify(investigation)))
    if (url.includes("/policies/")) return Promise.resolve(new Response(JSON.stringify({ document: {}, version: "v1" })))
    if (url.match(/\/customers\/c1(\?|$)/)) return Promise.resolve(new Response(JSON.stringify(customer)))
    return Promise.resolve(new Response(JSON.stringify({ data: [], pagination: { has_more: false } })))
  })
}

function renderDetail() {
  return renderWithI18n(
    <MemoryRouter initialEntries={["/customers/c1"]}>
      <Routes>
        <Route path="customers/:id" element={<CustomerDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("an overdue EDD window shows how late it is and can be completed with a rationale", async () => {
  mockAPI()
  await renderDetail()

  // overdue_days, not remaining_days: the latter is clamped at zero, so a
  // window five days late would read as due today.
  expect(await screen.findByText("期限超過 5 日")).toBeDefined()

  fireEvent.change(screen.getByLabelText("理由"), { target: { value: "documents received" } })
  fireEvent.click(screen.getByRole("button", { name: /完了/ }))

  await vi.waitFor(() => {
    const call = posted.find((entry) => entry.url.includes("/edd/complete"))
    expect(call).toBeDefined()
    expect((call!.body as { rationale: string }).rationale).toBe("documents received")
  })
})

test("the rule-set selector offers the policy's candidates and marks the recommended one", async () => {
  mockAPI()
  await renderDetail()

  const select = await screen.findByLabelText(/ルールセット/)
  const options = within(select as HTMLElement).getAllByRole("option")
  const labels = options.map((option) => option.textContent ?? "")
  expect(labels.some((label) => label.includes("cdd_basic") && label.includes("推奨"))).toBe(true)
  expect(labels.some((label) => label.includes("cdd_strict"))).toBe(true)
})

test("a pending tier override can be approved and sends its expected version", async () => {
  mockAPI()
  await renderDetail()

  const approve = await screen.findByRole("button", { name: /承認/ })

  // A decision with no rationale must be refused rather than sent: the
  // second signature is the point of the control.
  fireEvent.click(approve)
  await vi.waitFor(() => {
    expect(posted.find((entry) => entry.url.includes("/score-overrides/ovr-1"))).toBeUndefined()
  })

  fireEvent.change(screen.getByLabelText("決裁理由"), { target: { value: "controls verified" } })
  fireEvent.click(approve)

  await vi.waitFor(() => {
    const call = posted.find((entry) => entry.url.includes("/score-overrides/ovr-1"))
    expect(call).toBeDefined()
    expect((call!.body as { expected_version: number }).expected_version).toBe(1)
    expect((call!.body as { reject: boolean }).reject).toBe(false)
  })
})

test("factors that do not add up to the score are called out, not quietly displayed", async () => {
  mockAPI()
  await renderDetail()

  // reconciled=false is the one signal that the explanation cannot be trusted.
  const alerts = await screen.findAllByRole("alert")
  expect(alerts.some((node) => (node.textContent ?? "").includes("7"))).toBe(true)
})

test("the applied rule set is shown with the digest that produced the score", async () => {
  mockAPI()
  await renderDetail()

  const applied = await screen.findByTestId("score-applied-rule-set")
  expect(applied.textContent).toContain("cdd_basic")
  expect(applied.textContent).toContain("abc123def456")
})
