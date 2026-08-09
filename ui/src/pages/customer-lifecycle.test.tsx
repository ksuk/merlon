import { screen } from "@testing-library/react"
import { expect, test, vi, beforeEach } from "vitest"
import { MemoryRouter } from "react-router"
import { renderWithI18n } from "@/test/i18n-test-utils"
import { CustomersPage } from "./customers"
import { customerStatusVariant } from "@/lib/utils"

// #75: "the lifecycle state cannot be mistaken for active". Every state
// rendered with the same outline badge, so a frozen or closed customer looked
// exactly like an active one on the field that decides whether they are
// evaluated at all.

function customer(id: string, status: string) {
  return {
    id,
    external_id: `EXT-${id}`,
    customer_type: "individual",
    country_code: "JP",
    product_types: [],
    attributes: { name: `Customer ${id}` },
    status,
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  }
}

beforeEach(() => {
  vi.restoreAllMocks()
})

test("each lifecycle state gets its own badge treatment", () => {
  // Distinctness is the requirement: active must not share a variant with any
  // state that means the customer is not operating normally.
  const active = customerStatusVariant("active")
  for (const status of ["dormant", "frozen", "closed"]) {
    expect(customerStatusVariant(status)).not.toBe(active)
  }
  expect(new Set(["active", "dormant", "frozen", "closed"].map(customerStatusVariant)).size).toBe(4)
})

test("the customer list renders the lifecycle state with a non-default variant", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(() =>
    Promise.resolve(
      new Response(
        JSON.stringify({
          data: [customer("c1", "frozen"), customer("c2", "active")],
          pagination: { has_more: false },
        }),
      ),
    ),
  )

  await renderWithI18n(
    <MemoryRouter>
      <CustomersPage />
    </MemoryRouter>,
  )

  const frozen = await screen.findByTestId("customer-status-c1")
  const active = screen.getByTestId("customer-status-c2")
  expect(frozen.className).not.toBe(active.className)
  expect(frozen.textContent).toContain("凍結")
})
