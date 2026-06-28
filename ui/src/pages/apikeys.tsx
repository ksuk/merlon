import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api, type Role } from "@/lib/api"
import { Copy, Key, Plus, ShieldOff } from "lucide-react"
import { useRef, useState } from "react"

const ROLES: { value: Role; label: string }[] = [
  { value: "admin", label: "管理者" },
  { value: "analyst", label: "アナリスト" },
  { value: "viewer", label: "閲覧者" },
]

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

export function APIKeysPage() {
  const { data: keys, loading, error } = useApi(api.admin.apikeys.list)
  const [showForm, setShowForm] = useState(false)
  const [creating, setCreating] = useState(false)
  const [newKey, setNewKey] = useState<string | null>(null)
  const nameRef = useRef<HTMLInputElement>(null)
  const [selectedRole, setSelectedRole] = useState<Role>("viewer")

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    const name = nameRef.current?.value.trim()
    if (!name) return
    setCreating(true)
    try {
      const res = await api.admin.apikeys.create(name, selectedRole)
      setNewKey(res.key)
      setShowForm(false)
    } finally {
      setCreating(false)
    }
  }

  async function handleRevoke(id: string) {
    await api.admin.apikeys.revoke(id)
    window.location.reload()
  }

  function copyKey() {
    if (newKey) navigator.clipboard.writeText(newKey)
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-40 animate-pulse rounded bg-muted" />
        <div className="h-48 animate-pulse rounded-xl border bg-muted" />
      </div>
    )
  }

  if (error) {
    return <p className="p-12 text-center text-destructive">APIキーの取得に失敗しました</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">APIキー管理</h1>
        <Button size="sm" onClick={() => setShowForm(!showForm)}>
          <Plus className="h-4 w-4" />
          新規作成
        </Button>
      </div>

      {newKey && (
        <Card className="border-amber-200 bg-amber-50">
          <CardContent className="flex items-center justify-between p-4">
            <div>
              <p className="text-sm font-medium text-amber-800">APIキーが生成されました（この表示は一度のみ）</p>
              <code className="mt-1 block rounded bg-amber-100 px-2 py-1 font-mono text-xs text-amber-900">
                {newKey}
              </code>
            </div>
            <Button variant="outline" size="sm" onClick={copyKey}>
              <Copy className="h-4 w-4" />
              コピー
            </Button>
          </CardContent>
        </Card>
      )}

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Key className="h-4 w-4" />
              APIキー作成
            </CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="mb-1 block text-sm font-medium">名前</label>
                <input
                  ref={nameRef}
                  required
                  placeholder="APIキーの用途..."
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label className="mb-2 block text-sm font-medium">ロール</label>
                <div className="flex gap-2">
                  {ROLES.map((r) => (
                    <button
                      key={r.value}
                      type="button"
                      onClick={() => setSelectedRole(r.value)}
                      className={`rounded-md border px-3 py-1 text-xs font-medium transition-colors ${
                        selectedRole === r.value
                          ? "border-primary bg-primary/10 text-primary"
                          : "border-input text-muted-foreground hover:bg-accent"
                      }`}
                    >
                      {r.label}
                    </button>
                  ))}
                </div>
              </div>
              <Button type="submit" size="sm" disabled={creating}>
                作成
              </Button>
            </form>
          </CardContent>
        </Card>
      )}

      <div className="space-y-3">
        {keys && keys.length > 0 ? (
          keys.map((k) => (
            <Card key={k.id}>
              <CardContent className="flex items-center justify-between p-4">
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">{k.name}</span>
                    <Badge variant={k.active ? "low" : "destructive"}>
                      {k.active ? "有効" : "無効"}
                    </Badge>
                    <Badge variant="outline">
                      {ROLES.find((r) => r.value === k.role)?.label ?? k.role}
                    </Badge>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    作成: {formatDateTime(k.created_at)}
                    {k.last_used && ` | 最終使用: ${formatDateTime(k.last_used)}`}
                  </p>
                </div>
                {k.active && (
                  <Button variant="ghost" size="sm" onClick={() => handleRevoke(k.id)}>
                    <ShieldOff className="h-4 w-4 text-destructive" />
                    無効化
                  </Button>
                )}
              </CardContent>
            </Card>
          ))
        ) : (
          <Card>
            <CardContent className="p-8 text-center text-sm text-muted-foreground">
              APIキーが登録されていません
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  )
}
