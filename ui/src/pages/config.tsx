import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  api,
  type ConfigValidationError,
  type ConfigValidationResult,
  type RuleDefinition,
  type RuleType,
} from "@/lib/api"
import { translateApiError } from "@/lib/errors"
import { AlertTriangle, CheckCircle, Settings2, XCircle } from "lucide-react"
import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

// configType is the value the engine switches on
// (api/internal/engine/native/native.go). The page previously offered
// "scenario_rules", which the engine has never accepted: choosing it always
// produced "unknown config type". ruleType is the stored rule kind of the same
// configuration, and is what makes an existing version loadable as a baseline.
const CONFIG_TYPES: { value: string; labelKey: string; ruleType: RuleType; docs: string }[] = [
  { value: "cdd_weights", labelKey: "config.types.cddWeights", ruleType: "CDD_WEIGHT", docs: "/docs/configuration" },
  { value: "tm_scenarios", labelKey: "config.types.scenarioRules", ruleType: "TM_SCENARIO", docs: "/docs/rule-authoring" },
  { value: "screening_lists", labelKey: "config.types.screeningLists", ruleType: "SCREENING_CONFIG", docs: "/docs/configuration" },
  { value: "country_risk", labelKey: "config.types.countryRisk", ruleType: "COUNTRY_RISK", docs: "/docs/configuration" },
]

function classBadgeVariant(entry: ConfigValidationError): "destructive" | "outline" {
  return entry.severity === "warning" ? "outline" : "destructive"
}

/** Renders one finding with everything needed to act on it. */
function Finding({ entry }: { entry: ConfigValidationError }) {
  const { t } = useTranslation()
  const warning = entry.severity === "warning"

  return (
    <li className={`rounded-md p-3 text-sm ${warning ? "bg-amber-50" : "bg-red-50"}`}>
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={classBadgeVariant(entry)}>
          {entry.class
            ? t(`config.result.class.${entry.class}`, { defaultValue: entry.class })
            : t("config.result.class.unknown")}
        </Badge>
        {/* The path is what the operator edits; the legacy coarse field name is
            kept beside it because support requests still quote it. */}
        {entry.path ? <code className="font-mono text-xs">{entry.path}</code> : null}
        {entry.line ? (
          <span className="text-xs text-muted-foreground">
            {entry.column
              ? t("config.result.atLineColumn", { line: entry.line, column: entry.column })
              : t("config.result.atLine", { line: entry.line })}
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">{t("config.result.noPosition")}</span>
        )}
        <span className="font-mono text-xs text-muted-foreground">{entry.field}</span>
      </div>
      <p className={`mt-1 ${warning ? "text-amber-900" : "text-red-800"}`}>{entry.message}</p>
    </li>
  )
}

