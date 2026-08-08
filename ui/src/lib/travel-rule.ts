import type { Transaction } from "@/lib/api"

// The four states a reviewer has to be able to tell apart. `not_assessed` is
// the one the previous rendering could not express: a transaction accepted
// before the policy existed carries no verdict at all, which is neither a
// verdict of "not applicable" nor an unknown counterparty.
export type TravelRuleState = "not_assessed" | "not_applicable" | "complete" | "incomplete"

export const TRAVEL_RULE_STATE_VARIANT: Record<TravelRuleState, "secondary" | "outline" | "low" | "critical"> = {
  not_assessed: "outline",
  not_applicable: "secondary",
  complete: "low",
  incomplete: "critical",
}

export function travelRuleStateOf(transaction: Transaction): TravelRuleState {
  const assessment = transaction.travel_rule_assessment
  if (!assessment) return "not_assessed"
  if (!assessment.applicable) return "not_applicable"
  return (assessment.missing_fields?.length ?? 0) > 0 || transaction.travel_rule_status === "incomplete"
    ? "incomplete"
    : "complete"
}
