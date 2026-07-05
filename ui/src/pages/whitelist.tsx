import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useApi } from "@/hooks/use-api"
import { api, type WhitelistEntry, type WhitelistEntryStatus } from "@/lib/api"
import { Plus } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

const STATUS_VARIANTS: Record<WhitelistEntryStatus, "medium" | "low" | "secondary" | "destructive"> = {
  pending_approval: "medium",
  active: "low",
  expired: "secondary",
  revoked: "destructive",
}

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

export function WhitelistPage() {
  const { t, i18n } = useTranslation()
  const statusLabels: Record<WhitelistEntryStatus, string> = {
    pending_approval: t("whitelist.status.pending_approval"),
    active: t("whitelist.status.active"),
    expired: t("whitelist.status.expired"),
    revoked: t("whitelist.status.revoked"),
  }
  const { data: user } = useApi(api.auth.me)
  // whitelist:request (create/revoke) is granted to admin and analyst;
  // whitelist:approve is admin-only (auth.md §3, RolePermissions). The
  // server enforces both; these are just UI affordance hints.
  const canRequest = user?.role === "admin" || user?.role === "analyst"
  const canApprove = user?.role === "admin"

  const [entries, setEntries] = useState<WhitelistEntry[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const [showForm, setShowForm] = useState(false)
  const [creating, setCreating] = useState(false)
  const customerIdRef = useRef<HTMLInputElement>(null)
  const reasonRef = useRef<HTMLTextAreaElement>(null)
  const validUntilRef = useRef<HTMLInputElement>(null)
  const excludedRuleIdsRef = useRef<HTMLInputElement>(null)

  async function reload() {
    setLoading(true)
    try {
      const res = await api.whitelist.list()
      setEntries(res.data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    reload()
  }, [])

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    const customerId = customerIdRef.current?.value.trim()
    const reason = reasonRef.current?.value.trim()
    const validUntil = validUntilRef.current?.value
    if (!customerId || !reason || !validUntil) return

    const excludedRuleIds = excludedRuleIdsRef.current?.value
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean)

    setCreating(true)
    setActionError(null)
    try {
      await api.whitelist.create({
        customer_id: customerId,
        reason,
        valid_until: new Date(validUntil).toISOString(),
        excluded_rule_ids: excludedRuleIds && excludedRuleIds.length > 0 ? excludedRuleIds : undefined,
      })
      setShowForm(false)
      await reload()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setCreating(false)
    }
  }

  async function handleApprove(id: string) {
    setActionError(null)
    try {
      await api.whitelist.approve(id)
      await reload()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    }
  }

  async function handleRevoke(id: string) {
    setActionError(null)
    try {
      await api.whitelist.revoke(id)
      await reload()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    }
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
    return <p className="p-12 text-center text-destructive">{t("whitelist.error")}</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">{t("whitelist.title")}</h1>
        {canRequest && (
          <Button size="sm" onClick={() => setShowForm(!showForm)}>
            <Plus className="h-4 w-4" />
            {t("whitelist.requestButton")}
          </Button>
        )}
      </div>

      {actionError && (
        <p className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {actionError}
        </p>
      )}

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("whitelist.form.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="mb-1 block text-sm font-medium">{t("whitelist.form.customerId")}</label>
                <input
                  ref={customerIdRef}
                  required
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium">{t("whitelist.form.reason")}</label>
                <textarea
                  ref={reasonRef}
                  required
                  rows={3}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium">{t("whitelist.form.validUntil")}</label>
                <input
                  ref={validUntilRef}
                  type="date"
                  required
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium">
                  {t("whitelist.form.excludedRuleIds")}
                </label>
                <input
                  ref={excludedRuleIdsRef}
                  placeholder={t("whitelist.form.excludedRuleIdsPlaceholder")}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <Button type="submit" size="sm" disabled={creating}>
                {t("whitelist.form.submit")}
              </Button>
            </form>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardContent className="p-0">
          {entries && entries.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("whitelist.table.header.customerId")}</TableHead>
                  <TableHead>{t("whitelist.table.header.status")}</TableHead>
                  <TableHead>{t("whitelist.table.header.reason")}</TableHead>
                  <TableHead>{t("whitelist.table.header.validUntil")}</TableHead>
                  <TableHead>{t("whitelist.table.header.requestedBy")}</TableHead>
                  <TableHead>{t("whitelist.table.header.actions")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {entries.map((entry) => {
                  // WL-003 requires requester != approver; the server is the
                  // enforcement point (403 on self-approval) — this only
                  // disables the button as a UX hint.
                  const isOwnRequest = user != null && entry.requested_by === user.id
                  const canApproveThis = canApprove && entry.status === "pending_approval" && !isOwnRequest
                  const canRevokeThis =
                    canRequest &&
                    (entry.status === "pending_approval" || entry.status === "active")

                  return (
                    <TableRow key={entry.id}>
                      <TableCell className="font-mono text-xs">{entry.customer_id}</TableCell>
                      <TableCell>
                        <Badge variant={STATUS_VARIANTS[entry.status]}>
                          {statusLabels[entry.status]}
                        </Badge>
                      </TableCell>
                      <TableCell className="max-w-xs truncate text-sm">{entry.reason}</TableCell>
                      <TableCell className="text-xs">{formatDateTime(entry.valid_until, i18n.language)}</TableCell>
                      <TableCell className="font-mono text-xs">{entry.requested_by}</TableCell>
                      <TableCell>
                        <div className="flex gap-1">
                          {entry.status === "pending_approval" && (
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={!canApproveThis}
                              title={isOwnRequest ? t("whitelist.actions.approveDisabledTitle") : undefined}
                              onClick={() => handleApprove(entry.id)}
                            >
                              {t("whitelist.actions.approve")}
                            </Button>
                          )}
                          {canRevokeThis && (
                            <Button variant="ghost" size="sm" onClick={() => handleRevoke(entry.id)}>
                              {t("whitelist.actions.revoke")}
                            </Button>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          ) : (
            <p className="p-8 text-center text-sm text-muted-foreground">
              {t("whitelist.empty")}
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
