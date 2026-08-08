import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api } from "@/lib/api"
import { TRAVEL_RULE_STATE_VARIANT, travelRuleStateOf } from "@/lib/travel-rule"
import { ArrowLeft } from "lucide-react"
import { useCallback } from "react"
import { useTranslation } from "react-i18next"
import { Link, useParams } from "react-router"

const DIR_VARIANT: Record<string, "low" | "medium" | "high"> = {
  inbound: "low",
  outbound: "high",
  internal: "medium",
}

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

function formatAmount(amount: number, currency: string, locale: string) {
  return new Intl.NumberFormat(locale, { style: "currency", currency }).format(amount)
}

// A labelled, recursive renderer for the free-shape counterparty, evidence
// and metadata payloads. Rendering them as JSON.stringify put raw braces in
// front of an operator; rendering only known keys would silently drop
// whatever an integration added.
function StructuredValue({ value }: { value: unknown }) {
  const { t } = useTranslation()
  if (value === null || value === undefined || value === "") {
    return <span className="text-muted-foreground">-</span>
  }
  if (typeof value === "boolean") {
    return <span>{value ? t("transactionDetail.travelRule.yes") : t("transactionDetail.travelRule.no")}</span>
  }
  if (Array.isArray(value)) {
    if (value.length === 0) return <span className="text-muted-foreground">-</span>
    return (
      <ul className="list-inside list-disc">
        {value.map((item, index) => <li key={index}><StructuredValue value={item} /></li>)}
      </ul>
    )
  }
  if (typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>)
    if (entries.length === 0) return <span className="text-muted-foreground">-</span>
    return (
      <dl className="space-y-1">
        {entries.map(([key, child]) => (
          <div key={key} className="flex justify-between gap-4">
            <dt className="text-muted-foreground">{t(`transactionDetail.travelRule.field.${key}`, { defaultValue: key })}</dt>
            <dd className="text-right"><StructuredValue value={child} /></dd>
          </div>
        ))}
      </dl>
    )
  }
  return <span>{String(value)}</span>
}

