import { useTranslation } from "react-i18next"

interface RuleFieldChange {
  before?: unknown
  after?: unknown
}

interface RuleDiffViewProps {
  // details is the audit_logs.details map as recorded by
  // handleUpdateRule/diffRuleDefinitions (ALD-003): a single "diff" key
  // holding a JSON-encoded { [field]: { before?, after? } } map of the
  // top-level rule Definition keys that changed.
  details?: Record<string, string>
}

function formatValue(v: unknown): string {
  if (v === undefined) return "-"
  if (typeof v === "string") return v
  return JSON.stringify(v)
}

export function RuleDiffView({ details }: RuleDiffViewProps) {
  const { t } = useTranslation()
  const raw = details?.diff
  if (!raw) {
    return <p className="text-sm text-muted-foreground">{t("audit.ruleDiff.empty")}</p>
  }

  let changes: Record<string, RuleFieldChange>
  try {
    changes = JSON.parse(raw)
  } catch {
    return <p className="text-sm text-destructive">{t("audit.ruleDiff.parseError")}</p>
  }

  const fields = Object.keys(changes)
  if (fields.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("audit.ruleDiff.noChangedFields")}</p>
  }

  return (
    <div className="space-y-2">
      {fields.map((field) => {
        const change = changes[field]
        return (
          <div
            key={field}
            className="grid grid-cols-[8rem_1fr_1fr] gap-3 rounded-md border border-amber-500/40 bg-amber-500/5 p-2 text-xs"
          >
            <div className="font-medium">{field}</div>
            <div>
              <div className="mb-1 text-muted-foreground">{t("audit.ruleDiff.before")}</div>
              <pre className="whitespace-pre-wrap break-all font-mono">{formatValue(change.before)}</pre>
            </div>
            <div>
              <div className="mb-1 text-muted-foreground">{t("audit.ruleDiff.after")}</div>
              <pre className="whitespace-pre-wrap break-all font-mono">{formatValue(change.after)}</pre>
            </div>
          </div>
        )
      })}
    </div>
  )
}
