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
import { api, type RiskTier, type ScreenResult } from "@/lib/api"
import { ArrowLeft, Pencil, RefreshCw, Search } from "lucide-react"
import { useCallback, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useParams } from "react-router"

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

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

export function CustomerDetailPage() {
  const { t, i18n } = useTranslation()
  const { id } = useParams<{ id: string }>()
  const { data: customer, loading, error } = useApi(
    useCallback(() => api.customers.get(id!), [id]),
  )
  const { data: scores, loading: scoresLoading } = useApi(
    useCallback(() => api.customers.scoreHistory(id!), [id]),
  )
  const [scoring, setScoring] = useState(false)
  const [screening, setScreening] = useState(false)
  const [screenResult, setScreenResult] = useState<ScreenResult | null>(null)
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const countryRef = useRef<HTMLInputElement>(null)

  async function handleScore() {
    if (!id) return
    setScoring(true)
    try {
      await api.customers.score(id, "default")
      window.location.reload()
    } catch {
      setScoring(false)
    }
  }

  async function handleScreen() {
    if (!id) return
    setScreening(true)
    setScreenResult(null)
    try {
      const result = await api.customers.screen(id, [])
      setScreenResult(result)
    } finally {
      setScreening(false)
    }
  }

  async function handleSave() {
    if (!id || !countryRef.current) return
    setSaving(true)
    try {
      await api.customers.update(id, { country_code: countryRef.current.value.trim() })
      window.location.reload()
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-64 animate-pulse rounded bg-muted" />
        <div className="h-48 animate-pulse rounded-xl border bg-muted" />
      </div>
    )
  }

  if (error || !customer) {
    return (
      <div className="space-y-4">
        <Link to="/customers" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> {t("customerDetail.backToList")}
        </Link>
        <p className="text-destructive">{t("customerDetail.error")}</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link to="/customers" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> {t("customerDetail.back")}
        </Link>
        <h1 className="text-2xl font-bold tracking-tight">{customer.external_id}</h1>
        {customer.risk_tier && (
          <Badge variant={TIER_VARIANT[customer.risk_tier]}>
            {t(`customers.tier.${customer.risk_tier}`)}
          </Badge>
        )}
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle className="text-base">{t("customerDetail.basicInfo.title")}</CardTitle>
            <Button size="sm" variant="ghost" onClick={() => setEditing(!editing)}>
              <Pencil className="h-4 w-4" />
            </Button>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.basicInfo.internalId")}</dt>
                <dd className="font-mono">{customer.id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.basicInfo.type")}</dt>
                <dd>
                  {t(`customers.type.${CUSTOMER_TYPE_KEYS[customer.customer_type] ?? customer.customer_type}`, {
                    defaultValue: customer.customer_type,
                  })}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.basicInfo.countryCode")}</dt>
                <dd>
                  {editing ? (
                    <div className="flex gap-2">
                      <input ref={countryRef} defaultValue={customer.country_code} maxLength={2}
                        className="w-16 rounded-md border bg-background px-2 py-1 text-sm uppercase focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" />
                      <Button size="sm" variant="outline" onClick={handleSave} disabled={saving}>
                        {t("customerDetail.basicInfo.save")}
                      </Button>
                    </div>
                  ) : customer.country_code}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.basicInfo.products")}</dt>
                <dd>{customer.product_types?.join(", ") || "-"}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.basicInfo.createdAt")}</dt>
                <dd>{formatDateTime(customer.created_at, i18n.language)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.basicInfo.updatedAt")}</dt>
                <dd>{formatDateTime(customer.updated_at, i18n.language)}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle className="text-base">{t("customerDetail.riskAssessment.title")}</CardTitle>
            <div className="flex gap-2">
              <Button size="sm" variant="outline" onClick={handleScreen} disabled={screening}>
                <Search className={`h-4 w-4 ${screening ? "animate-pulse" : ""}`} />
                {t("customerDetail.riskAssessment.screenButton")}
              </Button>
              <Button size="sm" variant="outline" onClick={handleScore} disabled={scoring}>
                <RefreshCw className={`h-4 w-4 ${scoring ? "animate-spin" : ""}`} />
                {t("customerDetail.riskAssessment.scoreButton")}
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.riskAssessment.riskScore")}</dt>
                <dd className="text-2xl font-bold">
                  {customer.risk_score != null ? customer.risk_score.toFixed(1) : "-"}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.riskAssessment.riskTier")}</dt>
                <dd>
                  {customer.risk_tier ? (
                    <Badge variant={TIER_VARIANT[customer.risk_tier]}>
                      {t(`customers.tier.${customer.risk_tier}`)}
                    </Badge>
                  ) : t("customerDetail.riskAssessment.unscored")}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("customerDetail.riskAssessment.lastScored")}</dt>
                <dd>{customer.last_scored_at ? formatDateTime(customer.last_scored_at, i18n.language) : "-"}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>
      </div>

      {screenResult && (
        <Card className={screenResult.hit ? "border-red-200" : "border-green-200"}>
          <CardHeader>
            <CardTitle className="text-base">
              {t("customerDetail.screening.title")}
              <Badge variant={screenResult.hit ? "destructive" : "low"} className="ml-2">
                {screenResult.hit ? t("customerDetail.screening.hit") : t("customerDetail.screening.noHit")}
              </Badge>
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">
              {t("customerDetail.screening.summary", {
                count: screenResult.lists_checked,
                time: formatDateTime(screenResult.screened_at, i18n.language),
              })}
            </p>
            {screenResult.matches.length > 0 && (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("customerDetail.screening.table.list")}</TableHead>
                    <TableHead>{t("customerDetail.screening.table.matchedName")}</TableHead>
                    <TableHead>{t("customerDetail.screening.table.similarity")}</TableHead>
                    <TableHead>{t("customerDetail.screening.table.type")}</TableHead>
                    <TableHead>{t("customerDetail.screening.table.source")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {screenResult.matches.map((m, i) => (
                    <TableRow key={i}>
                      <TableCell className="font-mono text-xs">{m.list_id}</TableCell>
                      <TableCell>{m.matched_name}</TableCell>
                      <TableCell>{(m.similarity * 100).toFixed(1)}%</TableCell>
                      <TableCell>{m.list_type}</TableCell>
                      <TableCell>{m.source}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      )}

      {customer.attributes && Object.keys(customer.attributes).length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("customerDetail.attributes.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid gap-2 text-sm md:grid-cols-2">
              {Object.entries(customer.attributes).map(([key, value]) => (
                <div key={key} className="flex justify-between rounded-md bg-muted/50 px-3 py-2">
                  <dt className="text-muted-foreground">{key}</dt>
                  <dd>{value}</dd>
                </div>
              ))}
            </dl>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("customerDetail.scoreHistory.title")}</CardTitle>
        </CardHeader>
        <CardContent>
          {scoresLoading ? (
            <div className="h-32 animate-pulse rounded bg-muted" />
          ) : scores && scores.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("customerDetail.scoreHistory.table.score")}</TableHead>
                  <TableHead>{t("customerDetail.scoreHistory.table.tier")}</TableHead>
                  <TableHead>{t("customerDetail.scoreHistory.table.ruleSet")}</TableHead>
                  <TableHead>{t("customerDetail.scoreHistory.table.version")}</TableHead>
                  <TableHead>{t("customerDetail.scoreHistory.table.evaluatedAt")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {scores.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell className="font-bold">{s.score.toFixed(1)}</TableCell>
                    <TableCell>
                      <Badge variant={TIER_VARIANT[s.tier]}>{t(`customers.tier.${s.tier}`)}</Badge>
                    </TableCell>
                    <TableCell className="font-mono text-sm">{s.rule_set_id}</TableCell>
                    <TableCell>v{s.rule_set_version}</TableCell>
                    <TableCell>{formatDateTime(s.scored_at, i18n.language)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="py-8 text-center text-sm text-muted-foreground">{t("customerDetail.scoreHistory.empty")}</p>
          )}
        </CardContent>
      </Card>

      {scores && scores.length > 0 && (scores[0].factors?.length ?? 0) > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("customerDetail.scoreFactors.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("customerDetail.scoreFactors.table.axis")}</TableHead>
                  <TableHead>{t("customerDetail.scoreFactors.table.name")}</TableHead>
                  <TableHead>{t("customerDetail.scoreFactors.table.score")}</TableHead>
                  <TableHead>{t("customerDetail.scoreFactors.table.description")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(scores[0].factors ?? []).map((factor) => (
                  <TableRow key={`${factor.axis}-${factor.name}`}>
                    <TableCell>{factor.axis}</TableCell>
                    <TableCell>{factor.name}</TableCell>
                    <TableCell>{factor.score.toFixed(1)}</TableCell>
                    <TableCell>{factor.description}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
