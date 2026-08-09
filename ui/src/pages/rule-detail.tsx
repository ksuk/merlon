import { RuleDiffView } from "@/components/audit/rule-diff-view"
import { CapabilityNotice } from "@/components/capability-notice"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useCan } from "@/hooks/use-session"
import { api, type RuleDefinition } from "@/lib/api"
import { translateApiError } from "@/lib/errors"
import { ArrowLeft, Download } from "lucide-react"
import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useParams } from "react-router"

/**
 * definitionDiff reproduces the server's own diff semantics
 * (diffRuleDefinitions in api/internal/server/rules.go): a top-level key
 * comparison, encoded the way the audit trail records it, so the comparison an
 * operator sees before activating matches the one the audit log will show
 * afterwards. Reusing RuleDiffView rather than writing a second renderer keeps
 * the two from drifting.
 */
function definitionDiff(before: unknown, after: unknown): Record<string, string> {
  const a = (before ?? {}) as Record<string, unknown>
  const b = (after ?? {}) as Record<string, unknown>
  const changes: Record<string, { before?: unknown; after?: unknown }> = {}

  for (const key of new Set([...Object.keys(a), ...Object.keys(b)])) {
    const left = a[key]
    const right = b[key]
    if (JSON.stringify(left) !== JSON.stringify(right)) {
      changes[key] = { before: left, after: right }
    }
  }
  return { diff: JSON.stringify(changes) }
}

function DefinitionEntries({ definition }: { definition: unknown }) {
  const { t } = useTranslation()
  const entries = Object.entries((definition ?? {}) as Record<string, unknown>)

  if (entries.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("ruleDetail.definition.empty")}</p>
  }

  return (
    <dl className="grid gap-2 sm:grid-cols-[12rem_1fr]">
      {entries.map(([key, value]) => (
        <div key={key} className="contents">
          <dt className="font-mono text-xs text-muted-foreground">{key}</dt>
          <dd>
            <pre className="whitespace-pre-wrap break-all font-mono text-xs">
              {typeof value === "string" ? value : JSON.stringify(value, null, 2)}
            </pre>
          </dd>
        </div>
      ))}
    </dl>
  )
}

export function RuleDetailPage() {
  const { t } = useTranslation()
  const { name } = useParams<{ name: string }>()
  const canWrite = useCan("rules.write")

  const [rule, setRule] = useState<RuleDefinition | null>(null)
  const [comparison, setComparison] = useState<RuleDefinition | null>(null)
  const [compareVersion, setCompareVersion] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    if (!name) return
    setLoading(true)
    try {
      const current = await api.rules.get(name)
      setRule(current)
      setError(null)
      // The previous version is the comparison an operator wants by default:
      // "what changed to get here".
      setCompareVersion(current.version > 1 ? current.version - 1 : null)
    } catch (err: unknown) {
      setError(translateApiError(err, t))
    } finally {
      setLoading(false)
    }
  }, [name, t])

  useEffect(() => {
    void Promise.resolve().then(load)
  }, [load])

  useEffect(() => {
    let cancelled = false
    void Promise.resolve().then(async () => {
      if (cancelled) return
      if (!name || compareVersion === null) {
        setComparison(null)
        return
      }
      try {
        const value = await api.rules.get(name, compareVersion)
        if (!cancelled) setComparison(value)
      } catch {
        // A version the store no longer holds is reported as an absent
        // comparison rather than as a page failure: the current definition is
        // still worth reading.
        if (!cancelled) setComparison(null)
      }
    })
    return () => {
      cancelled = true
    }
  }, [name, compareVersion])

  if (loading) {
    return <div className="h-40 animate-pulse rounded-lg bg-muted" />
  }

  if (error || !rule) {
    return (
      <div role="alert" className="p-12 text-center text-destructive">
        {error ?? t("ruleDetail.notFound")}
      </div>
    )
  }

  const versions = Array.from({ length: rule.version }, (_, i) => i + 1)

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <Link to="/rules" className="mb-1 inline-flex items-center gap-1 text-xs text-muted-foreground underline">
            <ArrowLeft className="h-3 w-3" aria-hidden="true" />
            {t("ruleDetail.backToRules")}
          </Link>
          <h1 className="text-2xl font-bold tracking-tight">{rule.name}</h1>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={rule.is_active ? "low" : "outline"}>
            {rule.is_active ? t("rules.status.active") : t("rules.status.inactive")}
          </Badge>
          <a
            className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs"
            href={api.rules.exportUrl(rule.name, "json")}
          >
            <Download className="h-3 w-3" aria-hidden="true" />
            JSON
          </a>
          <a
            className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs"
            href={api.rules.exportUrl(rule.name, "yaml")}
          >
            <Download className="h-3 w-3" aria-hidden="true" />
            YAML
          </a>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("ruleDetail.identity.title")}</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid gap-3 sm:grid-cols-2">
            <div>
              <dt className="text-xs text-muted-foreground">{t("ruleDetail.identity.canonicalId")}</dt>
              {/* name+version is what the API resolves and what provenance
                  records; the row's primary key is regenerated on every version
                  insert and is not a stable identifier. */}
              <dd className="font-mono text-sm">
                {rule.name} @ v{rule.version}
              </dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t("ruleDetail.identity.type")}</dt>
              <dd className="text-sm">{rule.type}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t("ruleDetail.identity.createdBy")}</dt>
              <dd className="text-sm">{rule.created_by || t("ruleDetail.identity.unknownAuthor")}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t("ruleDetail.identity.updatedAt")}</dt>
              <dd className="text-sm">{rule.updated_at}</dd>
            </div>
            <div className="sm:col-span-2">
              <dt className="text-xs text-muted-foreground">{t("ruleDetail.identity.description")}</dt>
              <dd className="text-sm">{rule.description || t("ruleDetail.identity.noDescription")}</dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("ruleDetail.definition.title")}</CardTitle>
        </CardHeader>
        <CardContent>
          <DefinitionEntries definition={rule.definition} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("ruleDetail.compare.title")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {rule.version === 1 ? (
            <p className="text-sm text-muted-foreground">{t("ruleDetail.compare.onlyVersion")}</p>
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-xs text-muted-foreground">{t("ruleDetail.compare.against")}</span>
                {versions
                  .filter((v) => v !== rule.version)
                  .map((v) => (
                    <button
                      key={v}
                      type="button"
                      aria-pressed={compareVersion === v}
                      onClick={() => setCompareVersion(v)}
                      className={`rounded-md border px-2 py-1 text-xs ${
                        compareVersion === v
                          ? "border-primary bg-primary/10 text-primary"
                          : "border-input text-muted-foreground hover:bg-accent"
                      }`}
                    >
                      v{v}
                    </button>
                  ))}
              </div>
              {comparison ? (
                <RuleDiffView details={definitionDiff(comparison.definition, rule.definition)} />
              ) : (
                <p className="text-sm text-muted-foreground">{t("ruleDetail.compare.unavailable")}</p>
              )}
            </>
          )}
        </CardContent>
      </Card>

      {!canWrite && <CapabilityNotice capabilityId="rules.write" />}
    </div>
  )
}
