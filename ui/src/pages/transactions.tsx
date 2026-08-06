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
import { useCallback, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

const EMPTY_TRANSACTION_PAGE = {
  data: [],
  pagination: { has_more: false },
}

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
  const [selectedCustomerId, setSelectedCustomerId] = useState("")
  const { data: customerPage, loading: customersLoading, error: customersError } = useApi(api.customers.listAll)
  const fetchTransactions = useCallback(
    () => selectedCustomerId
      ? api.transactions.listAll(selectedCustomerId)
      : Promise.resolve(EMPTY_TRANSACTION_PAGE),
    [selectedCustomerId],
  )
  const { data: page, loading, error } = useApi(fetchTransactions, selectedCustomerId)
  const customers = customerPage?.data ?? []
  const transactions = selectedCustomerId ? (page?.data ?? []) : []
  const [showForm, setShowForm] = useState(false)
  const [creating, setCreating] = useState(false)
  const [direction, setDirection] = useState("inbound")
  const custRef = useRef<HTMLInputElement>(null)
  const extRef = useRef<HTMLInputElement>(null)
  const amountRef = useRef<HTMLInputElement>(null)
  const currencyRef = useRef<HTMLInputElement>(null)
  const countryRef = useRef<HTMLInputElement>(null)
  const channelRef = useRef<HTMLInputElement>(null)
  const accountRef = useRef<HTMLInputElement>(null)
  const counterpartyNameRef = useRef<HTMLInputElement>(null)
  const counterpartyTypeRef = useRef<HTMLSelectElement>(null)
  const travelStatusRef = useRef<HTMLSelectElement>(null)
  const travelReasonRef = useRef<HTMLInputElement>(null)
  const travelEvidenceRef = useRef<HTMLInputElement>(null)
  const metadataRef = useRef<HTMLInputElement>(null)
  const idempotencyRef = useRef<HTMLInputElement>(null)
  const [createError, setCreateError] = useState<string | null>(null)

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    const customerId = custRef.current?.value.trim()
    const externalId = extRef.current?.value.trim()
    const amount = parseFloat(amountRef.current?.value ?? "0")
    const currency = currencyRef.current?.value.trim() || "JPY"
    if (!customerId || !externalId || !amount) return
    setCreating(true)
    setCreateError(null)
    try {
      const travelStatus = travelStatusRef.current?.value || ""
      const counterpartyName = counterpartyNameRef.current?.value.trim() || ""
      let metadata: Record<string, unknown> | undefined
      let travelEvidence: Record<string, unknown> | undefined
      try {
        metadata = metadataRef.current?.value.trim() ? JSON.parse(metadataRef.current.value) as Record<string, unknown> : undefined
        travelEvidence = travelEvidenceRef.current?.value.trim() ? JSON.parse(travelEvidenceRef.current.value) as Record<string, unknown> : undefined
      } catch {
        setCreateError(t("transactions.form.invalidJson"))
        return
      }
      await api.transactions.create({
        customer_id: customerId,
        external_id: externalId,
        amount,
        currency,
        direction,
        counterparty_country: countryRef.current?.value.trim() || undefined,
        channel: channelRef.current?.value.trim() || undefined,
        account_id: accountRef.current?.value.trim() || undefined,
        counterparty: counterpartyName || travelStatus ? {
          counterparty_type: counterpartyTypeRef.current?.value || "unknown",
          originator: { name: counterpartyName, account_number: "" },
          beneficiary: { name: counterpartyName, account_number: "" },
          travel_rule_status: travelStatus || "incomplete",
        } : undefined,
        metadata,
        travel_rule_applicable: travelStatus === "" ? undefined : travelStatus !== "not_applicable",
        travel_rule_evidence: travelEvidence,
        travel_rule_not_applicable_reason: travelReasonRef.current?.value.trim() || undefined,
        executed_at: new Date().toISOString(),
      }, idempotencyRef.current?.value.trim() || undefined)
      window.location.reload()
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err))
    } finally {
      setCreating(false)
    }
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

      <div className="rounded-xl border bg-card p-4">
        <label htmlFor="transaction-customer-selector" className="mb-2 block text-sm font-medium">
          {t("transactions.customerSelector")}
        </label>
        <select
          id="transaction-customer-selector"
          aria-label={t("transactions.customerSelector")}
          value={selectedCustomerId}
          onChange={(e) => setSelectedCustomerId(e.target.value)}
          disabled={customersLoading || Boolean(customersError)}
          className="w-full max-w-md rounded-md border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        >
          <option value="">
            {customersLoading
              ? t("transactions.customerSelectorLoading")
              : t("transactions.customerSelectorPlaceholder")}
          </option>
          {customers.map((customer) => (
            <option key={customer.id} value={customer.id}>
              {customer.external_id} ({customer.id})
            </option>
          ))}
        </select>
        {customersError && (
          <p role="alert" className="mt-2 text-sm text-destructive">{t("transactions.customerSelectorError")}</p>
        )}
        {!selectedCustomerId && !customersError && (
          <p className="mt-2 text-sm text-muted-foreground">{t("transactions.customerSelectorPrompt")}</p>
        )}
      </div>

      {error && (
        <div role="alert" className="rounded-md border border-destructive/50 bg-destructive/5 p-3 text-sm text-destructive">
          {t("transactions.error")}
        </div>
      )}

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("transactions.form.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            {createError && <p role="alert" className="mb-3 text-sm text-destructive">{createError}</p>}
            <form onSubmit={handleCreate} className="flex flex-wrap items-end gap-3">
              <div>
                <label htmlFor="transaction-customer" className="mb-1 block text-xs font-medium">{t("transactions.form.customerId")}</label>
                <input id="transaction-customer" key={selectedCustomerId} ref={custRef} required defaultValue={selectedCustomerId} placeholder="cust-001"
                  className="w-32 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label htmlFor="transaction-external" className="mb-1 block text-xs font-medium">{t("transactions.form.externalId")}</label>
                <input id="transaction-external" ref={extRef} required placeholder="TXN-001"
                  className="w-32 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label htmlFor="transaction-amount" className="mb-1 block text-xs font-medium">{t("transactions.form.amount")}</label>
                <input id="transaction-amount" ref={amountRef} type="number" required placeholder="100000" min="1"
                  className="w-28 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label htmlFor="transaction-currency" className="mb-1 block text-xs font-medium">{t("transactions.form.currency")}</label>
                <input id="transaction-currency" ref={currencyRef} defaultValue="JPY" maxLength={3}
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
                <label htmlFor="transaction-counterparty-country" className="mb-1 block text-xs font-medium">{t("transactions.form.counterpartyCountry")}</label>
                <input id="transaction-counterparty-country" ref={countryRef} placeholder="JP" maxLength={2}
                  className="w-16 rounded-md border bg-background px-2 py-2 text-sm uppercase focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label htmlFor="transaction-channel" className="mb-1 block text-xs font-medium">{t("transactions.form.channel")}</label>
                <input id="transaction-channel" ref={channelRef} placeholder="online"
                  className="w-24 rounded-md border bg-background px-2 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label htmlFor="transaction-account" className="mb-1 block text-xs font-medium">{t("transactions.form.accountId")}</label>
                <input id="transaction-account" ref={accountRef} placeholder="account-001" className="w-28 rounded-md border bg-background px-2 py-2 text-sm" />
              </div>
              <div>
                <label htmlFor="transaction-counterparty-name" className="mb-1 block text-xs font-medium">{t("transactions.form.counterpartyName")}</label>
                <input id="transaction-counterparty-name" ref={counterpartyNameRef} placeholder="Counterparty" className="w-32 rounded-md border bg-background px-2 py-2 text-sm" />
              </div>
              <div>
                <label htmlFor="transaction-counterparty-type" className="mb-1 block text-xs font-medium">{t("transactions.form.counterpartyType")}</label>
                <select id="transaction-counterparty-type" ref={counterpartyTypeRef} defaultValue="unknown" className="w-32 rounded-md border bg-background px-2 py-2 text-sm"><option value="vasp">VASP</option><option value="unhosted_wallet">{t("transactions.form.unhostedWallet")}</option><option value="unknown">{t("transactions.form.unknown")}</option></select>
              </div>
              <div>
                <label htmlFor="transaction-travel-status" className="mb-1 block text-xs font-medium">{t("transactions.form.travelStatus")}</label>
                <select id="transaction-travel-status" ref={travelStatusRef} defaultValue="" className="w-36 rounded-md border bg-background px-2 py-2 text-sm"><option value="">{t("transactions.form.notSpecified")}</option><option value="complete">{t("transactions.form.complete")}</option><option value="incomplete">{t("transactions.form.incomplete")}</option><option value="not_applicable">{t("transactions.form.notApplicable")}</option></select>
              </div>
              <div>
                <label htmlFor="transaction-travel-reason" className="mb-1 block text-xs font-medium">{t("transactions.form.travelReason")}</label>
                <input id="transaction-travel-reason" ref={travelReasonRef} placeholder={t("transactions.form.travelReasonPlaceholder")} className="w-44 rounded-md border bg-background px-2 py-2 text-sm" />
              </div>
              <div>
                <label htmlFor="transaction-idempotency" className="mb-1 block text-xs font-medium">{t("transactions.form.idempotencyKey")}</label>
                <input id="transaction-idempotency" ref={idempotencyRef} placeholder="retry-key" className="w-32 rounded-md border bg-background px-2 py-2 text-sm" />
              </div>
              <div>
                <label htmlFor="transaction-metadata" className="mb-1 block text-xs font-medium">{t("transactions.form.metadata")}</label>
                <input id="transaction-metadata" ref={metadataRef} placeholder='{"source":"vendor"}' className="w-44 rounded-md border bg-background px-2 py-2 text-sm" />
              </div>
              <div>
                <label htmlFor="transaction-evidence" className="mb-1 block text-xs font-medium">{t("transactions.form.travelEvidence")}</label>
                <input id="transaction-evidence" ref={travelEvidenceRef} placeholder='{"provider":"..."}' className="w-44 rounded-md border bg-background px-2 py-2 text-sm" />
              </div>
              <Button type="submit" size="sm" disabled={creating}>{t("transactions.form.submit")}</Button>
            </form>
          </CardContent>
        </Card>
      )}

      {loading ? <TableSkeleton /> : (
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
              {transactions.length > 0 ? (
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
      )}
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
