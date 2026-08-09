import { useCallback, useEffect, useState } from "react"

interface UseApiResult<T> {
  data: T | null
  /**
   * The failure message, kept for the twenty pages that render it directly.
   */
  error: string | null
  /**
   * The original thrown value.
   *
   * The hook used to collapse every failure to `err.message`, which discarded
   * the ApiError carrying the status, the stable error_code and the request ID.
   * That is why pages could not classify a failure or show a correlation
   * identifier: the information never reached them (#85).
   */
  cause: unknown
  loading: boolean
  /**
   * Re-runs the request. Reads are idempotent, so offering this is safe; a
   * mutation must not be retried through this hook.
   */
  refetch: () => void
}

// dependencyKey lets a page re-run a request when its explicit scope changes
// while preserving the one-shot behavior used by the existing pages.
export function useApi<T>(fetcher: () => Promise<T>, dependencyKey?: unknown): UseApiResult<T> {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [cause, setCause] = useState<unknown>(null)
  const [loading, setLoading] = useState(true)
  const [attempt, setAttempt] = useState(0)

  const refetch = useCallback(() => setAttempt((value) => value + 1), [])

  useEffect(() => {
    let cancelled = false
    // Defer the state update so dependency changes show a loading state
    // without performing a synchronous setState inside the effect body.
    void Promise.resolve().then(async () => {
      if (cancelled) return
      setLoading(true)
      try {
        const result = await fetcher()
        if (!cancelled) {
          setData(result)
          setError(null)
          setCause(null)
        }
      } catch (err: unknown) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err))
          setCause(err)
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    })
    return () => {
      cancelled = true
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps -- fetcher is scoped by the explicit dependency key
  }, [dependencyKey, attempt])

  return { data, error, cause, loading, refetch }
}
