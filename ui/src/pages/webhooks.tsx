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
import { PagePurpose } from "@/components/page-purpose"
import { useApi } from "@/hooks/use-api"
import { api, type WebhookDLQEntry, type WebhookDelivery, type WebhookEventType } from "@/lib/api"
import { ChevronDown, ChevronUp, Plus, Trash2 } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

export function WebhooksPage() {
  const { t, i18n } = useTranslation()
  const allEvents: { value: WebhookEventType; label: string }[] = [
    { value: "alert.created", label: t("webhooks.events.alertCreated") },
    { value: "alert.resolved", label: t("webhooks.events.alertResolved") },
    { value: "case.created", label: t("webhooks.events.caseCreated") },
    { value: "case.updated", label: t("webhooks.events.caseUpdated") },
    { value: "case.closed", label: t("webhooks.events.caseClosed") },
    { value: "str.created", label: t("webhooks.events.strCreated") },
    { value: "score.changed", label: t("webhooks.events.scoreChanged") },
    { value: "screening.match", label: t("webhooks.events.screeningMatch") },
    { value: "screening_true_positive", label: t("webhooks.events.screeningTruePositive") },
    { value: "edd_required", label: t("webhooks.events.eddRequired") },
    { value: "transaction_restriction_recommended", label: t("webhooks.events.transactionRestrictionRecommended") },
    { value: "relationship_decline_recommended", label: t("webhooks.events.relationshipDeclineRecommended") },
  ]
  function eventLabel(event: WebhookEventType) {
    return allEvents.find((ae) => ae.value === event)?.label ?? event
  }
  const { data: webhooks, loading, error } = useApi(api.webhooks.list)
  const [showForm, setShowForm] = useState(false)
  const [creating, setCreating] = useState(false)
  const urlRef = useRef<HTMLInputElement>(null)
  const [selectedEvents, setSelectedEvents] = useState<WebhookEventType[]>([])
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)
  const [deliveries, setDeliveries] = useState<WebhookDelivery[]>([])
  const [loadingDeliveries, setLoadingDeliveries] = useState(false)

  // DLQ タブ（the HTTP API contract §3.1「DLQ内イベントの再処理はUI上から実行可能」）。
  const [tab, setTab] = useState<"webhooks" | "dlq">("webhooks")
  const [dlqEntries, setDlqEntries] = useState<WebhookDLQEntry[] | null>(null)
  const [loadingDlq, setLoadingDlq] = useState(false)
  const [reprocessingId, setReprocessingId] = useState<string | null>(null)

  async function loadDlq() {
    setLoadingDlq(true)
    try {
      const entries = await api.webhooks.listDLQ()
      setDlqEntries(entries)
    } finally {
      setLoadingDlq(false)
    }
  }

  useEffect(() => {
    // Start the load outside the effect's synchronous phase: the setState at the
    // head of loadDlq() would otherwise run synchronously inside the effect, which
    // cascades an extra render.
    if (tab === "dlq") {
      void Promise.resolve().then(loadDlq)
    }
  }, [tab])

  async function handleReprocess(id: string) {
    setReprocessingId(id)
    try {
      await api.webhooks.reprocessDLQ(id)
      await loadDlq()
    } finally {
      setReprocessingId(null)
    }
  }

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    const url = urlRef.current?.value.trim()
    if (!url || selectedEvents.length === 0) return
    setCreating(true)
    try {
      await api.webhooks.create(url, selectedEvents)
      window.location.reload()
    } catch {
      setCreating(false)
    }
  }

  // Deletion is confirmed in place rather than performed on the first click.
  // The confirmation names the endpoint that stops receiving events, because
  // an operator scanning a list of similar URLs cannot otherwise tell which
  // row they are about to silence.
  async function handleDelete(id: string) {
    setPendingDelete(null)
    await api.webhooks.delete(id)
    window.location.reload()
  }

  function toggleEvent(event: WebhookEventType) {
    setSelectedEvents((prev) =>
      prev.includes(event) ? prev.filter((e) => e !== event) : [...prev, event],
    )
  }

  async function toggleDeliveries(id: string) {
    if (expandedId === id) {
      setExpandedId(null)
      return
    }
    setExpandedId(id)
    setLoadingDeliveries(true)
    try {
      const data = await api.webhooks.deliveries(id)
      setDeliveries(data)
    } finally {
      setLoadingDeliveries(false)
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
    return <p className="p-12 text-center text-destructive">{t("webhooks.error")}</p>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight">{t("webhooks.title")}</h1>
        {tab === "webhooks" && (
          <Button size="sm" onClick={() => setShowForm(!showForm)}>
            <Plus className="h-4 w-4" />
            {t("webhooks.createButton")}
          </Button>
        )}
      </div>

      <PagePurpose
        capabilityId="webhooks.manage"
        bodyKey="webhooks.purpose.body"
        points={[
          "webhooks.purpose.consumers",
          "webhooks.purpose.delivery",
          "webhooks.purpose.payload",
          "webhooks.purpose.removal",
          "webhooks.purpose.owner",
        ]}
      />

      <div className="flex gap-2 border-b">
        <button
          type="button"
          onClick={() => setTab("webhooks")}
          className={`border-b-2 px-3 py-2 text-sm font-medium ${
            tab === "webhooks" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"
          }`}
        >
          {t("webhooks.tabs.webhooks")}
        </button>
        <button
          type="button"
          onClick={() => setTab("dlq")}
          className={`border-b-2 px-3 py-2 text-sm font-medium ${
            tab === "dlq" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"
          }`}
        >
          {t("webhooks.tabs.dlq")}
        </button>
      </div>

      {tab === "dlq" ? (
        loadingDlq ? (
          <div className="h-48 animate-pulse rounded-xl border bg-muted" />
        ) : dlqEntries && dlqEntries.length > 0 ? (
          <div className="rounded-xl border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("webhooks.dlq.table.header.event")}</TableHead>
                  <TableHead>{t("webhooks.dlq.table.header.attemptCount")}</TableHead>
                  <TableHead>{t("webhooks.dlq.table.header.lastError")}</TableHead>
                  <TableHead>{t("webhooks.dlq.table.header.failedAt")}</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {dlqEntries.map((entry) => (
                  <TableRow key={entry.id}>
                    <TableCell className="text-xs">{eventLabel(entry.event)}</TableCell>
                    <TableCell>{entry.attempt_count}</TableCell>
                    <TableCell className="max-w-[300px] truncate text-xs text-destructive">
                      {entry.last_error ?? "-"}
                    </TableCell>
                    <TableCell className="text-xs">{formatDateTime(entry.failed_at, i18n.language)}</TableCell>
                    <TableCell>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={reprocessingId === entry.id}
                        onClick={() => handleReprocess(entry.id)}
                      >
                        {t("webhooks.dlq.reprocess")}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        ) : (
          <Card>
            <CardContent className="p-8 text-center text-sm text-muted-foreground">
              {t("webhooks.dlq.empty")}
            </CardContent>
          </Card>
        )
      ) : (
        <>
          {showForm && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("webhooks.form.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="mb-1 block text-sm font-medium">{t("webhooks.form.urlLabel")}</label>
                <input
                  ref={urlRef}
                  type="url"
                  required
                  placeholder="https://example.com/webhook"
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label className="mb-2 block text-sm font-medium">{t("webhooks.form.eventsLabel")}</label>
                <div className="flex flex-wrap gap-2">
                  {allEvents.map((evt) => (
                    <button
                      key={evt.value}
                      type="button"
                      onClick={() => toggleEvent(evt.value)}
                      className={`rounded-md border px-3 py-1 text-xs font-medium transition-colors ${
                        selectedEvents.includes(evt.value)
                          ? "border-primary bg-primary/10 text-primary"
                          : "border-input text-muted-foreground hover:bg-accent"
                      }`}
                    >
                      {evt.label}
                    </button>
                  ))}
                </div>
              </div>
              <Button type="submit" size="sm" disabled={creating || selectedEvents.length === 0}>
                {t("webhooks.form.submit")}
              </Button>
            </form>
          </CardContent>
        </Card>
      )}

      <div className="space-y-3">
        {webhooks && webhooks.length > 0 ? (
          webhooks.map((w) => (
            <Card key={w.id}>
              <CardContent className="p-4">
                <div className="flex items-center justify-between">
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-sm">{w.url}</span>
                      <Badge variant={w.active ? "low" : "secondary"}>
                        {w.active ? t("webhooks.status.active") : t("webhooks.status.inactive")}
                      </Badge>
                    </div>
                    <div className="flex flex-wrap gap-1">
                      {w.events.map((e) => (
                        <Badge key={e} variant="outline" className="text-xs">
                          {eventLabel(e)}
                        </Badge>
                      ))}
                    </div>
                    <p className="text-xs text-muted-foreground">
                      {t("webhooks.entry.createdAt", { date: formatDateTime(w.created_at, i18n.language) })}
                    </p>
                  </div>
                  <div className="flex gap-1">
                    <Button variant="ghost" size="sm" onClick={() => toggleDeliveries(w.id)}>
                      {expandedId === w.id ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                      {t("webhooks.entry.deliveries")}
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={t("webhooks.entry.deleteLabel")}
                      title={t("webhooks.entry.deleteLabel")}
                      onClick={() => setPendingDelete(w.id)}
                    >
                      <Trash2 className="h-4 w-4 text-destructive" aria-hidden="true" />
                    </Button>
                  </div>
                </div>
                {pendingDelete === w.id && (
                  <div role="alertdialog" aria-label={t("webhooks.entry.confirmTitle")} className="mt-3 rounded-md border border-destructive/40 bg-destructive/5 p-3">
                    <p className="text-sm font-medium">{t("webhooks.entry.confirmTitle")}</p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {t("webhooks.entry.confirmDetail", { url: w.url, count: w.events.length })}
                    </p>
                    <div className="mt-2 flex gap-2">
                      <Button size="sm" variant="destructive" onClick={() => void handleDelete(w.id)}>
                        {t("webhooks.entry.confirmDelete")}
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => setPendingDelete(null)}>
                        {t("webhooks.entry.cancelDelete")}
                      </Button>
                    </div>
                  </div>
                )}
                {expandedId === w.id && (
                  <div className="mt-3 border-t pt-3">
                    {loadingDeliveries ? (
                      <div className="h-16 animate-pulse rounded bg-muted" />
                    ) : deliveries.length > 0 ? (
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead>{t("webhooks.deliveries.table.header.event")}</TableHead>
                            <TableHead>{t("webhooks.deliveries.table.header.status")}</TableHead>
                            <TableHead>{t("webhooks.deliveries.table.header.result")}</TableHead>
                            <TableHead>{t("webhooks.deliveries.table.header.timestamp")}</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {deliveries.map((d) => (
                            <TableRow key={d.id}>
                              <TableCell className="text-xs">
                                {eventLabel(d.event)}
                              </TableCell>
                              <TableCell>{d.status_code || "-"}</TableCell>
                              <TableCell>
                                <Badge variant={d.success ? "low" : "destructive"}>
                                  {d.success ? t("webhooks.deliveries.success") : t("webhooks.deliveries.failure")}
                                </Badge>
                                {d.error && <span className="ml-2 text-xs text-destructive">{d.error}</span>}
                              </TableCell>
                              <TableCell className="text-xs">{formatDateTime(d.created_at, i18n.language)}</TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    ) : (
                      <p className="py-4 text-center text-sm text-muted-foreground">{t("webhooks.deliveries.empty")}</p>
                    )}
                  </div>
                )}
              </CardContent>
            </Card>
          ))
        ) : (
          <Card>
            <CardContent className="p-8 text-center text-sm text-muted-foreground">
              {t("webhooks.empty")}
            </CardContent>
          </Card>
        )}
          </div>
        </>
      )}
    </div>
  )
}