export function ConfigPage() {
  const { t } = useTranslation()
  const [configType, setConfigType] = useState(CONFIG_TYPES[0].value)
  const [yamlContent, setYamlContent] = useState("")
  const [baseline, setBaseline] = useState<{ name: string; version: number; content: string } | null>(null)
  const [candidates, setCandidates] = useState<RuleDefinition[] | null>(null)
  const [loadingBaseline, setLoadingBaseline] = useState(false)
  const [validating, setValidating] = useState(false)
  const [result, setResult] = useState<ConfigValidationResult | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const selected = CONFIG_TYPES.find((ct) => ct.value === configType) ?? CONFIG_TYPES[0]

  // Editing starts from something the deployment actually runs. A blank box
  // invites a paste that silently replaces a configuration nobody looked at.
  const loadCandidates = useCallback(async (ruleType: RuleType) => {
    setCandidates(null)
    try {
      const page = await api.rules.list({ type: ruleType, limit: 50 })
      setCandidates(page.data)
    } catch {
      setCandidates([])
    }
  }, [])

  useEffect(() => {
    // Deferred out of the effect's synchronous phase, matching the pattern the
    // other pages use: a setState here cascades an extra render.
    void Promise.resolve().then(() => {
      setBaseline(null)
      setResult(null)
      setNotice(null)
      setError(null)
      return loadCandidates(selected.ruleType)
    })
  }, [selected.ruleType, loadCandidates])

  async function loadBaseline(rule: RuleDefinition) {
    setLoadingBaseline(true)
    setError(null)
    try {
      const response = await fetch(api.rules.exportUrl(rule.name, "yaml"), { credentials: "include" })
      if (!response.ok) {
        throw new Error(`${response.status}`)
      }
      const content = await response.text()
      setBaseline({ name: rule.name, version: rule.version, content })
      setYamlContent(content)
      setResult(null)
      setNotice(null)
    } catch (err: unknown) {
      setError(translateApiError(err, t))
    } finally {
      setLoadingBaseline(false)
    }
  }

  async function handleValidate(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    setNotice(null)

    const yaml = yamlContent.trim()
    if (!yaml) {
      // An empty submission used to do nothing at all, which is
      // indistinguishable from a request that succeeded.
      setResult(null)
      setNotice(t("config.result.emptyInput"))
      return
    }
    if (baseline && yaml === baseline.content.trim()) {
      setResult(null)
      setNotice(t("config.result.unchanged", { name: baseline.name, version: baseline.version }))
      return
    }

    setValidating(true)
    setResult(null)
    try {
      setResult(await api.config.validate(configType, yaml))
    } catch (err: unknown) {
      setError(translateApiError(err, t))
    } finally {
      setValidating(false)
    }
  }

  const warnings = result?.warnings ?? []

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{t("config.title")}</h1>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <Settings2 className="h-4 w-4" />
            {t("config.form.title")}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleValidate} className="space-y-4">
            <div>
              <label className="mb-2 block text-sm font-medium">{t("config.form.typeLabel")}</label>
              <div className="flex flex-wrap gap-2">
                {CONFIG_TYPES.map((ct) => (
                  <button
                    key={ct.value}
                    type="button"
                    aria-pressed={configType === ct.value}
                    onClick={() => setConfigType(ct.value)}
                    className={`rounded-md border px-3 py-1 text-xs font-medium transition-colors ${
                      configType === ct.value
                        ? "border-primary bg-primary/10 text-primary"
                        : "border-input text-muted-foreground hover:bg-accent"
                    }`}
                  >
                    {t(ct.labelKey)}
                  </button>
                ))}
              </div>
              <p className="mt-2 text-xs text-muted-foreground">
                <a className="underline" href={selected.docs}>
                  {t("config.form.schemaDocs")}
                </a>
              </p>
            </div>

            <div className="rounded-md border bg-muted/30 p-3">
              <p className="text-sm font-medium">{t("config.baseline.title")}</p>
              <p className="mt-1 text-xs text-muted-foreground">{t("config.baseline.help")}</p>
              {candidates === null ? (
                <div className="mt-2 h-6 w-40 animate-pulse rounded bg-muted" />
              ) : candidates.length === 0 ? (
                <p className="mt-2 text-xs text-muted-foreground">{t("config.baseline.none")}</p>
              ) : (
                <div className="mt-2 flex flex-wrap gap-2">
                  {candidates.map((rule) => (
                    <Button
                      key={rule.id}
                      type="button"
                      size="sm"
                      variant="outline"
                      disabled={loadingBaseline}
                      onClick={() => void loadBaseline(rule)}
                    >
                      {t("config.baseline.load", { name: rule.name, version: rule.version })}
                    </Button>
                  ))}
                </div>
              )}
              {baseline ? (
                <p className="mt-2 text-xs" data-testid="config-baseline">
                  {t("config.baseline.current", { name: baseline.name, version: baseline.version })}
                </p>
              ) : (
                <p className="mt-2 text-xs text-amber-700">{t("config.baseline.unset")}</p>
              )}
            </div>

            <div>
              <label className="mb-1 block text-sm font-medium" htmlFor="config-yaml">
                {t("config.form.yamlLabel")}
              </label>
              <textarea
                id="config-yaml"
                value={yamlContent}
                onChange={(e) => setYamlContent(e.target.value)}
                rows={14}
                placeholder={t("config.form.yamlPlaceholder")}
                className="w-full rounded-md border bg-background px-3 py-2 font-mono text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
            </div>
            <Button type="submit" size="sm" disabled={validating}>
              {validating ? t("config.form.validating") : t("config.form.submit")}
            </Button>
          </form>
        </CardContent>
      </Card>

      {error && (
        <div role="alert" className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
          {error}
        </div>
      )}

      {notice && (
        <div role="status" className="rounded-md border bg-muted/40 p-3 text-sm">
          {notice}
        </div>
      )}

      {result && (
        <Card className={result.valid ? "border-green-200" : "border-red-200"}>
          <CardContent className="p-4">
            <div className="mb-3 flex flex-wrap items-center gap-2">
              {result.valid ? (
                <>
                  <CheckCircle className="h-5 w-5 text-green-600" aria-hidden="true" />
                  <span className="font-medium text-green-800">{t("config.result.successTitle")}</span>
                  <Badge variant="low">{t("config.result.validBadge")}</Badge>
                </>
              ) : (
                <>
                  <XCircle className="h-5 w-5 text-red-600" aria-hidden="true" />
                  <span className="font-medium text-red-800">{t("config.result.errorTitle")}</span>
                  <Badge variant="destructive">{t("config.result.errorCount", { count: result.errors.length })}</Badge>
                </>
              )}
              {warnings.length > 0 && (
                <Badge variant="outline" className="border-amber-500/40 text-amber-700">
                  {t("config.result.warningCount", { count: warnings.length })}
                </Badge>
              )}
            </div>

            {result.errors.length > 0 && (
              <ul className="space-y-2">
                {result.errors.map((entry, i) => (
                  <Finding key={`e${i}`} entry={entry} />
                ))}
              </ul>
            )}

            {warnings.length > 0 && (
              <div className="mt-3">
                <p className="mb-1 flex items-center gap-1 text-xs text-amber-700">
                  <AlertTriangle className="h-3 w-3" aria-hidden="true" />
                  {t("config.result.warningsDoNotBlock")}
                </p>
                <ul className="space-y-2">
                  {warnings.map((entry, i) => (
                    <Finding key={`w${i}`} entry={{ ...entry, severity: "warning" }} />
                  ))}
                </ul>
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  )
}
