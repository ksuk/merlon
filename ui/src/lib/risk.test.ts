import { expect, test } from "vitest"
import { compareRiskValues, riskRank } from "./risk"

test("riskRank places known levels above unknown values", () => {
  expect(riskRank("critical")).toBeGreaterThan(riskRank("high"))
  expect(riskRank("high")).toBeGreaterThan(riskRank("medium"))
  expect(riskRank("medium")).toBeGreaterThan(riskRank("low"))
  expect(riskRank("future_level")).toBe(0)
})

test("compareRiskValues uses created_at and id as deterministic tie-breakers", () => {
  const newer = { risk: "high", created_at: "2026-08-02T00:00:00Z", id: "a" }
  const older = { risk: "high", created_at: "2026-08-01T00:00:00Z", id: "z" }
  expect(compareRiskValues(newer, older)).toBeLessThan(0)
  expect(compareRiskValues({ ...newer, risk: "critical" }, newer)).toBeLessThan(0)
})
