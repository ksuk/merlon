import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api } from "@/lib/api"
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
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("transactionDetail.travelRule.applicable")}</dt>
                <dd>{txn.travel_rule_applicable == null ? t("transactionDetail.travelRule.legacy") : txn.travel_rule_applicable ? t("transactionDetail.travelRule.yes") : t("transactionDetail.travelRule.no")}</dd>
              </div>
              {txn.travel_rule_not_applicable_reason && (
                <div className="flex justify-between gap-4">
                  <dt className="text-muted-foreground">{t("transactionDetail.travelRule.reason")}</dt>
                  <dd className="text-right">{txn.travel_rule_not_applicable_reason}</dd>
                </div>
              )}
              {txn.counterparty && (
                <div>
                  <dt className="mb-1 text-muted-foreground">{t("transactionDetail.travelRule.counterparty")}</dt>
                  <dd><pre className="overflow-auto rounded bg-muted p-2 text-xs">{JSON.stringify(txn.counterparty, null, 2)}</pre></dd>
                </div>
              )}
              {txn.travel_rule_evidence && (
                <div>
                  <dt className="mb-1 text-muted-foreground">{t("transactionDetail.travelRule.evidence")}</dt>
                  <dd><pre className="overflow-auto rounded bg-muted p-2 text-xs">{JSON.stringify(txn.travel_rule_evidence, null, 2)}</pre></dd>
                </div>
              )}
              {txn.metadata && (
                <div>
                  <dt className="mb-1 text-muted-foreground">{t("transactionDetail.travelRule.metadata")}</dt>
                  <dd><pre className="overflow-auto rounded bg-muted p-2 text-xs">{JSON.stringify(txn.metadata, null, 2)}</pre></dd>
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
