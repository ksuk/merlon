import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { CountrySelect, IdentityFields } from "@/components/identity-fields"
import { useApi } from "@/hooks/use-api"
import { usePolicy } from "@/hooks/use-policy"
import { api, type Customer, type RiskTier } from "@/lib/api"
import { Plus } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

const TIER_VARIANT: Record<RiskTier, "low" | "medium" | "high"> = { low: "low", medium: "medium", high: "high" }
const CUSTOMER_TYPES = ["individual", "corporate_domestic", "corporate_foreign", "trust", "partnership", "npo", "government", "foreign_legal_arrangement"] as const
const CUSTOMER_TYPE_KEYS: Record<string, string> = { individual: "individual", corporate_domestic: "corporateDomestic", corporate_foreign: "corporateForeign", trust: "trust", partnership: "partnership", npo: "npo", government: "government", foreign_legal_arrangement: "foreignLegalArrangement" }

function formatDate(iso: string, locale: string) { return new Date(iso).toLocaleDateString(locale) }
function attribute(customer: Customer, key: string) { const value = customer.attributes?.[key]; return typeof value === "string" ? value : "" }
function formatCountry(code: string, locale: string) { try { return new Intl.DisplayNames([locale], { type: "region" }).of(code) ?? code } catch { return code } }