export function TransactionDetailPage() {
  const { t, i18n } = useTranslation()
  const { id } = useParams<{ id: string }>()
  const { data: txn, loading, error } = useApi(
    useCallback(() => api.transactions.get(id!), [id]),
  )
  const investigationCustomerID = txn?.customer_id
  const { data: investigation } = useApi(
    useCallback(() => investigationCustomerID ? api.customers.investigation(investigationCustomerID) : Promise.resolve(null), [investigationCustomerID]),
    investigationCustomerID,
  )

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-64 animate-pulse rounded bg-muted" />
        <div className="h-48 animate-pulse rounded-xl border bg-muted" />
      </div>
    )
  }

  if (error || !txn) {
    return (
      <div className="space-y-4">
        <Link to="/transactions" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> {t("transactionDetail.backToList")}
        </Link>
        <p role="alert" className="text-destructive">{t("transactionDetail.error")}</p>
      </div>
    )
  }

  const assessment = txn.travel_rule_assessment
  const travelRuleState = travelRuleStateOf(txn)
  const relatedAlerts = investigation?.alerts?.filter((alert) => alert.transaction_ids?.includes(txn.id)) ?? []
  const relatedAlertIDs = new Set(relatedAlerts.map((alert) => alert.id))
  const relatedCases = investigation?.cases?.filter((item) => item.alert_ids?.some((alertID) => relatedAlertIDs.has(alertID))) ?? []

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link to="/transactions" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> {t("transactionDetail.back")}
        </Link>
        <h1 className="text-2xl font-bold tracking-tight">{txn.external_id}</h1>
        <Badge variant={DIR_VARIANT[txn.direction] ?? "secondary"}>
          {t(`transactions.direction.${txn.direction}`, { defaultValue: txn.direction })}
        </Badge>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("transactionDetail.info.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("transactionDetail.info.internalId")}</dt>
                <dd className="font-mono">{txn.id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("transactionDetail.info.externalId")}</dt>
                <dd className="font-mono">{txn.external_id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("transactionDetail.info.customerId")}</dt>
                <dd>
                  <Link to={`/customers/${txn.customer_id}`} className="text-primary hover:underline font-mono">
                    {investigation?.customer?.external_id ?? txn.customer_id}
                  </Link>
                </dd>
              </div>
              {txn.account_id && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">{t("transactionDetail.info.accountId")}</dt>
                  <dd><Link to={`/accounts/${txn.account_id}`} className="text-primary hover:underline font-mono">{txn.account_id}</Link></dd>
                </div>
              )}
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("transactionDetail.info.direction")}</dt>
                <dd>
                  <Badge variant={DIR_VARIANT[txn.direction]}>
                    {t(`transactions.direction.${txn.direction}`)}
                  </Badge>
                </dd>
              </div>
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("transactionDetail.amountRoute.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("transactionDetail.amountRoute.amount")}</dt>
                <dd className="text-xl font-bold">{formatAmount(txn.amount, txn.currency, i18n.language)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("transactionDetail.amountRoute.currency")}</dt>
                <dd>{txn.currency}</dd>
              </div>
              {txn.counterparty_country && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">{t("transactionDetail.amountRoute.counterpartyCountry")}</dt>
                  <dd>{txn.counterparty_country}</dd>
                </div>
              )}
              {txn.channel && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">{t("transactionDetail.amountRoute.channel")}</dt>
                  <dd>{txn.channel}</dd>
                </div>
              )}
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("transactionDetail.amountRoute.executedAt")}</dt>
                <dd>{formatDateTime(txn.executed_at, i18n.language)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("transactionDetail.amountRoute.createdAt")}</dt>
                <dd>{formatDateTime(txn.created_at, i18n.language)}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("transactionDetail.travelRule.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between gap-4">
                <dt className="text-muted-foreground">{t("transactionDetail.travelRule.verdict")}</dt>
                <dd className="text-right">
                  <Badge variant={TRAVEL_RULE_STATE_VARIANT[travelRuleState]}>{t(`transactionDetail.travelRule.state.${travelRuleState}`)}</Badge>
                </dd>
              </div>
              <p className="text-xs text-muted-foreground">{t(`transactionDetail.travelRule.stateDescription.${travelRuleState}`)}</p>
              {assessment && (
                <>
                  <div className="flex justify-between gap-4">
                    <dt className="text-muted-foreground">{t("transactionDetail.travelRule.threshold")}</dt>
                    <dd className="text-right">{t("transactionDetail.travelRule.thresholdValue", { amount: assessment.threshold, currency: assessment.currency })}</dd>
                  </div>
                  {assessment.reason_code && (
                    <div className="flex justify-between gap-4">
                      <dt className="text-muted-foreground">{t("transactionDetail.travelRule.reasonCode")}</dt>
                      <dd className="text-right">{t(`transactionDetail.travelRule.reasonCodeLabel.${assessment.reason_code}`, { defaultValue: assessment.reason_code })}</dd>
                    </div>
                  )}
                  {(assessment.missing_fields?.length ?? 0) > 0 && (
                    <div className="flex justify-between gap-4">
                      <dt className="text-muted-foreground">{t("transactionDetail.travelRule.missingFields")}</dt>
                      <dd role="alert" className="text-right text-destructive">{assessment.missing_fields?.map((field) => t(`transactionDetail.travelRule.field.${field}`, { defaultValue: field })).join(", ")}</dd>
                    </div>
                  )}
                  <div className="flex justify-between gap-4">
                    <dt className="text-muted-foreground">{t("transactionDetail.travelRule.assessedAt")}</dt>
                    <dd className="text-right">{formatDateTime(assessment.evaluated_at, i18n.language)} · {assessment.policy_version}</dd>
                  </div>
                  {assessment.conflict && (
                    // Both readings are kept: the institution asserted one
                    // thing and the configured policy implies another.
                    <div role="alert" className="rounded-md border border-amber-300 bg-amber-50 p-2 text-amber-950">
                      <div className="font-semibold">{t("transactionDetail.travelRule.conflict")}</div>
                      <div>{t("transactionDetail.travelRule.conflictClient", { value: txn.travel_rule_applicable ? t("transactionDetail.travelRule.yes") : t("transactionDetail.travelRule.no") })}</div>
                      <div>{t("transactionDetail.travelRule.conflictServer", { value: assessment.applicable ? t("transactionDetail.travelRule.yes") : t("transactionDetail.travelRule.no") })}</div>
                    </div>
                  )}
                </>
              )}
              <div className="flex justify-between gap-4">
                <dt className="text-muted-foreground">{t("transactionDetail.travelRule.clientAssertion")}</dt>
                <dd className="text-right">{txn.travel_rule_applicable == null ? t("transactionDetail.travelRule.notAsserted") : txn.travel_rule_applicable ? t("transactionDetail.travelRule.yes") : t("transactionDetail.travelRule.no")}</dd>
              </div>
              {(txn.travel_rule_not_applicable_reason || txn.travel_rule_not_applicable_reason_code) && (
                <div className="flex justify-between gap-4">
                  <dt className="text-muted-foreground">{t("transactionDetail.travelRule.reason")}</dt>
                  <dd className="text-right">{txn.travel_rule_not_applicable_reason_code ? t(`transactionDetail.travelRule.reasonCodeLabel.${txn.travel_rule_not_applicable_reason_code}`, { defaultValue: txn.travel_rule_not_applicable_reason_code }) : txn.travel_rule_not_applicable_reason}</dd>
                </div>
              )}
              {txn.counterparty && (
                <div>
                  <dt className="mb-1 text-muted-foreground">{t("transactionDetail.travelRule.counterparty")}</dt>
                  <dd className="rounded bg-muted/50 p-2"><StructuredValue value={txn.counterparty} /></dd>
                </div>
              )}
              {txn.travel_rule_evidence && (
                <div>
                  <dt className="mb-1 text-muted-foreground">{t("transactionDetail.travelRule.evidence")}</dt>
                  <dd className="rounded bg-muted/50 p-2"><StructuredValue value={txn.travel_rule_evidence} /></dd>
                </div>
              )}
              {txn.metadata && (
                <div>
                  <dt className="mb-1 text-muted-foreground">{t("transactionDetail.travelRule.metadata")}</dt>
                  <dd className="rounded bg-muted/50 p-2"><StructuredValue value={txn.metadata} /></dd>
                </div>
              )}
            </dl>
          </CardContent>
        </Card>
      </div>

      {(investigation && (relatedAlerts.length > 0 || relatedCases.length > 0)) && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("transactionDetail.related.title")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 text-sm">
            {relatedAlerts.length > 0 && (
              <div>
                <h3 className="mb-1 font-semibold">{t("transactionDetail.related.alerts")}</h3>
                <ul className="list-inside list-disc">
                  {relatedAlerts.map((alert) => <li key={alert.id}><Link to={`/alerts/${alert.id}`} className="text-primary hover:underline">{alert.id}</Link> · {alert.description}</li>)}
                </ul>
              </div>
            )}
            {relatedCases.length > 0 && (
              <div>
                <h3 className="mb-1 font-semibold">{t("transactionDetail.related.cases")}</h3>
                <ul className="list-inside list-disc">
                  {relatedCases.map((item) => <li key={item.id}><Link to={`/cases/${item.id}`} className="text-primary hover:underline">{item.id}</Link> · {item.summary}</li>)}
                </ul>
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  )
}
