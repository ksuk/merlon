import { Badge } from "@/components/ui/badge"
import { formatDateTime } from "@/lib/format"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { PagePurpose } from "@/components/page-purpose"
import { useApi } from "@/hooks/use-api"
import { api, type Role } from "@/lib/api"
import { Copy, Key, Plus, ShieldOff } from "lucide-react"
import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"


export function APIKeysPage() {
  const { t, i18n } = useTranslation()
  const roles: { value: Role; label: string }[] = [
    { value: "admin", label: t("apikeys.roles.admin") },
    { value: "analyst", label: t("apikeys.roles.analyst") },
    { value: "viewer", label: t("apikeys.roles.viewer") },
  ]
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
    return <p className="p-12 text-center text-destructive">{t("apikeys.error")}</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">{t("apikeys.title")}</h1>
        <Button size="sm" onClick={() => setShowForm(!showForm)}>
          <Plus className="h-4 w-4" />
          {t("apikeys.createButton")}
        </Button>
      </div>

      <PagePurpose
        capabilityId="api_keys.manage"
        bodyKey="apikeys.purpose.body"
        points={[
          "apikeys.purpose.consumers",
          "apikeys.purpose.permission",
          "apikeys.purpose.display",
          "apikeys.purpose.revocation",
          "apikeys.purpose.owner",
        ]}
      />

      {newKey && (
        <Card className="border-amber-200 bg-amber-50">
          <CardContent className="flex items-center justify-between p-4">
            <div>
              <p className="text-sm font-medium text-amber-800">{t("apikeys.newKey.title")}</p>
              <code className="mt-1 block rounded bg-amber-100 px-2 py-1 font-mono text-xs text-amber-900">
                {newKey}
              </code>
            </div>
            <Button variant="outline" size="sm" onClick={copyKey}>
              <Copy className="h-4 w-4" />
              {t("apikeys.newKey.copy")}
            </Button>
          </CardContent>
        </Card>
      )}

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Key className="h-4 w-4" />
              {t("apikeys.form.title")}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="mb-1 block text-sm font-medium">{t("apikeys.form.nameLabel")}</label>
                <input
                  ref={nameRef}
                  required
                  placeholder={t("apikeys.form.namePlaceholder")}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label className="mb-2 block text-sm font-medium">{t("apikeys.form.roleLabel")}</label>
                <div className="flex gap-2">
                  {roles.map((r) => (
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
                {t("apikeys.form.submit")}
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
                      {k.active ? t("apikeys.status.active") : t("apikeys.status.inactive")}
                    </Badge>
                    <Badge variant="outline">
                      {roles.find((r) => r.value === k.role)?.label ?? k.role}
                    </Badge>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {t("apikeys.entry.created", { date: formatDateTime(k.created_at, i18n.language) })}
                    {k.last_used && t("apikeys.entry.lastUsed", { date: formatDateTime(k.last_used, i18n.language) })}
                  </p>
                </div>
                {k.active && (
                  <Button variant="ghost" size="sm" onClick={() => handleRevoke(k.id)}>
                    <ShieldOff className="h-4 w-4 text-destructive" />
                    {t("apikeys.revoke")}
                  </Button>
                )}
              </CardContent>
            </Card>
          ))
        ) : (
          <Card>
            <CardContent className="p-8 text-center text-sm text-muted-foreground">
              {t("apikeys.empty")}
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  )
}