export function CustomersPage() {
  const { t, i18n } = useTranslation()
  const [showForm, setShowForm] = useState(false)
  const [creating, setCreating] = useState(false)
  const [filter, setFilter] = useState("")
  const [tierFilter, setTierFilter] = useState("")
  const [formError, setFormError] = useState<string | null>(null)
  const [externalId, setExternalId] = useState("")
  const [customerType, setCustomerType] = useState<string>("individual")
  const [country, setCountry] = useState("")
  const [identity, setIdentity] = useState<Record<string, string>>({})
  const [products, setProducts] = useState("")
  const { data: kyc } = usePolicy("kyc_required_fields")
  const { data: page, loading, error } = useApi(() => api.customers.listAll({ search: filter }), filter)
  const customers = page?.data
  const filtered = customers?.filter((customer) => !tierFilter || (customer.risk_tier ?? "") === tierFilter)

  async function handleCreate(event: React.FormEvent) {
    event.preventDefault()
    if (!externalId.trim() || !country.trim()) return
    setCreating(true)
    setFormError(null)
    try {
      const identityPayload = Object.fromEntries(Object.entries(identity).map(([key, value]) => [key, value.trim()]).filter(([, value]) => value !== ""))
      await api.customers.create({ external_id: externalId.trim(), customer_type: customerType, country_code: country.trim().toUpperCase(), product_types: products.split(",").map((item) => item.trim()).filter(Boolean), attributes: {}, identity: identityPayload })
      window.location.reload()
    } catch (err) {
      setFormError(err instanceof Error ? err.message : String(err))
    } finally { setCreating(false) }
  }

  if (loading) return <TableSkeleton />
  if (error) return <div role="alert" className="p-12 text-center text-destructive">{t("customers.error")}: {error}</div>

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between"><h1 className="text-2xl font-bold tracking-tight">{t("customers.title")}</h1><div className="flex items-center gap-2"><p className="text-sm text-muted-foreground">{t("customers.count", { count: filtered?.length ?? 0 })}</p><Button size="sm" onClick={() => setShowForm((value) => !value)}><Plus className="h-4 w-4" />{t("customers.createButton")}</Button></div></div>
      {formError && <p role="alert" className="rounded-md border border-destructive/50 bg-destructive/5 p-3 text-sm text-destructive">{formError}</p>}
      {showForm && <Card><CardHeader><CardTitle className="text-base">{t("customers.form.title")}</CardTitle></CardHeader><CardContent><form onSubmit={handleCreate} className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Field id="customer-external-id" label={t("customers.form.externalId")} value={externalId} onChange={setExternalId} required placeholder="EXT-001" />
        <div><label htmlFor="customer-type" className="mb-1 block text-xs font-medium">{t("customers.form.type")}</label><select id="customer-type" value={customerType} onChange={(event) => setCustomerType(event.target.value)} className="w-full rounded-md border bg-background px-3 py-2 text-sm">{CUSTOMER_TYPES.map((type) => <option key={type} value={type}>{t(`customers.type.${CUSTOMER_TYPE_KEYS[type]}`, { defaultValue: type })}</option>)}</select></div>
        <div><label htmlFor="customer-country" className="mb-1 block text-xs font-medium">{t("customers.form.countryCode")}</label><CountrySelect id="customer-country" label={t("customers.form.countryCode")} value={country} onChange={setCountry} className="w-full" /></div>
        <IdentityFields customerType={customerType} policy={kyc?.document} values={identity} onChange={(field, value) => setIdentity((current) => ({ ...current, [field]: value }))} idPrefix="customer-identity" />
        <Field id="customer-products" label={t("customers.form.products")} value={products} onChange={setProducts} placeholder="crypto, remittance" />
        <p className="text-xs text-muted-foreground sm:col-span-2 lg:col-span-4">{kyc ? t("customers.form.kycPolicyVersion", { version: kyc.policy_version }) : t("customers.form.kycPolicyLoading")}</p>
        <div className="flex items-end"><Button type="submit" size="sm" disabled={creating}>{t("customers.form.submit")}</Button></div>
      </form></CardContent></Card>}
      <div className="flex gap-2"><div><label htmlFor="customer-search" className="sr-only">{t("customers.search.placeholder")}</label><input id="customer-search" value={filter} onChange={(event) => setFilter(event.target.value)} placeholder={t("customers.search.placeholder")} className="max-w-xs rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" /></div><label htmlFor="customer-tier-filter" className="sr-only">{t("customers.filter.allTiers")}</label><select id="customer-tier-filter" value={tierFilter} onChange={(event) => setTierFilter(event.target.value)} className="rounded-md border bg-background px-3 py-2 text-sm"><option value="">{t("customers.filter.allTiers")}</option><option value="low">{t("customers.tier.low")}</option><option value="medium">{t("customers.tier.medium")}</option><option value="high">{t("customers.tier.high")}</option></select></div>
      <div className="rounded-xl border"><Table><TableHeader><TableRow><TableHead>{t("customers.table.header.externalId")}</TableHead><TableHead>{t("customers.table.header.name")}</TableHead><TableHead>{t("customers.table.header.type")}</TableHead><TableHead>{t("customers.table.header.country")}</TableHead><TableHead>{t("customers.table.header.status")}</TableHead><TableHead>{t("customers.table.header.riskScore")}</TableHead><TableHead>{t("customers.table.header.riskTier")}</TableHead><TableHead>{t("customers.table.header.lastScored")}</TableHead></TableRow></TableHeader><TableBody>{filtered && filtered.length > 0 ? filtered.map((customer) => <TableRow key={customer.id}><TableCell className="font-mono text-sm"><Link to={`/customers/${customer.id}`} className="text-primary underline-offset-4 hover:underline">{customer.external_id}</Link></TableCell><TableCell>{attribute(customer, "name_ja") || attribute(customer, "name") || "-"}</TableCell><TableCell>{t(`customers.type.${CUSTOMER_TYPE_KEYS[customer.customer_type] ?? customer.customer_type}`, { defaultValue: customer.customer_type })}</TableCell><TableCell><span title={customer.country_code}>{formatCountry(customer.country_code, i18n.language)} <span className="text-xs text-muted-foreground">{customer.country_code}</span></span></TableCell><TableCell><Badge variant="outline">{t(`customers.status.${customer.status ?? "active"}`, { defaultValue: customer.status ?? "active" })}</Badge></TableCell><TableCell>{customer.risk_score != null ? customer.risk_score.toFixed(1) : "-"}</TableCell><TableCell>{customer.risk_tier ? <Badge variant={TIER_VARIANT[customer.risk_tier]}>{t(`customers.tierShort.${customer.risk_tier}`, { defaultValue: customer.risk_tier })}</Badge> : <Badge variant="secondary">{t("customers.table.unscored")}</Badge>}</TableCell><TableCell>{customer.last_scored_at ? formatDate(customer.last_scored_at, i18n.language) : "-"}</TableCell></TableRow>) : <TableRow><TableCell colSpan={8} className="h-24 text-center text-muted-foreground">{t("customers.table.empty")}</TableCell></TableRow>}</TableBody></Table></div>
      {customers && <p className="text-center text-xs text-muted-foreground">{t("list.allLoaded")}</p>}
    </div>
  )
}

function Field({ id, label, value, onChange, required, placeholder, maxLength, className = "" }: { id: string; label: string; value: string; onChange: (value: string) => void; required?: boolean; placeholder?: string; maxLength?: number; className?: string }) {
  return <div className={className}><label htmlFor={id} className="mb-1 block text-xs font-medium">{label}</label><input id={id} required={required} value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} maxLength={maxLength} className="w-full rounded-md border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" /></div>
}

function TableSkeleton() { return <div className="space-y-6"><div className="h-8 w-40 animate-pulse rounded bg-muted" /><div className="h-64 animate-pulse rounded-xl border bg-muted" /></div> }
