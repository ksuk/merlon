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
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

const TIER_VARIANT: Record<RiskTier, "low" | "medium" | "high"> = {
  low: "low",
  medium: "medium",
  high: "high",
}

const CUSTOMER_TYPE_KEYS: Record<string, string> = {
  individual: "individual",
  corporate_domestic: "corporateDomestic",
  corporate_foreign: "corporateForeign",
}

function formatDate(iso: string, locale: string) {
  return new Date(iso).toLocaleDateString(locale)
}

export function CustomersPage() {
  const { t, i18n } = useTranslation()
  const CUSTOMER_TYPES = [
    { value: "individual", label: t("customers.type.individual") },
    { value: "corporate_domestic", label: t("customers.type.corporateDomestic") },
    { value: "corporate_foreign", label: t("customers.type.corporateForeign") },
  ]
  const [showForm, setShowForm] = useState(false)
  const [creating, setCreating] = useState(false)
  const [filter, setFilter] = useState("")
  const [tierFilter, setTierFilter] = useState<string>("")
  const extIdRef = useRef<HTMLInputElement>(null)
  const countryRef = useRef<HTMLInputElement>(null)
  const [customerType, setCustomerType] = useState("individual")
  const { data: page, loading, error } = useApi(
    () => api.customers.listAll({ search: filter }),
    filter,
  )
  const customers = page?.data

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
    return <p className="p-12 text-center text-destructive">{t("customers.error")}</p>
  }

  const filtered = customers?.filter((c) => !tierFilter || (c.risk_tier ?? "") === tierFilter)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">{t("customers.title")}</h1>
        <div className="flex items-center gap-2">
          <p className="text-sm text-muted-foreground">{t("customers.count", { count: filtered?.length ?? 0 })}</p>
          <Button size="sm" onClick={() => setShowForm(!showForm)}>
            <Plus className="h-4 w-4" />
            {t("customers.createButton")}
          </Button>
        </div>
      </div>

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("customers.form.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="flex flex-wrap items-end gap-4">
              <div>
                <label className="mb-1 block text-xs font-medium">{t("customers.form.externalId")}</label>
                <input ref={extIdRef} required placeholder="EXT-001"
                  className="rounded-md border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">{t("customers.form.type")}</label>
                <div className="flex gap-1">
                  {CUSTOMER_TYPES.map((type) => (
                    <button key={type.value} type="button" onClick={() => setCustomerType(type.value)}
                      className={`rounded-md border px-2 py-1 text-xs font-medium transition-colors ${customerType === type.value ? "border-primary bg-primary/10 text-primary" : "border-input text-muted-foreground hover:bg-accent"}`}>
                      {type.label}
                    </button>
                  ))}
                </div>
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium">{t("customers.form.countryCode")}</label>
                <input ref={countryRef} required placeholder="JP" maxLength={2}
                  className="w-16 rounded-md border bg-background px-3 py-2 text-sm uppercase focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </div>
              <Button type="submit" size="sm" disabled={creating}>{t("customers.form.submit")}</Button>
            </form>
          </CardContent>
        </Card>
      )}

      <div className="flex gap-2">
        <input value={filter} onChange={(e) => setFilter(e.target.value)}
          placeholder={t("customers.search.placeholder")}
          className="max-w-xs rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
        <select value={tierFilter} onChange={(e) => setTierFilter(e.target.value)}
          className="rounded-md border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring">
          <option value="">{t("customers.filter.allTiers")}</option>
          <option value="low">{t("customers.tier.low")}</option>
          <option value="medium">{t("customers.tier.medium")}</option>
          <option value="high">{t("customers.tier.high")}</option>
        </select>
      </div>

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("customers.table.header.externalId")}</TableHead>
              <TableHead>{t("customers.table.header.type")}</TableHead>
              <TableHead>{t("customers.table.header.country")}</TableHead>
              <TableHead>{t("customers.table.header.riskScore")}</TableHead>
              <TableHead>{t("customers.table.header.riskTier")}</TableHead>
              <TableHead>{t("customers.table.header.lastScored")}</TableHead>
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
                  <TableCell>
                    {t(`customers.type.${CUSTOMER_TYPE_KEYS[c.customer_type] ?? c.customer_type}`, {
                      defaultValue: c.customer_type,
                    })}
                  </TableCell>
                  <TableCell>{c.country_code}</TableCell>
                  <TableCell>{c.risk_score != null ? c.risk_score.toFixed(1) : "-"}</TableCell>
                  <TableCell>
                    {c.risk_tier ? (
                      <Badge variant={TIER_VARIANT[c.risk_tier]}>
                        {t(`customers.tierShort.${c.risk_tier}`, { defaultValue: c.risk_tier })}
                      </Badge>
                    ) : (
                      <Badge variant="secondary">{t("customers.table.unscored")}</Badge>
                    )}
                  </TableCell>
                  <TableCell>{c.last_scored_at ? formatDate(c.last_scored_at, i18n.language) : "-"}</TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                  {t("customers.table.empty")}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      {customers && <p className="text-center text-xs text-muted-foreground">{t("list.allLoaded")}</p>}
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
