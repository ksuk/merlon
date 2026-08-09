import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import type { DashboardException, DashboardWorkload, WorkloadCounts } from "@/lib/api"
import { formatDuration } from "@/lib/utils"
import { AlertTriangle } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

/**
 * The dashboard's workload panels.
 *
 * The page reported totals and nothing about ownership, age or deadlines, so an
 * analyst could not answer any of the questions a shift starts with (#79).
 * Every figure here links to the queue that contains exactly those records, and
 * every figure states the scope it was taken over: "12 alerts" means nothing
 * without knowing whose.
 */

interface QueueLinks {
  base: string
  activeParam: string
}

function WorkloadTile({
  label,
  value,
  href,
  emphasis,
}: {
  label: string
  value: number | string
  href?: string
  emphasis?: "warning" | "danger"
}) {
  const tone =
    emphasis === "danger" ? "text-destructive" : emphasis === "warning" ? "text-amber-700" : ""

  const body = (
    <>
      <span className="block text-xs text-muted-foreground">{label}</span>
      <span className={`block text-2xl font-bold ${tone}`}>{value}</span>
    </>
  )

  if (!href) {
    return <div className="rounded-md border p-3">{body}</div>
  }
  return (
    <Link
      to={href}
      className="rounded-md border p-3 transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      {body}
    </Link>
  )
}

function QueuePanel({
  titleKey,
  counts,
  links,
  slaConfigured,
  hasScope,
}: {
  titleKey: string
  counts: WorkloadCounts
  links: QueueLinks
  slaConfigured: boolean
  hasScope: boolean
}) {
  const { t } = useTranslation()

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">{t(titleKey)}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid gap-2 sm:grid-cols-3">
          <WorkloadTile
            label={t("dashboard.workload.open")}
            value={counts.open}
            href={`${links.base}?${links.activeParam}`}
          />
          {hasScope ? (
            <WorkloadTile
              label={t("dashboard.workload.mine")}
              value={counts.mine}
              href={`${links.base}?${links.activeParam}&mine=true`}
            />
          ) : (
            // Without an identity there is no "mine" to count. Showing zero
            // would say nobody is assigned work, which is a different claim.
            <div className="rounded-md border border-dashed p-3">
              <span className="block text-xs text-muted-foreground">{t("dashboard.workload.mine")}</span>
              <span className="block text-xs">{t("dashboard.workload.noIdentity")}</span>
            </div>
          )}
          <WorkloadTile
            label={t("dashboard.workload.unassigned")}
            value={counts.unassigned}
            href={`${links.base}?${links.activeParam}&unassigned=true`}
            emphasis={counts.unassigned > 0 ? "warning" : undefined}
          />
        </div>

        <div>
          <p className="mb-1 text-xs font-medium">{t("dashboard.workload.ageTitle")}</p>
          <div className="grid gap-2 sm:grid-cols-4">
            {counts.age_buckets.map((bucket) => (
              <Link
                key={bucket.label}
                to={`${links.base}?${links.activeParam}&min_age_days=${Math.floor(bucket.from_hours / 24)}${
                  bucket.to_hours ? `&max_age_days=${Math.ceil(bucket.to_hours / 24)}` : ""
                }`}
                className="rounded-md border px-2 py-1 text-xs hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <span className="block text-muted-foreground">
                  {t(`dashboard.workload.ageBucket.${bucket.label}`, { defaultValue: bucket.label })}
                </span>
                <span className="font-medium">{bucket.count}</span>
              </Link>
            ))}
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            {counts.oldest_age_seconds !== undefined
              ? t("dashboard.workload.oldest", { age: formatDuration(counts.oldest_age_seconds, t) })
              : t("dashboard.workload.oldestNone")}
          </p>
        </div>

        <div>
          {slaConfigured ? (
            <div className="flex flex-wrap gap-2">
              <Link
                to={`${links.base}?${links.activeParam}&overdue=true`}
                className="rounded-md border px-2 py-1 text-xs hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <span className="block text-muted-foreground">{t("dashboard.workload.overdue")}</span>
                <span className={`font-medium ${(counts.overdue ?? 0) > 0 ? "text-destructive" : ""}`}>
                  {counts.overdue ?? 0}
                </span>
              </Link>
              <div className="rounded-md border px-2 py-1 text-xs">
                <span className="block text-muted-foreground">{t("dashboard.workload.dueSoon")}</span>
                <span className="font-medium">{counts.due_soon ?? 0}</span>
              </div>
            </div>
          ) : (
            // The whole point of #79: an unset deadline is reported as unset,
            // not as zero and not as healthy.
            <p className="text-xs text-amber-700" data-testid="sla-not-configured">
              {t("dashboard.workload.slaNotConfigured")}
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

export function DashboardWorkloadPanels({ workload }: { workload?: DashboardWorkload }) {
  const { t } = useTranslation()

  if (!workload) {
    return (
      <p role="status" className="text-sm text-muted-foreground">
        {t("dashboard.workload.unavailable")}
      </p>
    )
  }

  const slaConfigured = workload.sla.state !== "not_configured"
  const hasScope = workload.scope !== ""

  return (
    <section aria-label={t("dashboard.workload.title")} className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{t("dashboard.workload.title")}</h2>
        <Badge variant="outline">
          {hasScope
            ? t("dashboard.workload.scope", { scope: workload.scope })
            : t("dashboard.workload.scopeAll")}
        </Badge>
        <span className="text-xs text-muted-foreground">
          {t("dashboard.workload.evaluatedAt", { time: new Date(workload.evaluated_at).toLocaleString() })}
        </span>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <QueuePanel
          titleKey="dashboard.workload.alerts"
          counts={workload.alerts}
          links={{ base: "/alerts", activeParam: "active=true" }}
          slaConfigured={slaConfigured}
          hasScope={hasScope}
        />
        <QueuePanel
          titleKey="dashboard.workload.cases"
          counts={workload.cases}
          links={{ base: "/cases", activeParam: "active=true" }}
          slaConfigured={slaConfigured}
          hasScope={hasScope}
        />
      </div>
    </section>
  )
}

export function DashboardExceptions({ exceptions }: { exceptions?: DashboardException[] }) {
  const { t } = useTranslation()

  if (!exceptions) {
    return null
  }

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-base">
          <AlertTriangle className="h-4 w-4 text-amber-600" aria-hidden="true" />
          {t("dashboard.exceptions.title")}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {exceptions.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("dashboard.exceptions.none")}</p>
        ) : (
          <ul className="space-y-2">
            {exceptions.map((exception) => (
              <li key={exception.kind} className="flex flex-wrap items-center justify-between gap-2">
                <span className="text-sm">
                  {t(`dashboard.exceptions.kind.${exception.kind}`, { defaultValue: exception.kind })}
                </span>
                <span className="flex items-center gap-2">
                  <Badge variant={exception.state === "failed" ? "destructive" : "outline"}>
                    {exception.count}
                  </Badge>
                  <Link to={exception.href} className="text-xs underline">
                    {t("dashboard.exceptions.open")}
                  </Link>
                </span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}
