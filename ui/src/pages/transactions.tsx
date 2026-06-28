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
import { api } from "@/lib/api"
import { Plus } from "lucide-react"
import { useRef, useState } from "react"
import { Link } from "react-router-dom"

const DIRECTION_LABELS: Record<string, string> = {
  inbound: "入金",
  outbound: "出金",
  internal: "内部",
}

const DIRECTION_VARIANT: Record<string, "low" | "high" | "secondary"> = {
  inbound: "low",
  outbound: "high",
  internal: "secondary",
}

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

function formatAmount(amount: number, currency: string) {
  return new Intl.NumberFormat("ja-JP", { style: "currency", currency }).format(amount)
}

export function TransactionsPage() {
  const { data: transactions, loading, error } = useApi(api.transactions.list)
  const [showForm, setShowForm] = useState(false)
  const [creating, setCreating] = useState(false)
  const [direction, setDirection] = useState("inbound")
  const custRef = useRef<HTMLInputElement>(null)
  const extRef = useRef<HTMLInputElement>(null)
  const amountRef = useRef<HTMLInputElement>(null)
  const currencyRef = useRef<HTMLInputElement>(null)
  const countryRef = useRef<HTMLInputElement>(null)
  const channelRef = useRef<HTMLInputElement>(null)

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    const customerId = custRef.current?.value.trim()
    const externalId = extRef.current?.value.trim()
    const amount = parseFloat(amountRef.current?.value ?? "0")
    const currency = currencyRef.current?.value.trim() || "JPY"
    if (!customerId || !externalId || !amount) return
    setCreating(true)
    try {
      await api.transactions.create({
        customer_id: customerId,
        external_id: externalId,
        amount,
        currency,
        direction,
        counterparty_country: countryRef.current?.value.trim() || undefined,
        channel: channelRef.current?.value.trim() || undefined,
        executed_at: new Date().toISOString(),
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
    return <p className="p-12 text-center text-destructive">取引データの取得に失敗しました</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">取引一覧</h1>
        <div className="flex items-center gap-2">
          <p className="text-sm text-muted-foreground">{transactions?.length ?? 0} 件</p>
          <Button size="sm" onClick={() => setShowForm(!showForm)}>
            <Plus className="h-4 w-4" />
            新規登録
          </Button>
        </div>
      </div>

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">取引登録</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="flex flex-wrap items-end gap-3">
              <div>
                <label className="mb-1 block text-xs font-medium">顧客ID</label>
                <input ref={custRef} required placeholder="cust-001"
                  className="w-32 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">外部ID</label>
                <input ref={extRef} required placeholder="TXN-001"
                  className="w-32 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">金額</label>
                <input ref={amountRef} type="number" required placeholder="100000" min="1"
                  className="w-28 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">通貨</label>
                <input ref={currencyRef} defaultValue="JPY" maxLength={3}
                  className="w-16 rounded-md border bg-background px-2 py-2 text-sm uppercase focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">方向</label>
                <div className="flex gap-1">
                  {(["inbound", "outbound", "internal"] as const).map((d) => (
                    <button key={d} type="button" onClick={() => setDirection(d)}
                      className={`rounded-md border px-2 py-1 text-xs font-medium transition-colors ${direction === d ? "border-primary bg-primary/10 text-primary" : "border-input text-muted-foreground hover:bg-accent"}`}>
                      {DIRECTION_LABELS[d]}
                    </button>
                  ))}
                </div>
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">相手先国</label>
                <input ref={countryRef} placeholder="JP" maxLength={2}
                  className="w-16 rounded-md border bg-background px-2 py-2 text-sm uppercase focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">チャネル</label>
                <input ref={channelRef} placeholder="online"
                  className="w-24 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <Button type="submit" size="sm" disabled={creating}>登録</Button>
            </form>
          </CardContent>
        </Card>
      )}

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>方向</TableHead>
              <TableHead>顧客ID</TableHead>
              <TableHead>金額</TableHead>
              <TableHead>相手先国</TableHead>
              <TableHead>チャネル</TableHead>
              <TableHead>実行日時</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {transactions && transactions.length > 0 ? (
              transactions.map((t) => (
                <TableRow key={t.id}>
                  <TableCell>
                    <Link to={`/transactions/${t.id}`} className="hover:underline">
                      <Badge variant={DIRECTION_VARIANT[t.direction] ?? "secondary"}>
                        {DIRECTION_LABELS[t.direction] ?? t.direction}
                      </Badge>
                    </Link>
                  </TableCell>
                  <TableCell className="font-mono text-sm">
                    <Link to={`/customers/${t.customer_id}`} className="text-primary hover:underline">
                      {t.customer_id}
                    </Link>
                  </TableCell>
                  <TableCell className="font-mono">{formatAmount(t.amount, t.currency)}</TableCell>
                  <TableCell>{t.counterparty_country || "-"}</TableCell>
                  <TableCell>{t.channel || "-"}</TableCell>
                  <TableCell className="whitespace-nowrap">{formatDateTime(t.executed_at)}</TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                  取引データがありません
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
