import { Badge } from "@/components/ui/badge"
import { Button, buttonVariants } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { cn } from "@/lib/utils"
import { api, type RuleDefinition, type RuleType } from "@/lib/api"
import { Download, FileUp, Plus, PowerOff } from "lucide-react"
import { useEffect, useRef, useState } from "react"

const RULE_TYPES: { value: RuleType; label: string }[] = [
  { value: "TM_SCENARIO", label: "TMシナリオ" },
  { value: "CDD_WEIGHT", label: "CDD重み付け" },
  { value: "SCREENING_CONFIG", label: "スクリーニング設定" },
  { value: "COUNTRY_RISK", label: "国別リスク" },
]

function ruleTypeLabel(t: RuleType) {
  return RULE_TYPES.find((rt) => rt.value === t)?.label ?? t
}

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

export function RulesPage() {
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
      setError(err instanceof Error ? err.message : String(err))
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
      setImportError("JSONの解析に失敗しました")
      return
    }

    setImporting(true)
    setImportError(null)
    try {
      await api.rules.import(items)
      setShowImport(false)
      await reload()
    } catch (err) {
      setImportError(err instanceof Error ? err.message : String(err))
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
        <h1 className="text-2xl font-bold tracking-tight">ルール管理</h1>
        {isAdmin && (
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => setShowImport(!showImport)}>
              <FileUp className="h-4 w-4" />
              インポート
            </Button>
            <Button size="sm" onClick={() => setShowCreate(!showCreate)}>
              <Plus className="h-4 w-4" />
              新規作成
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
            全て
          </button>
          {RULE_TYPES.map((t) => (
            <button
              key={t.value}
              type="button"
              onClick={() => setTypeFilter(t.value)}
              className={`rounded-md border px-3 py-1 text-xs font-medium transition-colors ${
                typeFilter === t.value
                  ? "border-primary bg-primary/10 text-primary"
                  : "border-input text-muted-foreground hover:bg-accent"
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={activeOnly}
            onChange={(e) => setActiveOnly(e.target.checked)}
          />
          有効のみ
        </label>
      </div>

      {showCreate && isAdmin && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">ルール作成</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="mb-2 block text-sm font-medium">種別</label>
                <div className="flex gap-2">
                  {RULE_TYPES.map((t) => (
                    <button
                      key={t.value}
                      type="button"
                      onClick={() => setCreateType(t.value)}
                      className={`rounded-md border px-3 py-1 text-xs font-medium transition-colors ${
                        createType === t.value
                          ? "border-primary bg-primary/10 text-primary"
                          : "border-input text-muted-foreground hover:bg-accent"
                      }`}
                    >
                      {t.label}
                    </button>
                  ))}
                </div>
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium">名前</label>
                <input
                  ref={nameRef}
                  required
                  placeholder="rule_name"
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium">定義（JSON）</label>
                <textarea
                  ref={definitionRef}
                  required
                  rows={8}
                  placeholder={'{"schema_version": "1.0"}'}
                  className="w-full rounded-md border bg-background px-3 py-2 font-mono text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <Button type="submit" size="sm" disabled={creating}>
                作成
              </Button>
            </form>
          </CardContent>
        </Card>
      )}

      {showImport && isAdmin && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">ルール一括インポート</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleImport} className="space-y-4">
              <div>
                <label className="mb-1 block text-sm font-medium">JSON配列</label>
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
                インポート
              </Button>
            </form>
          </CardContent>
        </Card>
      )}

      {loading ? (
        <div className="h-48 animate-pulse rounded-xl border bg-muted" />
      ) : error ? (
        <p className="p-12 text-center text-destructive">ルールの取得に失敗しました</p>
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
                        {rule.is_active ? "有効" : "無効"}
                      </Badge>
                      <span className="text-xs text-muted-foreground">v{rule.version}</span>
                    </div>
                    {rule.description && (
                      <p className="text-xs text-muted-foreground">{rule.description}</p>
                    )}
                    <p className="text-xs text-muted-foreground">
                      更新: {formatDateTime(rule.updated_at)}
                    </p>
                  </div>
                  <div className="flex gap-1">
                    <a
                      href={api.rules.exportUrl(rule.name)}
                      download={`${rule.name}.json`}
                      className={cn(buttonVariants({ variant: "ghost", size: "sm" }))}
                    >
                      <Download className="h-4 w-4" />
                      エクスポート
                    </a>
                    {isAdmin && (
                      <Button variant="ghost" size="sm" onClick={() => handleToggleActive(rule)}>
                        <PowerOff className="h-4 w-4" />
                        {rule.is_active ? "無効化" : "有効化"}
                      </Button>
                    )}
                  </div>
                </CardContent>
              </Card>
            ))
          ) : (
            <Card>
              <CardContent className="p-8 text-center text-sm text-muted-foreground">
                ルールが登録されていません
              </CardContent>
            </Card>
          )}
        </div>
      )}
    </div>
  )
}
