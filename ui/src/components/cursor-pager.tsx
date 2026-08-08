import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"

// Cursor paging state, extracted so a list does not have to reimplement the
// back-stack. Four Wave 3 screens shipped with a hardcoded limit and no way
// to reach page two at all, which means a queue silently ends wherever the
// first page did -- the failure mode #57 already fixed once for the older
// lists.
export function useCursorPager(resetKey: string) {
  const [cursor, setCursor] = useState("")
  const [history, setHistory] = useState<string[]>([])
  const [lastResetKey, setLastResetKey] = useState(resetKey)

  // Filters changing invalidates the cursor: a cursor is only meaningful
  // against the filter set that produced it.
  if (resetKey !== lastResetKey) {
    setLastResetKey(resetKey)
    setCursor("")
    setHistory([])
  }

  return {
    cursor,
    // The request key a data hook should key on, so a page turn refetches.
    requestKey: `${resetKey}|${cursor}`,
    canGoBack: history.length > 0,
    next(nextCursor: string) {
      setHistory((stack) => [...stack, cursor])
      setCursor(nextCursor)
    },
    back() {
      setHistory((stack) => {
        setCursor(stack[stack.length - 1] ?? "")
        return stack.slice(0, -1)
      })
    },
  }
}

export function CursorPager({
  pager,
  nextCursor,
  loading,
  testId,
}: {
  pager: ReturnType<typeof useCursorPager>
  nextCursor?: string | null
  loading?: boolean
  testId?: string
}) {
  const { t } = useTranslation()
  if (!pager.canGoBack && !nextCursor) return null
  return (
    <div data-testid={testId ?? "cursor-pager"} className="flex items-center justify-center gap-3 py-2 text-sm">
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={!pager.canGoBack || loading}
        onClick={() => pager.back()}
      >
        {t("list.previous")}
      </Button>
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={!nextCursor || loading}
        onClick={() => nextCursor && pager.next(nextCursor)}
      >
        {t("list.next")}
      </Button>
    </div>
  )
}
