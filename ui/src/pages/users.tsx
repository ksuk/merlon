import { Badge } from "@/components/ui/badge"
import { formatDateTime } from "@/lib/format"
import { Card, CardContent } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api, type Role } from "@/lib/api"
import { useTranslation } from "react-i18next"


export function UsersPage() {
  const { t, i18n } = useTranslation()
  const roles: { value: Role; label: string }[] = [
    { value: "admin", label: t("users.roles.admin") },
    { value: "analyst", label: t("users.roles.analyst") },
    { value: "viewer", label: t("users.roles.viewer") },
  ]
  const { data: users, loading, error } = useApi(api.users.list)

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-40 animate-pulse rounded bg-muted" />
        <div className="h-48 animate-pulse rounded-xl border bg-muted" />
      </div>
    )
  }

  if (error) {
    return <p className="p-12 text-center text-destructive">{t("users.error")}</p>
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{t("users.title")}</h1>

      <div className="space-y-3">
        {users && users.length > 0 ? (
          users.map((u) => (
            <Card key={u.id}>
              <CardContent className="flex items-center justify-between p-4">
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">{u.email}</span>
                    <Badge variant={u.active ? "low" : "destructive"}>
                      {u.active ? t("users.status.active") : t("users.status.inactive")}
                    </Badge>
                    <Badge variant="outline">
                      {roles.find((r) => r.value === u.role)?.label ?? u.role}
                    </Badge>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {t("users.entry.created", { date: formatDateTime(u.created_at, i18n.language) })}
                  </p>
                </div>
              </CardContent>
            </Card>
          ))
        ) : (
          <Card>
            <CardContent className="p-8 text-center text-sm text-muted-foreground">
              {t("users.empty")}
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  )
}
