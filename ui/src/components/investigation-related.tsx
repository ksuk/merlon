import { Link } from "react-router"
import { useTranslation } from "react-i18next"
import { Badge } from "@/components/ui/badge"
import type { Alert, AlertSeverity, Case, CasePriority, CustomerInvestigation, Transaction } from "@/lib/api"

// GET /customers/{id}/investigation returns the related transactions, alerts
// and cases, and the frontend typed all of them, but the panel rendered only
// the aggregate counts. An investigator could see that a customer had four
// alerts and had no way to reach them from the 360 view.

const SEVERITY_VARIANT: Record<AlertSeverity, "low" | "medium" | "high" | "critical"> = {
  low: "low",
  medium: "medium",
  high: "high",
  critical: "critical",
}

const PRIORITY_VARIANT: Record<CasePriority, "low" | "medium" | "high" | "critical"> = {
  low: "low",
  medium: "medium",
  high: "high",
  critical: "critical",
}

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

function formatAmount(amount: number, currency: string, locale: string) {
  try {
    return new Intl.NumberFormat(locale, { style: "currency", currency }).format(amount)
  } catch {
    return `${amount} ${currency}`
  }
}

interface SectionProps {
  id: string
  title: string
  shown: number
  total: number
  viewAllHref: string
  children: React.ReactNode
}

function Section({ id, title, shown, total, viewAllHref, children }: SectionProps) {
  const { t } = useTranslation()
  return (
    <section aria-labelledby={`investigation-${id}-heading`}>
      <div className="mb-1 flex flex-wrap items-baseline justify-between gap-2">
        <h4 id={`investigation-${id}-heading`} className="text-sm font-semibold">
          {title}
        </h4>
        {/* The panel shows one page. Without a route to the rest, a count of
            12 next to a single row reads as a bug rather than a first page. */}
        {total > shown && (
          <Link
            data-testid={`investigation-view-all-${id}`}
            to={viewAllHref}
            className="text-xs text-primary underline-offset-4 hover:underline"
          >
            {t("customerDetail.investigation.relatedViewAll", { count: total })}
          </Link>
        )}
      </div>
      {shown === 0 ? (
        <p data-testid={`investigation-empty-${id}`} className="text-sm text-muted-foreground">
          {t("customerDetail.investigation.relatedEmpty")}
        </p>
      ) : (
        <>
          <ul className="space-y-1">{children}</ul>
          {total > shown && (
            <p className="mt-1 text-xs text-muted-foreground">
              {t("customerDetail.investigation.relatedShowing", { shown, total })}
            </p>
          )}
        </>
      )}
    </section>
  )
}

function Row({ children }: { children: React.ReactNode }) {
  return <li className="flex flex-wrap items-center gap-2 rounded-md border px-3 py-2 text-sm">{children}</li>
}

export function InvestigationRelated({
  investigation,
  customerID,
}: {
  investigation: CustomerInvestigation
  customerID: string
}) {
  const { t, i18n } = useTranslation()
  const transactions: Transaction[] = investigation.transactions ?? []
  const alerts: Alert[] = investigation.alerts ?? []
  const cases: Case[] = investigation.cases ?? []
  const counts = investigation.counts ?? {}
  const scope = encodeURIComponent(customerID)

  return (
    <div data-testid="investigation-related" className="grid gap-4 md:grid-cols-3">
      <Section
        id="transactions"
        title={t("customerDetail.investigation.relatedTransactions")}
        shown={transactions.length}
        total={counts.transactions ?? transactions.length}
        viewAllHref={`/transactions?customer_id=${scope}`}
      >
        {transactions.map((tx) => (
          <Row key={tx.id}>
            <Link to={`/transactions/${tx.id}`} className="text-primary underline-offset-4 hover:underline">
              {tx.external_id || tx.id}
            </Link>
            <span className="font-mono text-xs">{formatAmount(tx.amount, tx.currency, i18n.language)}</span>
            <span className="ml-auto text-xs text-muted-foreground">
              {formatDateTime(tx.executed_at, i18n.language)}
            </span>
          </Row>
        ))}
      </Section>

      <Section
        id="alerts"
        title={t("customerDetail.investigation.relatedAlerts")}
        shown={alerts.length}
        total={counts.alerts ?? alerts.length}
        viewAllHref={`/alerts?customer_id=${scope}`}
      >
        {alerts.map((alert) => (
          <Row key={alert.id}>
            <Link to={`/alerts/${alert.id}`} className="text-primary underline-offset-4 hover:underline">
              {alert.scenario_id || alert.id}
            </Link>
            <Badge variant={SEVERITY_VARIANT[alert.severity] ?? "outline"}>
              {t(`alertSeverity.${alert.severity}`, { defaultValue: alert.severity })}
            </Badge>
            <Badge variant="outline">
              {t(`alertStatus.${alert.status}`, { defaultValue: alert.status })}
            </Badge>
            <span className="ml-auto text-xs text-muted-foreground">
              {formatDateTime(alert.detected_at, i18n.language)}
            </span>
          </Row>
        ))}
      </Section>

      <Section
        id="cases"
        title={t("customerDetail.investigation.relatedCases")}
        shown={cases.length}
        total={counts.cases ?? cases.length}
        viewAllHref={`/cases?customer_id=${scope}`}
      >
        {cases.map((kase) => (
          <Row key={kase.id}>
            <Link to={`/cases/${kase.id}`} className="text-primary underline-offset-4 hover:underline">
              {kase.summary || kase.id}
            </Link>
            <Badge variant={PRIORITY_VARIANT[kase.priority] ?? "outline"}>
              {t(`casePriority.${kase.priority}`, { defaultValue: kase.priority })}
            </Badge>
            <Badge variant="outline">
              {t(`caseStatus.${kase.status}`, { defaultValue: kase.status })}
            </Badge>
            <span className="ml-auto text-xs text-muted-foreground">
              {formatDateTime(kase.updated_at, i18n.language)}
            </span>
          </Row>
        ))}
      </Section>
    </div>
  )
}
