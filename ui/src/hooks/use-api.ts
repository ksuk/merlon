import { useEffect, useState } from "react"

interface UseApiResult<T> {
  data: T | null
  error: string | null
  loading: boolean
}

// dependencyKey lets a page re-run a request when its explicit scope changes
// while preserving the one-shot behavior used by the existing pages.
export function useApi<T>(fetcher: () => Promise<T>, dependencyKey?: unknown): UseApiResult<T> {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

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
        }
      } catch (err: unknown) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err))
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    })
    return () => {
      cancelled = true
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps -- fetcher is scoped by the explicit dependency key
  }, [dependencyKey])

  return { data, error, loading }
}
