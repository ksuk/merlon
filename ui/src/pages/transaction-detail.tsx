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
        <p className="text-destructive">{t("transactionDetail.error")}</p>
      </div>
    )
  }

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
                    {txn.customer_id}
                  </Link>
                </dd>
              </div>
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
      </div>
    </div>
  )
}
