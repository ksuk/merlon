import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { CapabilityNotice } from "@/components/capability-notice"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { useCapability } from "@/hooks/use-session"
import { api, type Role, type User } from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { Plus, RotateCcw, Save } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

const roleValues: Role[] = ["admin", "analyst", "viewer"]
const inputClass = "w-full rounded-md border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"

function UserRow({ user, onChanged }: { user: User; onChanged: () => void }) {
  const { t, i18n } = useTranslation()
  const [role, setRole] = useState<Role>(user.role)
  const [active, setActive] = useState(user.active)
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  async function updateAuthority(event: React.FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError("")
    try {
      await api.users.updateAuthority(user.id, { role, active })
      onChanged()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function resetPassword(event: React.FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError("")
    try {
      await api.users.resetPassword(user.id, password)
      setPassword("")
      onChanged()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card>
      <CardContent className="space-y-4 p-4">
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium">{user.email}</span>
            <Badge variant={user.active ? "low" : "destructive"}>{user.active ? t("users.status.active") : t("users.status.inactive")}</Badge>
            <Badge variant="outline">{t(`users.roles.${user.role}`)}</Badge>
          </div>
          <p className="text-xs text-muted-foreground">{t("users.entry.created", { date: formatDateTime(user.created_at, i18n.language) })}</p>
        </div>

        <form onSubmit={updateAuthority} className="grid gap-3 border-t pt-4 sm:grid-cols-[1fr_1fr_auto] sm:items-end">
          <label className="space-y-1 text-sm">
            <span>{t("users.form.roleLabel")}</span>
            <select aria-label={t("users.entry.roleLabel", { email: user.email })} value={role} onChange={(event) => setRole(event.target.value as Role)} className={inputClass}>
              {roleValues.map((value) => <option key={value} value={value}>{t(`users.roles.${value}`)}</option>)}
            </select>
          </label>
          <label className="space-y-1 text-sm">
            <span>{t("users.form.statusLabel")}</span>
            <select aria-label={t("users.entry.statusLabel", { email: user.email })} value={active ? "active" : "inactive"} onChange={(event) => setActive(event.target.value === "active")} className={inputClass}>
              <option value="active">{t("users.status.active")}</option>
              <option value="inactive">{t("users.status.inactive")}</option>
            </select>
          </label>
          <Button type="submit" size="sm" disabled={busy || (role === user.role && active === user.active)} aria-label={t("users.entry.saveLabel", { email: user.email })}>
            <Save className="h-4 w-4" />{t("users.entry.save")}
          </Button>
        </form>

        <form onSubmit={resetPassword} className="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-end">
          <label className="space-y-1 text-sm">
            <span>{t("users.form.newPasswordLabel")}</span>
            <input type="password" minLength={12} required value={password} onChange={(event) => setPassword(event.target.value)} aria-label={t("users.entry.passwordLabel", { email: user.email })} className={inputClass} />
          </label>
          <Button type="submit" variant="outline" size="sm" disabled={busy || password.length < 12} aria-label={t("users.entry.resetLabel", { email: user.email })}>
            <RotateCcw className="h-4 w-4" />{t("users.entry.reset")}
          </Button>
        </form>
        {error && <p role="alert" className="text-sm text-destructive">{t("users.mutationError")}</p>}
      </CardContent>
    </Card>
  )
}

function UserManagementPage() {
  const { t } = useTranslation()
  const { data: users, loading, error, refetch } = useApi(api.users.list)
  const [showForm, setShowForm] = useState(false)
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [role, setRole] = useState<Role>("viewer")
  const [creating, setCreating] = useState(false)
  const [mutationError, setMutationError] = useState("")

  async function createUser(event: React.FormEvent) {
    event.preventDefault()
    setCreating(true)
    setMutationError("")
    try {
      await api.users.create({ email: email.trim(), password, role })
      setEmail("")
      setPassword("")
      setRole("viewer")
      setShowForm(false)
      refetch()
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : String(err))
    } finally {
      setCreating(false)
    }
  }

  if (loading) return <div className="space-y-6"><div className="h-8 w-40 animate-pulse rounded bg-muted" /><div className="h-48 animate-pulse rounded-xl border bg-muted" /></div>
  if (error) return <p className="p-12 text-center text-destructive">{t("users.error")}</p>

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">{t("users.title")}</h1>
        <Button size="sm" onClick={() => setShowForm((value) => !value)}><Plus className="h-4 w-4" />{t("users.createButton")}</Button>
      </div>

      {showForm && (
        <Card>
          <CardHeader><CardTitle className="text-base">{t("users.form.title")}</CardTitle></CardHeader>
          <CardContent>
            <form onSubmit={createUser} className="grid gap-4 sm:grid-cols-3">
              <label className="space-y-1 text-sm"><span>{t("users.form.emailLabel")}</span><input type="email" required value={email} onChange={(event) => setEmail(event.target.value)} className={inputClass} /></label>
              <label className="space-y-1 text-sm"><span>{t("users.form.passwordLabel")}</span><input type="password" minLength={12} required value={password} onChange={(event) => setPassword(event.target.value)} className={inputClass} /></label>
              <label className="space-y-1 text-sm"><span>{t("users.form.roleLabel")}</span><select value={role} onChange={(event) => setRole(event.target.value as Role)} className={inputClass}>{roleValues.map((value) => <option key={value} value={value}>{t(`users.roles.${value}`)}</option>)}</select></label>
              <Button type="submit" size="sm" disabled={creating || password.length < 12}>{t("users.form.submit")}</Button>
            </form>
            {mutationError && <p role="alert" className="mt-3 text-sm text-destructive">{t("users.mutationError")}</p>}
          </CardContent>
        </Card>
      )}

      <div className="space-y-3">
        {users && users.length > 0 ? users.map((user) => <UserRow key={`${user.id}:${user.updated_at}`} user={user} onChanged={refetch} />) : (
          <Card><CardContent className="p-8 text-center text-sm text-muted-foreground">{t("users.empty")}</CardContent></Card>
        )}
      </div>
    </div>
  )
}

export function UsersPage() {
  const { t } = useTranslation()
  const capability = useCapability("users.manage")

  if (capability.availability !== "available") {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold tracking-tight">{t("users.title")}</h1>
        <CapabilityNotice capabilityId="users.manage" />
      </div>
    )
  }

  return <UserManagementPage />
}
