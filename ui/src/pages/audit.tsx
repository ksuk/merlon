import { Badge } from "@/components/ui/badge"
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
import { useTranslation } from "react-i18next"

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

export function AuditPage() {
  const { t, i18n } = useTranslation()
  const actionLabels: Record<string, string> = {
    create: t("audit.action.create"),
    update: t("audit.action.update"),
    update_status: t("audit.action.update_status"),
    delete: t("audit.action.delete"),
    score_customer: t("audit.action.score_customer"),
    screen_customer: t("audit.action.screen_customer"),
    run_backtest: t("audit.action.run_backtest"),
    create_str: t("audit.action.create_str"),
  }
  const resourceLabels: Record<string, string> = {
    customers: t("audit.resource.customers"),
    transactions: t("audit.resource.transactions"),
    alerts: t("audit.resource.alerts"),
    cases: t("audit.resource.cases"),
    webhooks: t("audit.resource.webhooks"),
    batch: t("audit.resource.batch"),
    reports: t("audit.resource.reports"),
    admin: t("audit.resource.admin"),
  }
  const { data: entries, loading, error } = useApi(api.audit.list)

  if (loading) {
    return <TableSkeleton />
  }

  if (error) {
    return <p className="p-12 text-center text-destructive">{t("audit.error")}</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">{t("audit.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("audit.count", { count: entries?.length ?? 0 })}</p>
      </div>

      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("audit.table.header.timestamp")}</TableHead>
              <TableHead>{t("audit.table.header.action")}</TableHead>
              <TableHead>{t("audit.table.header.resource")}</TableHead>
              <TableHead>{t("audit.table.header.resourceId")}</TableHead>
              <TableHead>{t("audit.table.header.user")}</TableHead>
              <TableHead>{t("audit.table.header.ipAddress")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {entries && entries.length > 0 ? (
              entries.map((e) => (
                <TableRow key={e.id}>
                  <TableCell className="whitespace-nowrap text-sm">
                    {formatDateTime(e.created_at, i18n.language)}
                  </TableCell>
                  <TableCell>
                    <Badge variant="secondary">
                      {actionLabels[e.action] ?? e.action}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {resourceLabels[e.resource_type] ?? e.resource_type}
                  </TableCell>
                  <TableCell className="font-mono text-sm">
                    {e.resource_id || "-"}
                  </TableCell>
                  <TableCell>{e.user_id || "-"}</TableCell>
                  <TableCell className="font-mono text-sm">
                    {e.ip_address || "-"}
                  </TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                  {t("audit.table.empty")}
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
