import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { api, type Alert, type Case, type STRReport } from "@/lib/api"
import { translateApiError } from "@/lib/errors"
import { Download, FileText, RefreshCw } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

export function ReportsPage() {
  const { t } = useTranslation()
  const [refreshKey, setRefreshKey] = useState(0)
  // This page consumes bounded cursor pages. A browser view must not silently
  // materialize an unbounded number of alerts/cases/reports just to render a
  // form; the API queues own traversal and the response envelope exposes
  // whether another page exists.
  const { data: page, loading, error } = useApi(() => api.alerts.list({ sort: "risk", limit: 200 }), refreshKey)
  const { data: reportPage, loading: reportsLoading, error: reportsError } = useApi(() => api.reports.list({ limit: 200 }), refreshKey)
  const { data: candidateCasePage, loading: candidatesLoading, error: candidatesError } = useApi(() => api.cases.list({ active: true, strCandidate: true, sort: "risk", limit: 200 }), refreshKey)
  const alerts = page?.data
  const reports = reportPage?.data ?? []
  const candidateTargets = alerts && candidateCasePage
    ? candidateCasePage.data.flatMap((candidateCase: Case) => (candidateCase.alert_ids ?? []).flatMap((alertID) => {
      const alert = alerts.find((candidate) => candidate.id === alertID)
      return alert ? [{ alert, caseData: candidateCase }] : []
    }))
    : undefined
  const [result, setResult] = useState<STRReport | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [selectedAlert, setSelectedAlert] = useState<Alert | null>(null)
  const [selectedCase, setSelectedCase] = useState<Case | null>(null)
  const [selectedReport, setSelectedReport] = useState<STRReport | null>(null)
  const [suspiciousPoint, setSuspiciousPoint] = useState("")
  const [createdBy, setCreatedBy] = useState("")
  const [submissionEvidence, setSubmissionEvidence] = useState("")
  const [exporting, setExporting] = useState<string | null>(null)

  async function handleDownload(reportID: string, format: "csv" | "json") {
    setExporting(`${reportID}:${format}`)
    setActionError(null)
    try {
      await api.reports.downloadSTR(reportID, format)
    } catch (err) {
      // Keep the selected report/draft fields intact so an operator can retry
      // after a snapshot/completeness or network error.
      setActionError(translateApiError(err, t))
    } finally {
      setExporting(null)
    }
  }

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    if ((!selectedReport && (!selectedAlert || !selectedCase)) || !suspiciousPoint.trim() || !createdBy.trim()) return
    setSubmitting(true)
    setActionError(null)
    try {
      const report = selectedReport ? await api.reports.update(selectedReport.id, suspiciousPoint.trim()) : await api.reports.createSTR(selectedAlert!.id, selectedCase!.id, suspiciousPoint.trim(), createdBy.trim())
      setResult(report)
      setSelectedAlert(null)
      setSelectedCase(null)
      setSelectedReport(null)
      setSuspiciousPoint("")
      setCreatedBy("")
      setSubmissionEvidence("")
      setRefreshKey((key) => key + 1)
    } catch (err) {
      setActionError(translateApiError(err, t))
    } finally {
      setSubmitting(false)
    }
  }

  async function handleSubmitDraft(e: React.FormEvent) {
    e.preventDefault()
    if (!selectedReport || selectedReport.status !== "draft" || !submissionEvidence.trim()) return
    setSubmitting(true)
    setActionError(null)
    try {
      const current = suspiciousPoint.trim() !== selectedReport.suspicious_point ? await api.reports.update(selectedReport.id, suspiciousPoint.trim()) : selectedReport
      const report = await api.reports.submit(current.id, submissionEvidence.trim())
      setResult(report)
      setSelectedReport(null)
      setSubmissionEvidence("")
      setRefreshKey((key) => key + 1)
    } catch (err) {
      setActionError(translateApiError(err, t))
    } finally {
      setSubmitting(false)
    }
  }

  async function openDraft(report: STRReport) {
    setSubmitting(true)
    setActionError(null)
    try {
      const latest = await api.reports.get(report.id)
      setSelectedReport(latest)
      setSelectedAlert(null)
      setSelectedCase(null)
      setSuspiciousPoint(latest.suspicious_point)
      setCreatedBy(latest.created_by)
      setSubmissionEvidence(latest.submission_evidence ?? "")
    } catch (err) {
      setActionError(translateApiError(err, t))
    } finally {
      setSubmitting(false)
    }
  }

  if (loading || reportsLoading || candidatesLoading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-48 animate-pulse rounded bg-muted" />
        <div className="h-64 animate-pulse rounded-xl border bg-muted" />
      </div>
    )
  }

  if (error || reportsError || candidatesError) {
    return <div role="alert" className="space-y-3 p-12 text-center text-destructive"><p>{t("reports.error")}</p><Button type="button" variant="outline" onClick={() => setRefreshKey((key) => key + 1)}>{t("alertDetail.retry")}</Button></div>
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{t("reports.title")}</h1>

      {result && (
        <Card className="border-green-200 bg-green-50">
          <CardContent className="flex items-center justify-between p-4">
            <div>
              <p className="text-sm font-medium text-green-800">{t("reports.result.title")}</p>
              <p className="text-xs text-green-600">{t("reports.result.idLabel", { id: result.id })}</p>
            </div>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" onClick={() => handleDownload(result.id, "csv")} disabled={exporting != null} aria-busy={exporting === `${result.id}:csv`}>
                <Download className="h-4 w-4" />
                {t("reports.export.csv")}
              </Button>
              <Button variant="outline" size="sm" onClick={() => handleDownload(result.id, "json")} disabled={exporting != null} aria-busy={exporting === `${result.id}:json`}>
                <Download className="h-4 w-4" />
                {t("reports.export.json")}
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {actionError && <p role="alert" className="text-sm text-destructive">{actionError}</p>}

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <RefreshCw className="h-4 w-4" />
            {t("reports.saved.title")}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {reports.length > 0 ? (
            <div className="space-y-2">
              {reports.map((report) => (
                <div key={report.id} className="flex items-center justify-between rounded-md border p-3 text-sm">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs">{report.id}</span>
                      <Badge variant={report.status === "submitted" ? "default" : "secondary"}>{report.status === "submitted" ? t("reports.status.submitted") : t("reports.status.draft")}</Badge>
                    </div>
                    <p className="mt-1 truncate text-muted-foreground">{report.suspicious_point}</p>
                    <p className="text-xs text-muted-foreground">
                      {t("reports.saved.source", { alert: report.alert_id })} · {new Date(report.created_at).toLocaleString()}
                    </p>
                  </div>
                  <div className="ml-4 flex shrink-0 gap-2">
                    {report.status === "draft" && (
                      <Button variant="outline" size="sm" onClick={() => openDraft(report)}>
                        {t("reports.saved.open")}
                      </Button>
                    )}
                    <Button variant="outline" size="sm" onClick={() => handleDownload(report.id, "json")} disabled={exporting != null} aria-busy={exporting === `${report.id}:json`}>
                      <Download className="h-4 w-4" />
                      {t("reports.export.json")}
                    </Button>
                    <Button variant="outline" size="sm" onClick={() => handleDownload(report.id, "csv")} disabled={exporting != null} aria-busy={exporting === `${report.id}:csv`}>
                      <Download className="h-4 w-4" />
                      {t("reports.export.csv")}
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">{t("reports.saved.empty")}</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <FileText className="h-4 w-4" />
            {selectedReport ? t("reports.form.draftTitle") : t("reports.form.title")}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={selectedReport ? handleSubmitDraft : handleCreate} className="space-y-4">
            {selectedReport ? (
              <p className="rounded-md bg-muted p-3 text-sm">{t("reports.form.draftId", { id: selectedReport.id })}</p>
            ) : (
              <div>
                <label className="mb-2 block text-sm font-medium">{t("reports.form.targetAlert")}</label>
                {candidateTargets && candidateTargets.length > 0 ? (
                  <div className="max-h-48 space-y-2 overflow-y-auto">
                    {candidateTargets.map(({ alert: a, caseData }) => (
                      <button
                        key={`${caseData.id}:${a.id}`}
                        type="button"
                        onClick={() => {
                          setSelectedAlert(a)
                          setSelectedCase(caseData)
                          setSelectedReport(null)
                        }}
                        className={`flex w-full items-center justify-between rounded-md border p-3 text-left text-sm transition-colors ${selectedAlert?.id === a.id && selectedCase?.id === caseData.id ? "border-primary bg-primary/5" : "hover:bg-accent"}`}
                      >
                        <div>
                          <span className="font-mono text-xs">{a.id.slice(0, 8)}</span>
                          <span className="ml-2">{a.description}</span>
                          <span className="ml-2 text-xs text-muted-foreground">{t("reports.form.caseLabel", { id: caseData.id })}</span>
                        </div>
                        <Badge variant={a.severity === "critical" ? "critical" : a.severity === "high" ? "high" : "medium"}>{a.severity}</Badge>
                      </button>
                    ))}
                  </div>
                ) : (
                  <p className="text-sm text-muted-foreground">{t("reports.form.noAlerts")}</p>
                )}
              </div>
            )}
            <div>
              <label className="mb-1 block text-sm font-medium">{t("reports.form.pointLabel")}</label>
              <textarea
                required
                rows={3}
                value={suspiciousPoint}
                onChange={(e) => setSuspiciousPoint(e.target.value)}
                placeholder={t("reports.form.pointPlaceholder")}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
            </div>
            <div>
              <label className="mb-1 block text-sm font-medium">{t("reports.form.createdByLabel")}</label>
              <input
                required={!selectedReport}
                value={createdBy}
                onChange={(e) => setCreatedBy(e.target.value)}
                placeholder={t("reports.form.createdByPlaceholder")}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
            </div>
            {selectedReport && (
              <div>
                <label className="mb-1 block text-sm font-medium">{t("reports.form.evidenceLabel")}</label>
                <input
                  required
                  value={submissionEvidence}
                  onChange={(e) => setSubmissionEvidence(e.target.value)}
                  placeholder={t("reports.form.evidencePlaceholder")}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </div>
            )}
            <div className="flex gap-2">
              <Button type="submit" size="sm" disabled={submitting || (!selectedAlert && !selectedReport) || (!selectedReport && !selectedCase)}>
                {selectedReport ? t("reports.form.submitDraft") : t("reports.form.submit")}
              </Button>
              {selectedReport && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setSelectedReport(null)
                    setSubmissionEvidence("")
                  }}
                >
                  {t("reports.form.cancel")}
                </Button>
              )}
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
