import { Badge } from "@/components/ui/badge"
import { Button, buttonVariants } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { cn } from "@/lib/utils"
import { api, type RuleDefinition, type RuleType } from "@/lib/api"
import { translateApiError } from "@/lib/errors"
import { Download, FileUp, Plus, PowerOff } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

export function RulesPage() {
  const { t, i18n } = useTranslation()
  const ruleTypes: { value: RuleType; label: string }[] = [
    { value: "TM_SCENARIO", label: t("rules.types.tmScenario") },
    { value: "CDD_WEIGHT", label: t("rules.types.cddWeight") },
    { value: "SCREENING_CONFIG", label: t("rules.types.screeningConfig") },
    { value: "COUNTRY_RISK", label: t("rules.types.countryRisk") },
  ]
  function ruleTypeLabel(rt: RuleType) {
    return ruleTypes.find((entry) => entry.value === rt)?.label ?? rt
  }
  const { data: user } = useApi(api.auth.me)
  const isAdmin = user?.role === "admin"

  const [typeFilter, setTypeFilter] = useState<RuleType | "">("")
  const [activeOnly, setActiveOnly] = useState(false)
  const [rules, setRules] = useState<RuleDefinition[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [showCreate, setShowCreate] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createType, setCreateType] = useState<RuleType>("CDD_WEIGHT")
  const nameRef = useRef<HTMLInputElement>(null)
  const definitionRef = useRef<HTMLTextAreaElement>(null)

  const [showImport, setShowImport] = useState(false)
  const [importing, setImporting] = useState(false)
  const [importError, setImportError] = useState<string | null>(null)
  const importRef = useRef<HTMLTextAreaElement>(null)

  async function reload() {
    setLoading(true)
    try {
      const res = await api.rules.list({ type: typeFilter || undefined, activeOnly })
      setRules(res.data)
      setError(null)
    } catch (err) {
      setError(translateApiError(err, t))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [typeFilter, activeOnly])

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    const name = nameRef.current?.value.trim()
    const definitionText = definitionRef.current?.value.trim()
    if (!name || !definitionText) return

    let definition: unknown
    try {
      definition = JSON.parse(definitionText)
    } catch {
      return
    }

    setCreating(true)
    try {
      await api.rules.create({ type: createType, name, definition })
      setShowCreate(false)
      await reload()
    } finally {
      setCreating(false)
    }
  }

  async function handleImport(e: React.FormEvent) {
    e.preventDefault()
    const text = importRef.current?.value.trim()
    if (!text) return

    let items
    try {
      items = JSON.parse(text)
    } catch {
      setImportError(t("rules.import.error"))
      return
    }

    setImporting(true)
    setImportError(null)
    try {
      await api.rules.import(items)
      setShowImport(false)
      await reload()
    } catch (err) {
      setImportError(translateApiError(err, t))
    } finally {
      setImporting(false)
    }
  }

  async function handleToggleActive(rule: RuleDefinition) {
    if (rule.is_active) {
      await api.rules.deactivate(rule.name)
    } else {
      await api.rules.activate(rule.name)
    }
    await reload()
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">{t("rules.title")}</h1>
        {isAdmin && (
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => setShowImport(!showImport)}>
              <FileUp className="h-4 w-4" />
              {t("rules.actions.import")}
            </Button>
            <Button size="sm" onClick={() => setShowCreate(!showCreate)}>
              <Plus className="h-4 w-4" />
              {t("rules.actions.create")}
            </Button>
          </div>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-4">
        <div className="flex gap-2">
          <button
            type="button"
            onClick={() => setTypeFilter("")}
            className={`rounded-md border px-3 py-1 text-xs font-medium transition-colors ${
              typeFilter === ""
                ? "border-primary bg-primary/10 text-primary"
                : "border-input text-muted-foreground hover:bg-accent"
            }`}
          >
            {t("rules.filter.all")}
          </button>
          {ruleTypes.map((rt) => (
            <button
              key={rt.value}
              type="button"
              onClick={() => setTypeFilter(rt.value)}
              className={`rounded-md border px-3 py-1 text-xs font-medium transition-colors ${
                typeFilter === rt.value
                  ? "border-primary bg-primary/10 text-primary"
                  : "border-input text-muted-foreground hover:bg-accent"
              }`}
            >
              {rt.label}
            </button>
          ))}
        </div>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={activeOnly}
            onChange={(e) => setActiveOnly(e.target.checked)}
          />
          {t("rules.filter.activeOnly")}
        </label>
      </div>

      {showCreate && isAdmin && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("rules.create.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="mb-2 block text-sm font-medium">{t("rules.create.typeLabel")}</label>
                <div className="flex gap-2">
                  {ruleTypes.map((rt) => (
                    <button
                      key={rt.value}
                      type="button"
                      onClick={() => setCreateType(rt.value)}
                      className={`rounded-md border px-3 py-1 text-xs font-medium transition-colors ${
                        createType === rt.value
                          ? "border-primary bg-primary/10 text-primary"
                          : "border-input text-muted-foreground hover:bg-accent"
                      }`}
                    >
                      {rt.label}
                    </button>
                  ))}
                </div>
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium">{t("rules.create.nameLabel")}</label>
                <input
                  ref={nameRef}
                  required
                  placeholder={t("rules.create.namePlaceholder")}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium">{t("rules.create.definitionLabel")}</label>
                <textarea
                  ref={definitionRef}
                  required
                  rows={8}
                  placeholder={'{"schema_version": "1.0"}'}
                  className="w-full rounded-md border bg-background px-3 py-2 font-mono text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <Button type="submit" size="sm" disabled={creating}>
                {t("rules.create.submit")}
              </Button>
            </form>
          </CardContent>
        </Card>
      )}

      {showImport && isAdmin && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("rules.import.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleImport} className="space-y-4">
              <div>
                <label className="mb-1 block text-sm font-medium">{t("rules.import.jsonArrayLabel")}</label>
                <textarea
                  ref={importRef}
                  required
                  rows={10}
                  placeholder='[{"type":"COUNTRY_RISK","name":"country_risk_sample","definition":{...}}]'
                  className="w-full rounded-md border bg-background px-3 py-2 font-mono text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              {importError && <p className="text-sm text-destructive">{importError}</p>}
              <Button type="submit" size="sm" disabled={importing}>
                {t("rules.import.submit")}
              </Button>
            </form>
          </CardContent>
        </Card>
      )}

      {loading ? (
        <div className="h-48 animate-pulse rounded-xl border bg-muted" />
      ) : error ? (
        <p className="p-12 text-center text-destructive">{t("rules.error")}</p>
      ) : (
        <div className="space-y-3">
          {rules && rules.length > 0 ? (
            rules.map((rule) => (
              <Card key={`${rule.name}-${rule.version}`}>
                <CardContent className="flex items-center justify-between p-4">
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium">{rule.name}</span>
                      <Badge variant="outline">{ruleTypeLabel(rule.type)}</Badge>
                      <Badge variant={rule.is_active ? "low" : "secondary"}>
                        {rule.is_active ? t("rules.status.active") : t("rules.status.inactive")}
                      </Badge>
                      <span className="text-xs text-muted-foreground">v{rule.version}</span>
                    </div>
                    {rule.description && (
                      <p className="text-xs text-muted-foreground">{rule.description}</p>
                    )}
                    <p className="text-xs text-muted-foreground">
                      {t("rules.entry.updatedAt", { date: formatDateTime(rule.updated_at, i18n.language) })}
                    </p>
                  </div>
                  <div className="flex gap-1">
                    <a
                      href={api.rules.exportUrl(rule.name)}
                      download={`${rule.name}.json`}
                      className={cn(buttonVariants({ variant: "ghost", size: "sm" }))}
                    >
                      <Download className="h-4 w-4" />
                      {t("rules.entry.export")}
                    </a>
                    {isAdmin && (
                      <Button variant="ghost" size="sm" onClick={() => handleToggleActive(rule)}>
                        <PowerOff className="h-4 w-4" />
                        {rule.is_active ? t("rules.entry.deactivate") : t("rules.entry.activate")}
                      </Button>
                    )}
                  </div>
                </CardContent>
              </Card>
            ))
          ) : (
            <Card>
              <CardContent className="p-8 text-center text-sm text-muted-foreground">
                {t("rules.empty")}
              </CardContent>
            </Card>
          )}
        </div>
      )}
    </div>
  )
}
