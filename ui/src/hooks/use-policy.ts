import { useCallback } from "react"
import { useApi } from "@/hooks/use-api"
import { api, type PolicyDocument, type PolicyName } from "@/lib/api"

// usePolicy reads one of the server's policy documents (ADR-0016). Pages use
// it instead of restating a threshold, required field, stage schedule or
// reason code, so a screen can never claim a rule the server does not apply.
// A failed read leaves data null; callers fall back to showing nothing rather
// than to a guessed value.
export function usePolicy<N extends PolicyName>(name: N): {
  data: PolicyDocument<N> | null
  error: string | null
  loading: boolean
} {
  return useApi(useCallback(() => api.policies.get(name), [name]), name)
}
