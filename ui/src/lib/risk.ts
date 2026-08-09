const RISK_RANK: Record<string, number> = {
  critical: 4,
  high: 3,
  medium: 2,
  low: 1,
}

// Unknown values remain visible but sort below every known AML risk level.
export function riskRank(value: string): number {
  return RISK_RANK[value] ?? 0
}

export function compareRiskValues(
  left: { risk: string; created_at: string; id: string },
  right: { risk: string; created_at: string; id: string },
): number {
  const rankDelta = riskRank(right.risk) - riskRank(left.risk)
  if (rankDelta !== 0) return rankDelta
  const createdDelta = right.created_at.localeCompare(left.created_at)
  if (createdDelta !== 0) return createdDelta
  return right.id.localeCompare(left.id)
}
