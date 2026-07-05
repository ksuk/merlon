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

const STATUS_LABELS: Record<WhitelistEntryStatus, string> = {
  pending_approval: "承認待ち",
  active: "有効",
  expired: "期限切れ",
  revoked: "解除済み",
}

const STATUS_VARIANTS: Record<WhitelistEntryStatus, "medium" | "low" | "secondary" | "destructive"> = {
  pending_approval: "medium",
  active: "low",
  expired: "secondary",
  revoked: "destructive",
}

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString("ja-JP")
}

export function WhitelistPage() {
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
    return <p className="p-12 text-center text-destructive">ホワイトリストデータの取得に失敗しました</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">ホワイトリスト</h1>
        {canRequest && (
          <Button size="sm" onClick={() => setShowForm(!showForm)}>
            <Plus className="h-4 w-4" />
            申請
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
            <CardTitle className="text-base">ホワイトリスト申請</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="mb-1 block text-sm font-medium">顧客ID</label>
                <input
                  ref={customerIdRef}
                  required
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium">理由</label>
                <textarea
                  ref={reasonRef}
                  required
                  rows={3}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium">有効期限</label>
                <input
                  ref={validUntilRef}
                  type="date"
                  required
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium">
                  除外ルールID（任意、カンマ区切り。空欄なら全ルール除外）
                </label>
                <input
                  ref={excludedRuleIdsRef}
                  placeholder="rule-a, rule-b"
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <Button type="submit" size="sm" disabled={creating}>
                申請する
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
                  <TableHead>顧客ID</TableHead>
                  <TableHead>ステータス</TableHead>
                  <TableHead>理由</TableHead>
                  <TableHead>有効期限</TableHead>
                  <TableHead>申請者</TableHead>
                  <TableHead>操作</TableHead>
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
                          {STATUS_LABELS[entry.status]}
                        </Badge>
                      </TableCell>
                      <TableCell className="max-w-xs truncate text-sm">{entry.reason}</TableCell>
                      <TableCell className="text-xs">{formatDateTime(entry.valid_until)}</TableCell>
                      <TableCell className="font-mono text-xs">{entry.requested_by}</TableCell>
                      <TableCell>
                        <div className="flex gap-1">
                          {entry.status === "pending_approval" && (
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={!canApproveThis}
                              title={isOwnRequest ? "申請者自身は承認できません" : undefined}
                              onClick={() => handleApprove(entry.id)}
                            >
                              承認
                            </Button>
                          )}
                          {canRevokeThis && (
                            <Button variant="ghost" size="sm" onClick={() => handleRevoke(entry.id)}>
                              解除
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
              ホワイトリストエントリがありません
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
