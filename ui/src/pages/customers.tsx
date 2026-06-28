import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useApi } from "@/hooks/use-api"
import { api, type RiskTier } from "@/lib/api"
import { Plus } from "lucide-react"
import { useRef, useState } from "react"
import { Link } from "react-router-dom"

const TIER_VARIANT: Record<RiskTier, "low" | "medium" | "high"> = {
  low: "low",
  medium: "medium",
  high: "high",
}

const TIER_LABELS: Record<string, string> = {
  low: "低",
  medium: "中",
  high: "高",
}

const TYPE_LABELS: Record<string, string> = {
  individual: "個人",
  corporate_domestic: "国内法人",
  corporate_foreign: "海外法人",
}

const CUSTOMER_TYPES = [
  { value: "individual", label: "個人" },
  { value: "corporate_domestic", label: "国内法人" },
  { value: "corporate_foreign", label: "海外法人" },
]

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString("ja-JP")
}

export function CustomersPage() {
  const { data: customers, loading, error } = useApi(api.customers.list)
  const [showForm, setShowForm] = useState(false)
  const [creating, setCreating] = useState(false)
  const [filter, setFilter] = useState("")
  const [tierFilter, setTierFilter] = useState<string>("")
  const extIdRef = useRef<HTMLInputElement>(null)
  const countryRef = useRef<HTMLInputElement>(null)
  const [customerType, setCustomerType] = useState("individual")

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    const externalId = extIdRef.current?.value.trim()
    const country = countryRef.current?.value.trim()
    if (!externalId || !country) return
    setCreating(true)
    try {
      await api.customers.create({
        external_id: externalId,
        customer_type: customerType,
        country_code: country,
        product_types: [],
        attributes: {},
      })
      window.location.reload()
    } finally {
      setCreating(false)
    }
  }

  if (loading) {
    return <TableSkeleton />
  }

  if (error) {
    return <p className="p-12 text-center text-destructive">顧客データの取得に失敗しました</p>
  }

  const filtered = customers?.filter((c) => {
    const matchText = !filter || c.external_id.toLowerCase().includes(filter.toLowerCase()) ||
      (c.attributes?.name ?? "").includes(filter) || c.country_code.toLowerCase().includes(filter.toLowerCase())
    const matchTier = !tierFilter || (c.risk_tier ?? "") === tierFilter
    return matchText && matchTier
  })

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">顧客一覧</h1>
        <div className="flex items-center gap-2">
          <p className="text-sm text-muted-foreground">{filtered?.length ?? 0} 件</p>
          <Button size="sm" onClick={() => setShowForm(!showForm)}>
            <Plus className="h-4 w-4" />
            新規作成
          </Button>
        </div>
      </div>

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">顧客作成</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="flex flex-wrap items-end gap-4">
              <div>
                <label className="mb-1 block text-xs font-medium">外部ID</label>
                <input ref={extIdRef} required placeholder="EXT-001"
                  className="rounded-md border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">種別</label>
                <div className="flex gap-1">
                  {CUSTOMER_TYPES.map((t) => (
                    <button key={t.value} type="button" onClick={() => setCustomerType(t.value)}
                      className={`rounded-md border px-2 py-1 text-xs font-medium transition-colors ${customerType === t.value ? "border-primary bg-primary/10 text-primary" : "border-input text-muted-foreground hover:bg-accent"}`}>
                      {t.label}
                    </button>
                  ))}
                </div>
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">国コード</label>
                <input ref={countryRef} required placeholder="JP" maxLength={2}
                  className="w-16 rounded-md border bg-background px-3 py-2 text-sm uppercase focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <Button type="submit" size="sm" disabled={creating}>作成</Button>
            </form>
          </CardContent>
        </Card>
      )}

      <div className="flex gap-2">
        <input value={filter} onChange={(e) => setFilter(e.target.value)}
          placeholder="ID・名前・国コードで検索..."
          className="max-w-xs rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
        <select value={tierFilter} onChange={(e) => setTierFilter(e.target.value)}
          className="rounded-md border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring">
          <option value="">全リスクティア</option>
          <option value="low">低リスク</option>
          <option value="medium">中リスク</option>
          <option value="high">高リスク</option>
        </select>
      </div>

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>外部ID</TableHead>
              <TableHead>種別</TableHead>
              <TableHead>国</TableHead>
              <TableHead>リスクスコア</TableHead>
              <TableHead>リスクティア</TableHead>
              <TableHead>最終スコアリング</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered && filtered.length > 0 ? (
              filtered.map((c) => (
                <TableRow key={c.id} className="cursor-pointer">
                  <TableCell className="font-mono text-sm">
                    <Link to={`/customers/${c.id}`} className="text-primary underline-offset-4 hover:underline">
                      {c.external_id}
                    </Link>
                  </TableCell>
                  <TableCell>{TYPE_LABELS[c.customer_type] ?? c.customer_type}</TableCell>
                  <TableCell>{c.country_code}</TableCell>
                  <TableCell>{c.risk_score != null ? c.risk_score.toFixed(1) : "-"}</TableCell>
                  <TableCell>
                    {c.risk_tier ? (
                      <Badge variant={TIER_VARIANT[c.risk_tier]}>{TIER_LABELS[c.risk_tier] ?? c.risk_tier}</Badge>
                    ) : (
                      <Badge variant="secondary">未スコア</Badge>
                    )}
                  </TableCell>
                  <TableCell>{c.last_scored_at ? formatDate(c.last_scored_at) : "-"}</TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                  顧客データがありません
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function TableSkeleton() {
  return (
    <div className="space-y-6">
      <div className="h-8 w-40 animate-pulse rounded bg-muted" />
      <div className="h-64 animate-pulse rounded-xl border bg-muted" />
    </div>
  )
}
