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
import { useTranslation } from "react-i18next"
import { Link } from "react-router-dom"

const DIRECTION_VARIANT: Record<string, "low" | "high" | "secondary"> = {
  inbound: "low",
  outbound: "high",
  internal: "secondary",
}

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

function formatAmount(amount: number, currency: string, locale: string) {
  return new Intl.NumberFormat(locale, { style: "currency", currency }).format(amount)
}

export function TransactionsPage() {
  const { t, i18n } = useTranslation()
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
    return <p className="p-12 text-center text-destructive">{t("transactions.error")}</p>
  }

  const DIRECTIONS = ["inbound", "outbound", "internal"] as const

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">{t("transactions.title")}</h1>
        <div className="flex items-center gap-2">
          <p className="text-sm text-muted-foreground">
            {t("transactions.count", { count: transactions?.length ?? 0 })}
          </p>
          <Button size="sm" onClick={() => setShowForm(!showForm)}>
            <Plus className="h-4 w-4" />
            {t("transactions.createButton")}
          </Button>
        </div>
      </div>

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("transactions.form.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="flex flex-wrap items-end gap-3">
              <div>
                <label className="mb-1 block text-xs font-medium">{t("transactions.form.customerId")}</label>
                <input ref={custRef} required placeholder="cust-001"
                  className="w-32 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">{t("transactions.form.externalId")}</label>
                <input ref={extRef} required placeholder="TXN-001"
                  className="w-32 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">{t("transactions.form.amount")}</label>
                <input ref={amountRef} type="number" required placeholder="100000" min="1"
                  className="w-28 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">{t("transactions.form.currency")}</label>
                <input ref={currencyRef} defaultValue="JPY" maxLength={3}
                  className="w-16 rounded-md border bg-background px-2 py-2 text-sm uppercase focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">{t("transactions.form.direction")}</label>
                <div className="flex gap-1">
                  {DIRECTIONS.map((d) => (
                    <button key={d} type="button" onClick={() => setDirection(d)}
                      className={`rounded-md border px-2 py-1 text-xs font-medium transition-colors ${direction === d ? "border-primary bg-primary/10 text-primary" : "border-input text-muted-foreground hover:bg-accent"}`}>
                      {t(`transactions.direction.${d}`)}
                    </button>
                  ))}
                </div>
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">{t("transactions.form.counterpartyCountry")}</label>
                <input ref={countryRef} placeholder="JP" maxLength={2}
                  className="w-16 rounded-md border bg-background px-2 py-2 text-sm uppercase focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">{t("transactions.form.channel")}</label>
                <input ref={channelRef} placeholder="online"
                  className="w-24 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <Button type="submit" size="sm" disabled={creating}>{t("transactions.form.submit")}</Button>
            </form>
          </CardContent>
        </Card>
      )}

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("transactions.table.header.direction")}</TableHead>
              <TableHead>{t("transactions.table.header.customerId")}</TableHead>
              <TableHead>{t("transactions.table.header.amount")}</TableHead>
              <TableHead>{t("transactions.table.header.counterpartyCountry")}</TableHead>
              <TableHead>{t("transactions.table.header.channel")}</TableHead>
              <TableHead>{t("transactions.table.header.executedAt")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {transactions && transactions.length > 0 ? (
              transactions.map((tx) => (
                <TableRow key={tx.id}>
                  <TableCell>
                    <Link to={`/transactions/${tx.id}`} className="hover:underline">
                      <Badge variant={DIRECTION_VARIANT[tx.direction] ?? "secondary"}>
                        {t(`transactions.direction.${tx.direction}`, { defaultValue: tx.direction })}
                      </Badge>
                    </Link>
                  </TableCell>
                  <TableCell className="font-mono text-sm">
                    <Link to={`/customers/${tx.customer_id}`} className="text-primary hover:underline">
                      {tx.customer_id}
                    </Link>
                  </TableCell>
                  <TableCell className="font-mono">{formatAmount(tx.amount, tx.currency, i18n.language)}</TableCell>
                  <TableCell>{tx.counterparty_country || "-"}</TableCell>
                  <TableCell>{tx.channel || "-"}</TableCell>
                  <TableCell className="whitespace-nowrap">{formatDateTime(tx.executed_at, i18n.language)}</TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                  {t("transactions.table.empty")}
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
