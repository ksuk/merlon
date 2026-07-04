import { Badge } from "@/components/ui/badge"
import { Card, CardContent } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api, type Role } from "@/lib/api"

const ROLES: { value: Role; label: string }[] = [
  { value: "admin", label: "管理者" },
  { value: "analyst", label: "アナリスト" },
  { value: "viewer", label: "閲覧者" },
]

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

export function UsersPage() {
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
    return <p className="p-12 text-center text-destructive">ユーザ一覧の取得に失敗しました</p>
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">ユーザ管理</h1>

      <div className="space-y-3">
        {users && users.length > 0 ? (
          users.map((u) => (
            <Card key={u.id}>
              <CardContent className="flex items-center justify-between p-4">
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">{u.email}</span>
                    <Badge variant={u.active ? "low" : "destructive"}>
                      {u.active ? "有効" : "無効"}
                    </Badge>
                    <Badge variant="outline">
                      {ROLES.find((r) => r.value === u.role)?.label ?? u.role}
                    </Badge>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    作成: {formatDateTime(u.created_at)}
                  </p>
                </div>
              </CardContent>
            </Card>
          ))
        ) : (
          <Card>
            <CardContent className="p-8 text-center text-sm text-muted-foreground">
              ユーザが登録されていません
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  )
}
