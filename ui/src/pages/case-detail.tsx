import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useApi } from "@/hooks/use-api"
import { ApiError, api, type Case, type CaseFile, type CasePriority, type CaseStatus, type Customer, type RelatedCase, type STRReport } from "@/lib/api"
import { translateApiError } from "@/lib/errors"
import { ArrowLeft, Send } from "lucide-react"
import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useParams } from "react-router"

const PRIORITY_VARIANT: Record<CasePriority, "low" | "medium" | "high" | "critical"> = {
  low: "low",
  medium: "medium",
  high: "high",
  critical: "critical",
}

function formatDateTime(iso: string, locale: string) {
  return new Date(iso).toLocaleString(locale)
}

export function CaseDetailPage() {
  const { t, i18n } = useTranslation()
  const priorityLabels: Record<string, string> = {
    low: t("casePriority.low"),
    medium: t("casePriority.medium"),
    high: t("casePriority.high"),
    critical: t("casePriority.critical"),
  }
  const statusLabels: Record<CaseStatus, string> = {
    open: t("caseStatus.open"),
    new: t("caseStatus.new"),
    investigating: t("caseStatus.investigating"),
    escalated: t("caseStatus.escalated"),
    closed: t("caseStatus.closed"),
    reopened: t("caseStatus.reopened"),
    str_filed: t("caseStatus.str_filed"),
  }
  // the case-management workflow §ケースのステータス遷移の遷移図どおり
  // （NEW→INVESTIGATING→{ESCALATED→INVESTIGATING(差し戻し), CLOSED, STR_FILED}）。
  // CLOSED→REOPENED は理由必須・Analyst以上のため、別途の再オープンフォームで扱う
  // （このテーブルには含めない）。
  const statusTransitions: Record<CaseStatus, { label: string; value: CaseStatus }[]> = {
    open: [{ label: t("caseDetail.transitions.startInvestigation"), value: "investigating" }],
    new: [{ label: t("caseDetail.transitions.startInvestigation"), value: "investigating" }],
    investigating: [
      { label: t("caseDetail.transitions.escalate"), value: "escalated" },
      { label: t("caseDetail.transitions.close"), value: "closed" },
      { label: t("caseDetail.transitions.fileStr"), value: "str_filed" },
    ],
    escalated: [{ label: t("caseDetail.transitions.rollback"), value: "investigating" }],
    closed: [],
    reopened: [{ label: t("caseDetail.transitions.reinvestigate"), value: "investigating" }],
    str_filed: [],
  }
  const eddStageLabels: { key: keyof Customer; label: string; variant: "medium" | "high" | "critical" }[] = [
    { key: "edd_stage3_notified_at", label: t("caseDetail.edd.stage3"), variant: "critical" },
    { key: "edd_stage2_notified_at", label: t("caseDetail.edd.stage2"), variant: "high" },
    { key: "edd_stage1_last_sent_at", label: t("caseDetail.edd.stage1"), variant: "medium" },
  ]
  // eddStageDisplay picks the highest-reached EDD escalation stage for
  // display (the case-management workflow §EDD未実施継続時の段階的措置). Returns null
  // when the customer has no open EDD requirement.
  function eddStageDisplay(customer: Customer | null) {
    if (!customer?.edd_requested_at) return null
    for (const stage of eddStageLabels) {
      if (customer[stage.key]) return stage
    }
    return { key: "edd_requested_at" as const, label: t("caseDetail.edd.requested"), variant: "medium" as const }
  }
  const { id } = useParams<{ id: string }>()
	const { data: caseData, loading, error } = useApi(
	  useCallback(() => api.cases.get(id!), [id]),
	)
	const latestCaseUpdatedAt = useRef<string | undefined>(undefined)
  const { data: directory } = useApi(api.operators.directory)
  const [updating, setUpdating] = useState(false)
  const [conflictError, setConflictError] = useState<string | null>(null)
  const [addingNote, setAddingNote] = useState(false)
  const [noteError, setNoteError] = useState<string | null>(null)
  const noteRef = useRef<HTMLTextAreaElement>(null)

  // ケース間の関連付け（the case-management workflow §ケース間の関連付け）。
  const [relatedCases, setRelatedCases] = useState<RelatedCase[] | null>(null)
  const [relatedLoadError, setRelatedLoadError] = useState<string | null>(null)
  const [caseFile, setCaseFile] = useState<CaseFile | null>(null)
  const [caseFileLoadError, setCaseFileLoadError] = useState<string | null>(null)
  const [relatedFormOpen, setRelatedFormOpen] = useState(false)
  const [relatedCaseID, setRelatedCaseID] = useState("")
  const [relatedType, setRelatedType] = useState("related")
  const [relatedRationale, setRelatedRationale] = useState("")
  const [relatedSearch, setRelatedSearch] = useState("")
  const [relatedCandidates, setRelatedCandidates] = useState<Case[]>([])
  const [relatedCandidatesTruncated, setRelatedCandidatesTruncated] = useState(false)
  const [relatedBusy, setRelatedBusy] = useState(false)
  const [relatedError, setRelatedError] = useState<string | null>(null)
  const [correctingRelationshipID, setCorrectingRelationshipID] = useState<string | null>(null)
  const [correctionType, setCorrectionType] = useState("")
  const [correctionRationale, setCorrectionRationale] = useState("")
  const [removingRelationshipID, setRemovingRelationshipID] = useState<string | null>(null)
  const [removalReason, setRemovalReason] = useState("")
  const removeDialogRef = useRef<HTMLDivElement>(null)
  const removeReasonRef = useRef<HTMLInputElement>(null)
  const removeTriggerRef = useRef<HTMLButtonElement>(null)

  const [filingOpen, setFilingOpen] = useState(false)
  const [filingReports, setFilingReports] = useState<STRReport[] | null>(null)
  const [filingReportsTruncated, setFilingReportsTruncated] = useState(false)
  const [filingReportID, setFilingReportID] = useState("")
  const [filingChannel, setFilingChannel] = useState("")
  const [filingDestination, setFilingDestination] = useState("")
  const [filingExternalReference, setFilingExternalReference] = useState("")
  const [filingRationale, setFilingRationale] = useState("STR filing confirmed")
  const [filingBusy, setFilingBusy] = useState(false)
  const [filingError, setFilingError] = useState<string | null>(null)
  const [candidateBusy, setCandidateBusy] = useState(false)
  const [caseFileExportBusy, setCaseFileExportBusy] = useState(false)
  const [caseFileExportError, setCaseFileExportError] = useState<string | null>(null)

  const [closeRationale, setCloseRationale] = useState("")
  const [closeConfirmOpen, setCloseConfirmOpen] = useState(false)
  const closeDialogRef = useRef<HTMLDivElement>(null)
  const closeRationaleRef = useRef<HTMLTextAreaElement>(null)
  const closeTriggerRef = useRef<HTMLButtonElement>(null)
  const [assignmentTo, setAssignmentTo] = useState("")
  const [assignmentTeam, setAssignmentTeam] = useState("")
  const [assignmentDueAt, setAssignmentDueAt] = useState("")
  const [assignmentPriority, setAssignmentPriority] = useState<CasePriority>("medium")
  const [summaryDraft, setSummaryDraft] = useState("")
  const [assignmentBusy, setAssignmentBusy] = useState(false)
  const [evidenceDescription, setEvidenceDescription] = useState("")
  const [evidenceSource, setEvidenceSource] = useState("")
  const [evidenceType, setEvidenceType] = useState("")
  const [evidenceCollector, setEvidenceCollector] = useState("")
  const [evidenceHash, setEvidenceHash] = useState("")
  const [evidenceBusy, setEvidenceBusy] = useState(false)
  const [evidenceError, setEvidenceError] = useState<string | null>(null)
  const [correctingEvidenceID, setCorrectingEvidenceID] = useState<string | null>(null)
  const [evidenceCorrectionReason, setEvidenceCorrectionReason] = useState("")
  const [workTitle, setWorkTitle] = useState("")
  const [workBusy, setWorkBusy] = useState(false)
  const [workError, setWorkError] = useState<string | null>(null)
  const [checklistBusy, setChecklistBusy] = useState<string | null>(null)

  // EDD段階表示（the case-management workflow §EDD未実施継続時の段階的措置）。
  const [customer, setCustomer] = useState<Customer | null>(null)
  const [customerLoadError, setCustomerLoadError] = useState<string | null>(null)

  // 再オープン（理由必須・Analyst以上、the case-management workflow「再オープン時は
  // 理由（テキスト、必須）を記録する」）。
  const [reopenReason, setReopenReason] = useState("")
  const [reopening, setReopening] = useState(false)
  const [reopenError, setReopenError] = useState<string | null>(null)

  useEffect(() => {
    if (!closeConfirmOpen) return
    closeRationaleRef.current?.focus()
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== "Escape") return
      event.preventDefault()
      setCloseConfirmOpen(false)
      closeTriggerRef.current?.focus()
    }
    document.addEventListener("keydown", onKeyDown)
    return () => document.removeEventListener("keydown", onKeyDown)
  }, [closeConfirmOpen])

  const loadRelatedCases = useCallback(async () => {
    if (!id) return
    setRelatedLoadError(null)
    try {
      setRelatedCases(await api.cases.related(id))
    } catch (err) {
      setRelatedLoadError(translateApiError(err, t))
    }
  }, [id, t])

	const loadCaseFile = useCallback(async () => {
    if (!id) return
    setCaseFileLoadError(null)
    try {
	    const result = await api.cases.file(id)
	    setCaseFile(result && typeof result === "object" ? result : null)
		    if (result?.case?.updated_at) latestCaseUpdatedAt.current = result.case.updated_at
    } catch (err) {
      setCaseFileLoadError(translateApiError(err, t))
    }
  }, [id, t])

  useEffect(() => {
    void Promise.resolve().then(loadRelatedCases)
  }, [loadRelatedCases])

  useEffect(() => {
    void Promise.resolve().then(loadCaseFile)
	}, [loadCaseFile])

	useEffect(() => {
	  if (caseData?.updated_at) latestCaseUpdatedAt.current = caseData.updated_at
	}, [caseData?.updated_at])

	useEffect(() => {
	    if (!caseData?.customer_id) return
	    let cancelled = false
	    void Promise.resolve().then(() => {
	      if (cancelled) return null
	      setCustomerLoadError(null)
	      return api.customers.get(caseData.customer_id)
	    }).then((customer) => {
	      if (!cancelled && customer) setCustomer(customer)
	    }).catch((err) => {
	      if (!cancelled) setCustomerLoadError(translateApiError(err, t))
	    })
	    return () => { cancelled = true }
	  }, [caseData?.customer_id, t])

  useEffect(() => {
    if (!caseData) return
    void Promise.resolve().then(() => {
      setAssignmentTo(caseData.assigned_to ?? "")
      setAssignmentTeam(caseData.assigned_team ?? "")
      setAssignmentDueAt(caseData.due_at ? caseData.due_at.slice(0, 16) : "")
      setAssignmentPriority(caseData.priority)
      setSummaryDraft(caseData.summary)
    })
  }, [caseData])

  async function handleSaveAssignment(e: React.FormEvent) {
    e.preventDefault()
    if (!id) return
    setAssignmentBusy(true)
    setConflictError(null)
    try {
      await api.cases.update(id, {
        assigned_to: assignmentTo,
        assigned_team: assignmentTeam,
        priority: assignmentPriority,
        summary: summaryDraft.trim(),
        ...(assignmentDueAt ? { due_at: new Date(assignmentDueAt).toISOString() } : { clear_due_at: true }),
        expected_updated_at: caseData!.updated_at,
      })
      window.location.reload()
    } catch (err) {
      setConflictError(err instanceof ApiError && err.status === 409 ? t("caseDetail.conflict") : translateApiError(err, t))
      setAssignmentBusy(false)
    }
  }

  async function openFilingForm() {
    setFilingOpen(true)
    setFilingError(null)
    setFilingReports(null)
    setFilingReportsTruncated(false)
    if (!caseData) return
    try {
      // Filing lookup is a bounded search. The UI must not collect every
      // report page into the browser; the operator can enter a report ID
      // directly when the server reports more matches.
      const reports = await api.reports.list({ customerId: caseData.customer_id, status: "submitted", limit: 200 })
      setFilingReports(reports.data.filter((report) => report.case_id === caseData.id))
      setFilingReportsTruncated(reports.pagination.has_more)
    } catch (err) {
      setFilingError(translateApiError(err, t))
    }
  }

  async function submitStatusUpdate(status: CaseStatus, rationale?: string) {
    if (!id) return
    setUpdating(true)
    setConflictError(null)
    try {
      await api.cases.update(id, {
        status,
        expected_updated_at: caseData!.updated_at,
        ...(status === "closed" ? { rationale: rationale?.trim(), confirm: true } : {}),
      })
      window.location.reload()
    } catch (err) {
      setConflictError(
        err instanceof ApiError && err.status === 409
          ? t("caseDetail.conflict")
          : translateApiError(err, t),
      )
      setUpdating(false)
    }
  }

  async function handleStatusChange(status: CaseStatus) {
    if (status === "str_filed") {
      await openFilingForm()
      return
    }
    if (status === "closed") {
      setCloseConfirmOpen(true)
      setConflictError(null)
      return
    }
    await submitStatusUpdate(status)
  }

  async function handleConfirmClose(e: React.FormEvent) {
    e.preventDefault()
    if (!closeRationale.trim()) {
      setConflictError(t("caseDetail.transitions.closeRationaleRequired"))
      return
    }
    await submitStatusUpdate("closed", closeRationale)
  }

  async function handleFileCase(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !filingReportID.trim() || !filingChannel.trim() || !filingDestination.trim() || !filingExternalReference.trim() || !filingRationale.trim()) {
      setFilingError(t("caseDetail.filing.required"))
      return
    }
    setFilingBusy(true)
    setFilingError(null)
    setConflictError(null)
    try {
      await api.cases.update(id, {
        status: "str_filed",
        str_report_id: filingReportID.trim(),
        filing_channel: filingChannel.trim(),
        destination: filingDestination.trim(),
        external_reference: filingExternalReference.trim(),
        rationale: filingRationale.trim(),
        confirm: true,
        expected_updated_at: caseData!.updated_at,
      })
      window.location.reload()
    } catch (err) {
      setFilingError(err instanceof ApiError && err.status === 409 ? t("caseDetail.conflict") : translateApiError(err, t))
      setFilingBusy(false)
    }
  }

  async function handleExportCaseFile() {
    if (!caseData) return
    setCaseFileExportBusy(true)
    setCaseFileExportError(null)
    try {
      await api.cases.downloadFile(caseData.id)
    } catch (err) {
      setCaseFileExportError(translateApiError(err, t) || t("caseDetail.caseFile.exportError"))
    } finally {
      setCaseFileExportBusy(false)
    }
  }

  async function handleMarkCandidate() {
    if (!id) return
    setCandidateBusy(true)
    setConflictError(null)
    try {
      await api.cases.update(id, {
        str_candidate: true,
        investigation_disposition: "str_candidate",
        rationale: "STR candidate review",
        expected_updated_at: caseData!.updated_at,
      })
      window.location.reload()
    } catch (err) {
      setConflictError(err instanceof ApiError && err.status === 409 ? t("caseDetail.conflict") : translateApiError(err, t))
      setCandidateBusy(false)
    }
  }

  async function findRelatedCandidates() {
    if (!caseData) return
    setRelatedBusy(true)
    setRelatedError(null)
    try {
      // Related-case search is a bounded server-side page. It must not turn
      // a detail view into an unbounded queue traversal.
      const page = await api.cases.list({ customerId: caseData.customer_id, search: relatedSearch.trim() || undefined, sort: "risk", limit: 200 })
      setRelatedCandidates((page?.data ?? []).filter((candidate) => candidate.id !== caseData.id && candidate.customer_id === caseData.customer_id && ["open", "new", "investigating", "escalated", "reopened"].includes(candidate.status)))
      setRelatedCandidatesTruncated(page.pagination.has_more)
    } catch (err) {
      setRelatedError(translateApiError(err, t))
    } finally {
      setRelatedBusy(false)
    }
  }

  async function handleAddRelated(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !relatedCaseID.trim() || !relatedRationale.trim()) {
      setRelatedError(t("caseDetail.relatedCases.error"))
      return
    }
    setRelatedBusy(true)
    setRelatedError(null)
    try {
	      await api.cases.addRelated(id, relatedCaseID.trim(), relatedType.trim() || "related", relatedRationale.trim(), latestCaseUpdatedAt.current ?? caseData?.updated_at)
      setRelatedFormOpen(false)
	      setRelatedCaseID("")
	      setRelatedRationale("")
	      await loadRelatedCases()
	      await loadCaseFile()
    } catch (err) {
      setRelatedError(translateApiError(err, t))
    } finally {
      setRelatedBusy(false)
    }
  }

  useEffect(() => {
    if (!removingRelationshipID) return
    const dialog = removeDialogRef.current
    const first = removeReasonRef.current
    first?.focus()
    if (!dialog) return
    const dialogElement = dialog
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.preventDefault()
        setRemovingRelationshipID(null)
        setRemovalReason("")
        removeTriggerRef.current?.focus()
        return
      }
      if (event.key !== "Tab") return
      const focusable = Array.from(dialogElement.querySelectorAll<HTMLElement>("button, input, [tabindex]:not([tabindex='-1'])"))
      if (focusable.length === 0) return
      const current = document.activeElement
      const index = focusable.indexOf(current as HTMLElement)
      const next = event.shiftKey ? (index <= 0 ? focusable.length - 1 : index - 1) : (index === focusable.length - 1 ? 0 : index + 1)
      event.preventDefault()
      focusable[next].focus()
    }
    document.addEventListener("keydown", onKeyDown)
    return () => document.removeEventListener("keydown", onKeyDown)
  }, [removingRelationshipID])

  async function handleRemoveRelated() {
    if (!id || !removingRelationshipID || !removalReason.trim()) {
      setRelatedError(t("caseDetail.relatedCases.removeReasonRequired"))
      return
    }
    setRelatedBusy(true)
    setRelatedError(null)
    try {
	      await api.cases.removeRelated(id, removingRelationshipID, removalReason.trim(), latestCaseUpdatedAt.current ?? caseData?.updated_at)
      setRemovingRelationshipID(null)
      setRemovalReason("")
	      removeTriggerRef.current?.focus()
      await loadRelatedCases()
      await loadCaseFile()
    } catch (err) {
      setRelatedError(translateApiError(err, t))
    } finally {
      setRelatedBusy(false)
    }
  }

  function beginCorrectRelated(relationship: RelatedCase["relationship"]) {
    if (!relationship) return
    setCorrectingRelationshipID(relationship.id)
    setCorrectionType(relationship.relationship_type)
    setCorrectionRationale("")
    setRelatedError(null)
  }

  async function handleCorrectRelated(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !correctingRelationshipID || !correctionRationale.trim()) {
      setRelatedError(t("caseDetail.relatedCases.correctionRequired"))
      return
    }
    setRelatedBusy(true)
    setRelatedError(null)
    try {
      await api.cases.correctRelated(id, correctingRelationshipID, correctionType.trim() || "related", correctionRationale.trim())
      setCorrectingRelationshipID(null)
      setCorrectionRationale("")
      await loadRelatedCases()
      await loadCaseFile()
    } catch (err) {
      setRelatedError(translateApiError(err, t))
    } finally {
      setRelatedBusy(false)
    }
  }

  async function handleAddEvidence(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !evidenceDescription.trim() || !evidenceSource.trim() || !evidenceType.trim() || !evidenceCollector.trim()) {
      setEvidenceError(t("caseDetail.caseFile.evidenceAdd"))
      return
    }
    if (correctingEvidenceID && !evidenceCorrectionReason.trim()) {
      setEvidenceError(t("caseDetail.caseFile.evidenceCorrectionRequired"))
      return
    }
    setEvidenceBusy(true)
    setEvidenceError(null)
    try {
      if (correctingEvidenceID) {
        await api.cases.correctEvidence(id, correctingEvidenceID, { description: evidenceDescription.trim(), source: evidenceSource.trim(), evidence_type: evidenceType.trim(), collected_by: evidenceCollector.trim(), ...(evidenceHash.trim() ? { integrity_hash: evidenceHash.trim() } : {}), reason: evidenceCorrectionReason.trim() })
      } else {
        await api.cases.addEvidence(id, { description: evidenceDescription.trim(), source: evidenceSource.trim(), evidence_type: evidenceType.trim(), collected_by: evidenceCollector.trim(), ...(evidenceHash.trim() ? { integrity_hash: evidenceHash.trim() } : {}) })
      }
      setEvidenceDescription("")
      setEvidenceSource("")
      setEvidenceType("")
      setEvidenceCollector("")
      setEvidenceHash("")
      setCorrectingEvidenceID(null)
      setEvidenceCorrectionReason("")
      await loadCaseFile()
    } catch (err) {
      setEvidenceError(translateApiError(err, t))
    } finally {
      setEvidenceBusy(false)
    }
  }

  async function handleChecklistChange(key: string, label: string, completed: boolean) {
    if (!id) return
    setChecklistBusy(key)
    try {
      await api.cases.updateChecklist(id, key, label, completed)
      await loadCaseFile()
    } catch (err) {
      setEvidenceError(translateApiError(err, t))
    } finally {
      setChecklistBusy(null)
    }
  }

  async function handleAddWorkItem(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !workTitle.trim()) {
      setWorkError(t("caseDetail.caseFile.workTitle"))
      return
    }
    setWorkBusy(true)
    setWorkError(null)
    try {
      await api.cases.addWorkItem(id, { title: workTitle.trim() })
      setWorkTitle("")
      await loadCaseFile()
    } catch (err) {
      setWorkError(translateApiError(err, t))
    } finally {
      setWorkBusy(false)
    }
  }

  async function handleWorkStatusChange(item: CaseFile["work_items"][number], status: string) {
    if (!id || status === item.status) return
    setWorkBusy(true)
    setWorkError(null)
    try {
      await api.cases.updateWorkItem(id, item.id, { title: item.title, description: item.description, status, assigned_to: item.assigned_to, due_at: item.due_at })
      await loadCaseFile()
    } catch (err) {
      setWorkError(translateApiError(err, t))
    } finally {
      setWorkBusy(false)
    }
  }

  async function handleReopen(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !reopenReason.trim()) return
    setReopening(true)
    setReopenError(null)
    setConflictError(null)
    try {
      await api.cases.update(id, {
        status: "reopened",
        reason: reopenReason.trim(),
        expected_updated_at: caseData!.updated_at,
      })
      window.location.reload()
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setConflictError(t("caseDetail.conflict"))
      } else {
        setReopenError(translateApiError(err, t))
      }
      setReopening(false)
    }
  }

  async function handleAddNote(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !noteRef.current?.value.trim()) return
    setAddingNote(true)
    setNoteError(null)
    try {
      await api.cases.addNote(id, "operator", noteRef.current.value.trim())
      window.location.reload()
    } catch (err) {
      setNoteError(translateApiError(err, t))
      setAddingNote(false)
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-64 animate-pulse rounded bg-muted" />
        <div className="h-48 animate-pulse rounded-xl border bg-muted" />
      </div>
    )
  }

  if (error || !caseData) {
    return (
      <div className="space-y-4">
        <Link to="/cases" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> {t("caseDetail.backToList")}
        </Link>
        <p className="text-destructive">{t("caseDetail.error")}</p>
      </div>
    )
  }

  const transitions = statusTransitions[caseData.status] ?? []
  const eddStage = eddStageDisplay(customer)
  const events = Array.isArray(caseFile?.events) ? caseFile.events : []
  const evidence = Array.isArray(caseFile?.evidence) ? caseFile.evidence : []
  const checklist = Array.isArray(caseFile?.checklist) ? caseFile.checklist : []
  const workItems = Array.isArray(caseFile?.work_items) ? caseFile.work_items : []
  const checklistDefaults = [
    { key: "customer_identity", label: "Customer identity reviewed" },
    { key: "transaction_rationale", label: "Transaction rationale documented" },
    { key: "decision_review", label: "Disposition independently reviewed" },
  ]
  const checklistRows = [
    ...checklistDefaults.map((defaultItem) => ({
      ...defaultItem,
      item: checklist.find((candidate) => candidate.key === defaultItem.key),
    })),
    ...checklist
      .filter((item) => !checklistDefaults.some((defaultItem) => defaultItem.key === item.key))
      .map((item) => ({ key: item.key, label: item.label, item })),
  ]

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-4">
        <Link to="/cases" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> {t("caseDetail.back")}
        </Link>
        <h1 className="text-2xl font-bold tracking-tight">{t("caseDetail.title")}</h1>
        <Badge variant={PRIORITY_VARIANT[caseData.priority]}>
          {priorityLabels[caseData.priority]}
        </Badge>
        <Badge variant="outline">{statusLabels[caseData.status]}</Badge>
        {caseData.str_candidate && caseData.status !== "str_filed" && <Badge variant="secondary">{t("caseStatus.str_candidate")}</Badge>}
        {eddStage && <Badge variant={eddStage.variant}>{eddStage.label}</Badge>}
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("caseDetail.info.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            {customerLoadError && <p role="alert" className="mb-3 text-sm text-destructive">{customerLoadError}</p>}
            <dl className="space-y-3 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">ID</dt>
                <dd className="font-mono">{caseData.id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("caseDetail.info.customerId")}</dt>
                <dd>
                  <Link to={`/customers/${caseData.customer_id}`} className="font-mono text-primary underline-offset-4 hover:underline">
                    {caseData.customer_id}
                  </Link>
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("caseDetail.info.assignedTo")}</dt>
                <dd>{caseData.assigned_to || "-"}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("caseDetail.info.assignedTeam")}</dt>
                <dd>{caseData.assigned_team || "-"}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("caseDetail.info.dueAt")}</dt>
                <dd>{caseData.due_at ? formatDateTime(caseData.due_at, i18n.language) : "-"}</dd>
              </div>
              {caseData.investigation_disposition && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">{t("caseDetail.info.disposition")}</dt>
                  <dd>{caseData.investigation_disposition}</dd>
                </div>
              )}
              {caseData.disposition_rationale && (
                <div className="flex justify-between gap-4">
                  <dt className="text-muted-foreground">{t("caseDetail.info.rationale")}</dt>
                  <dd className="text-right">{caseData.disposition_rationale}</dd>
                </div>
              )}
              {caseData.str_report_id && (
                <>
                  <div className="flex justify-between gap-4">
                    <dt className="text-muted-foreground">{t("caseDetail.info.strReport")}</dt>
                    <dd className="font-mono text-right">{caseData.str_report_id}</dd>
                  </div>
                  {caseData.str_filed_at && <div className="flex justify-between gap-4"><dt className="text-muted-foreground">{t("caseDetail.info.strSubmittedAt")}</dt><dd>{formatDateTime(caseData.str_filed_at, i18n.language)}</dd></div>}
                  {caseData.str_filing_channel && <div className="flex justify-between gap-4"><dt className="text-muted-foreground">{t("caseDetail.info.filingChannel")}</dt><dd>{caseData.str_filing_channel}</dd></div>}
                  {caseData.str_destination && <div className="flex justify-between gap-4"><dt className="text-muted-foreground">{t("caseDetail.info.filingDestination")}</dt><dd>{caseData.str_destination}</dd></div>}
                  {caseData.str_external_reference && <div className="flex justify-between gap-4"><dt className="text-muted-foreground">{t("caseDetail.info.externalReference")}</dt><dd>{caseData.str_external_reference}</dd></div>}
                </>
              )}
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("caseDetail.info.createdAt")}</dt>
                <dd>{formatDateTime(caseData.created_at, i18n.language)}</dd>
              </div>
              {caseData.closed_at && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">{t("caseDetail.info.closedAt")}</dt>
                  <dd>{formatDateTime(caseData.closed_at, i18n.language)}</dd>
                </div>
              )}
            </dl>
            <form onSubmit={handleSaveAssignment} className="mt-4 grid gap-2 border-t pt-4 sm:grid-cols-2">
              <input list="case-detail-directory-users" value={assignmentTo} onChange={(e) => setAssignmentTo(e.target.value)} aria-label={t("caseDetail.info.assignedTo")} placeholder={t("caseDetail.info.assignedTo")} className="rounded-md border bg-background px-2 py-2 text-sm" />
              <input list="case-detail-directory-teams" value={assignmentTeam} onChange={(e) => setAssignmentTeam(e.target.value)} aria-label={t("caseDetail.info.assignedTeam")} placeholder={t("caseDetail.info.assignedTeam")} className="rounded-md border bg-background px-2 py-2 text-sm" />
              <select value={assignmentPriority} onChange={(e) => setAssignmentPriority(e.target.value as CasePriority)} aria-label={t("casePriority.high")} className="rounded-md border bg-background px-2 py-2 text-sm">
                <option value="low">{priorityLabels.low}</option><option value="medium">{priorityLabels.medium}</option><option value="high">{priorityLabels.high}</option><option value="critical">{priorityLabels.critical}</option>
              </select>
              <input type="datetime-local" value={assignmentDueAt} onChange={(e) => setAssignmentDueAt(e.target.value)} aria-label={t("caseDetail.info.dueAt")} className="rounded-md border bg-background px-2 py-2 text-sm" />
              <textarea value={summaryDraft} onChange={(e) => setSummaryDraft(e.target.value)} aria-label={t("caseDetail.summary.title")} rows={2} className="rounded-md border bg-background px-2 py-2 text-sm sm:col-span-2" />
              <Button type="submit" size="sm" disabled={assignmentBusy} className="sm:col-span-2">{t("caseDetail.info.saveAssignment")}</Button>
            </form>
            <datalist id="case-detail-directory-users">{(directory?.users ?? []).map((user) => <option key={user.id} value={user.id}>{user.email}</option>)}</datalist>
            <datalist id="case-detail-directory-teams">{(directory?.teams ?? []).map((team) => <option key={team} value={team} />)}</datalist>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("caseDetail.summary.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm">{caseData.summary}</p>
            {caseData.alert_ids.length > 0 && (
              <div className="mt-4">
                <p className="mb-2 text-xs font-medium text-muted-foreground">{t("caseDetail.summary.relatedAlerts")}</p>
                <div className="flex flex-wrap gap-1">
                  {caseData.alert_ids.map((aid) => (
                    <Link key={aid} to={`/alerts/${aid}`}>
                      <Badge variant="secondary" className="font-mono text-xs hover:bg-secondary/80">
                        {aid}
                      </Badge>
                    </Link>
                  ))}
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {transitions.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("caseDetail.transitions.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            {conflictError && (
              <div role="alert" className="mb-3 rounded-md border border-destructive/50 bg-destructive/5 p-3 text-sm text-destructive">
                <p>{conflictError}</p>
                <Button variant="outline" size="sm" className="mt-2" onClick={() => window.location.reload()}>
                  {t("caseDetail.reload")}
                </Button>
              </div>
            )}
            <div className="flex gap-2">
              {transitions.map((transition) => (
                <Button
                  key={transition.value}
                  variant={transition.value === "closed" ? "destructive" : "outline"}
                  size="sm"
                  disabled={updating}
                  onClick={(event) => { if (transition.value === "closed") closeTriggerRef.current = event.currentTarget; void handleStatusChange(transition.value) }}
                >
                  {transition.label}
                </Button>
              ))}
            </div>
            {!caseData.str_candidate && caseData.status !== "str_filed" && (
              <Button className="mt-3" variant="outline" size="sm" disabled={candidateBusy} onClick={handleMarkCandidate}>
                {t("caseDetail.transitions.markCandidate")}
              </Button>
            )}
          </CardContent>
        </Card>
      )}

      {closeConfirmOpen && (
        <Card ref={closeDialogRef} role="dialog" aria-modal="true" aria-labelledby="case-close-title">
          <CardHeader>
            <CardTitle id="case-close-title" className="text-base">{t("caseDetail.transitions.confirmClose")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="mb-3 text-sm text-muted-foreground">{caseData.id} · {t("caseDetail.transitions.confirmCloseText", { outcome: t("caseStatus.closed") })}</p>
            <form onSubmit={handleConfirmClose} className="space-y-3">
              <label className="block text-sm font-medium">
                {t("caseDetail.transitions.closeRationale")}
                <textarea ref={closeRationaleRef} aria-label={t("caseDetail.transitions.closeRationale")} value={closeRationale} onChange={(e) => setCloseRationale(e.target.value)} rows={3} className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm" />
              </label>
              {conflictError && <p role="alert" className="text-sm text-destructive">{conflictError}</p>}
              <div className="flex gap-2">
                <Button type="submit" variant="destructive" disabled={updating}>{t("caseDetail.transitions.confirm")}</Button>
                <Button type="button" variant="ghost" disabled={updating} onClick={() => setCloseConfirmOpen(false)}>{t("caseDetail.relatedCases.cancel")}</Button>
              </div>
            </form>
          </CardContent>
        </Card>
      )}

      {filingOpen && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("caseDetail.filing.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleFileCase} className="space-y-3">
              <label className="block text-sm">
                <span className="mb-1 block text-xs font-medium">{t("caseDetail.filing.report")}</span>
                <input
                  list="submitted-str-reports"
                  value={filingReportID}
                  onChange={(e) => setFilingReportID(e.target.value)}
                  aria-label={t("caseDetail.filing.report")}
                  placeholder={t("caseDetail.filing.reportPlaceholder")}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm"
                />
                <datalist id="submitted-str-reports">
                  {filingReports?.map((report) => <option key={report.id} value={report.id}>{report.suspicious_point}</option>)}
                </datalist>
                {filingReports === null && !filingError && <p className="mt-1 text-xs text-muted-foreground">{t("caseDetail.loading")}</p>}
                {filingReportsTruncated && <p className="mt-1 text-xs text-muted-foreground">{t("caseDetail.filing.more")}</p>}
              </label>
              <div className="grid gap-3 sm:grid-cols-3">
                <input value={filingChannel} onChange={(e) => setFilingChannel(e.target.value)} aria-label={t("caseDetail.filing.channel")} placeholder={t("caseDetail.filing.channel")} className="rounded-md border bg-background px-3 py-2 text-sm" />
                <input value={filingDestination} onChange={(e) => setFilingDestination(e.target.value)} aria-label={t("caseDetail.filing.destination")} placeholder={t("caseDetail.filing.destination")} className="rounded-md border bg-background px-3 py-2 text-sm" />
                <input value={filingExternalReference} onChange={(e) => setFilingExternalReference(e.target.value)} aria-label={t("caseDetail.filing.externalReference")} placeholder={t("caseDetail.filing.externalReference")} className="rounded-md border bg-background px-3 py-2 text-sm" />
              </div>
              <input value={filingRationale} onChange={(e) => setFilingRationale(e.target.value)} aria-label={t("caseDetail.info.rationale")} className="w-full rounded-md border bg-background px-3 py-2 text-sm" />
              {filingError && <div role="alert" className="space-y-2 text-sm text-destructive"><p>{filingError}</p><Button type="button" variant="outline" size="sm" onClick={() => void openFilingForm()}>{t("caseDetail.retry")}</Button></div>}
              <div className="flex gap-2">
                <Button type="submit" size="sm" disabled={filingBusy}>{t("caseDetail.filing.submit")}</Button>
                <Button type="button" variant="outline" size="sm" onClick={() => setFilingOpen(false)}>{t("caseDetail.filing.cancel")}</Button>
              </div>
            </form>
          </CardContent>
        </Card>
      )}

      {caseData.status === "closed" && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("caseDetail.reopen.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleReopen} className="flex gap-2">
              <input
                type="text"
                value={reopenReason}
                onChange={(e) => setReopenReason(e.target.value)}
                aria-label={t("caseDetail.reopen.placeholder")}
                placeholder={t("caseDetail.reopen.placeholder")}
                className="flex-1 rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
              <Button type="submit" size="sm" disabled={reopening || !reopenReason.trim()}>
                {t("caseDetail.reopen.submit")}
              </Button>
            </form>
            {reopenError && <p className="mt-2 text-sm text-destructive">{reopenError}</p>}
          </CardContent>
        </Card>
      )}

      {caseData.reopen_reason && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("caseDetail.reopenReason.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm">{caseData.reopen_reason}</p>
          </CardContent>
        </Card>
      )}

      <Card>
		<CardHeader>
		  <div className="flex items-center justify-between gap-3">
		    <CardTitle className="text-base">{t("caseDetail.relatedCases.title")}</CardTitle>
            <Button type="button" variant="outline" size="sm" onClick={() => setRelatedFormOpen((open) => !open)}>
              {t("caseDetail.relatedCases.add")}
            </Button>
		  </div>
		  <p className="mt-2 text-xs text-muted-foreground">{t("caseDetail.relatedCases.directedNote")}</p>
        </CardHeader>
        <CardContent>
          {relatedLoadError && <div role="alert" className="mb-3 text-sm text-destructive"><p>{relatedLoadError}</p><Button type="button" variant="outline" size="sm" className="mt-2" onClick={() => void loadRelatedCases()}>{t("caseDetail.retry")}</Button></div>}
          {relatedFormOpen && (
            <form onSubmit={handleAddRelated} className="mb-4 space-y-3 rounded-lg border bg-muted/30 p-3">
              <div className="flex flex-wrap gap-2">
				<input aria-label={t("caseDetail.relatedCases.search")} value={relatedSearch} onChange={(e) => setRelatedSearch(e.target.value)} placeholder={t("caseDetail.relatedCases.search")} className="rounded-md border bg-background px-3 py-2 text-sm" />
                <Button type="button" variant="outline" size="sm" disabled={relatedBusy} onClick={findRelatedCandidates}>{relatedBusy ? "…" : t("cases.queue.search")}</Button>
              </div>
			  <input aria-label={t("caseDetail.relatedCases.caseId")} list="related-case-candidates" value={relatedCaseID} onChange={(e) => setRelatedCaseID(e.target.value)} placeholder={t("caseDetail.relatedCases.caseId")} className="w-full rounded-md border bg-background px-3 py-2 text-sm" />
              <datalist id="related-case-candidates">
				{relatedCandidates.map((candidate) => <option key={candidate.id} value={candidate.id}>{candidate.summary} · {statusLabels[candidate.status] ?? candidate.status}</option>)}
              </datalist>
              {relatedCandidates.length > 0 && (
                <ul aria-label={t("caseDetail.relatedCases.results")} className="max-h-48 space-y-1 overflow-y-auto rounded-md border p-1">
                  {relatedCandidates.map((candidate) => (
                    <li key={candidate.id}>
                      <button type="button" onClick={() => setRelatedCaseID(candidate.id)} className={`flex w-full items-start justify-between rounded px-2 py-2 text-left text-sm ${relatedCaseID === candidate.id ? "bg-primary/10" : "hover:bg-accent"}`}>
                        <span><span className="block font-mono text-xs">{candidate.id}</span><span className="block text-muted-foreground">{candidate.summary}</span></span>
                        <Badge variant="outline">{statusLabels[candidate.status] ?? candidate.status}</Badge>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              {relatedCandidatesTruncated && <p className="text-xs text-muted-foreground">{t("caseDetail.relatedCases.more")}</p>}
              <div className="grid gap-2 sm:grid-cols-2">
				<input aria-label={t("caseDetail.relatedCases.type")} value={relatedType} onChange={(e) => setRelatedType(e.target.value)} placeholder={t("caseDetail.relatedCases.type")} className="rounded-md border bg-background px-3 py-2 text-sm" />
				<input aria-label={t("caseDetail.relatedCases.rationale")} required value={relatedRationale} onChange={(e) => setRelatedRationale(e.target.value)} placeholder={t("caseDetail.relatedCases.rationale")} className="rounded-md border bg-background px-3 py-2 text-sm" />
              </div>
              {relatedError && <p role="alert" className="text-sm text-destructive">{relatedError}</p>}
              <div className="flex gap-2">
                <Button type="submit" size="sm" disabled={relatedBusy}>{t("caseDetail.relatedCases.submit")}</Button>
                <Button type="button" variant="outline" size="sm" onClick={() => setRelatedFormOpen(false)}>{t("caseDetail.relatedCases.cancel")}</Button>
              </div>
            </form>
          )}
          {relatedCases === null && !relatedLoadError ? (
            <p className="text-sm text-muted-foreground">{t("caseDetail.loading")}</p>
          ) : relatedLoadError ? null : relatedCases && relatedCases.length > 0 ? (
            <div className="space-y-2">
              {relatedCases.map((rc) => (
                <div key={rc.case.id} className="flex items-center justify-between rounded-lg bg-muted/50 p-3 text-sm">
                  <Link to={`/cases/${rc.case.id}`} className="font-mono text-primary hover:underline">
                    {rc.case.id}
                  </Link>
                  <div className="flex items-center gap-2">
                    <Badge variant="outline">{statusLabels[rc.case.status]}</Badge>
                    {rc.case.str_candidate && rc.case.status !== "str_filed" && <Badge variant="secondary">{t("caseStatus.str_candidate")}</Badge>}
                    <Badge variant="secondary" className="text-xs">
                      {rc.link_type === "auto" ? t("caseDetail.relatedCases.auto") : t("caseDetail.relatedCases.manual")}
                    </Badge>
                    {rc.relationship && (
                      <>
                        <span className="text-xs text-muted-foreground">
                          {rc.relationship.relationship_type}: {rc.relationship.rationale} · {t("caseDetail.relatedCases.createdByAt", {
                            actor: rc.relationship.created_by || "-",
                            at: formatDateTime(rc.relationship.created_at, i18n.language),
                          })}
                        </span>
                        {rc.relationship.active && <>
                          <Button type="button" variant="ghost" size="sm" disabled={relatedBusy} onClick={(event) => { removeTriggerRef.current = event.currentTarget; setRemovingRelationshipID(rc.relationship!.id); setRemovalReason(""); setRelatedError(null) }}>{t("caseDetail.relatedCases.remove")}</Button>
                          <Button type="button" variant="ghost" size="sm" disabled={relatedBusy} onClick={() => beginCorrectRelated(rc.relationship)}>{t("caseDetail.relatedCases.correct")}</Button>
                        </>}
                      </>
                    )}
                    {rc.relationship?.active && correctingRelationshipID === rc.relationship.id && (
                      <form onSubmit={handleCorrectRelated} className="basis-full space-y-2 rounded-md border p-2">
                        <input value={correctionType} onChange={(e) => setCorrectionType(e.target.value)} aria-label={t("caseDetail.relatedCases.type")} className="rounded-md border bg-background px-2 py-1 text-sm" />
                        <input required value={correctionRationale} onChange={(e) => setCorrectionRationale(e.target.value)} aria-label={t("caseDetail.relatedCases.rationale")} placeholder={t("caseDetail.relatedCases.correctionReason")} className="rounded-md border bg-background px-2 py-1 text-sm" />
                        <Button type="submit" size="sm" disabled={relatedBusy}>{t("caseDetail.relatedCases.saveCorrection")}</Button>
                        <Button type="button" variant="outline" size="sm" onClick={() => setCorrectingRelationshipID(null)}>{t("caseDetail.relatedCases.cancel")}</Button>
                      </form>
                    )}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">{t("caseDetail.relatedCases.empty")}</p>
          )}
          {removingRelationshipID && (
            <div ref={removeDialogRef} role="dialog" aria-modal="true" aria-labelledby="remove-related-title" className="mt-4 rounded-lg border bg-background p-4 shadow-lg">
              <h3 id="remove-related-title" className="font-medium">{t("caseDetail.relatedCases.removeTitle")}</h3>
              <p className="mt-1 text-sm text-muted-foreground">{caseData.id} · {removingRelationshipID}</p>
              <label className="mt-3 block text-sm font-medium">
                {t("caseDetail.relatedCases.removeReason")}
                <input ref={removeReasonRef} value={removalReason} onChange={(event) => setRemovalReason(event.target.value)} className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm" />
              </label>
              {relatedError && <p role="alert" className="mt-2 text-sm text-destructive">{relatedError}</p>}
              <div className="mt-3 flex gap-2">
                <Button type="button" variant="destructive" disabled={relatedBusy} onClick={() => void handleRemoveRelated()}>{t("caseDetail.relatedCases.removeConfirm")}</Button>
                <Button type="button" variant="outline" disabled={relatedBusy} onClick={() => { setRemovingRelationshipID(null); setRemovalReason(""); removeTriggerRef.current?.focus() }}>{t("caseDetail.relatedCases.cancel")}</Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("caseDetail.notes.title")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {caseData.notes && caseData.notes.length > 0 ? (
            <div className="space-y-3">
              {caseData.notes.map((note) => (
                <div key={note.id} className="rounded-lg bg-muted/50 p-3">
                  <div className="mb-1 flex items-center gap-2 text-xs text-muted-foreground">
                    <span className="font-medium">{note.author}</span>
                    <span>{formatDateTime(note.created_at, i18n.language)}</span>
                  </div>
                  <p className="text-sm">{note.content}</p>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">{t("caseDetail.notes.empty")}</p>
          )}

          {caseData.status !== "closed" && (
            <form onSubmit={handleAddNote} className="flex gap-2">
              <textarea
                ref={noteRef}
                aria-label={t("caseDetail.notes.placeholder")}
                placeholder={t("caseDetail.notes.placeholder")}
                className="flex-1 resize-none rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                rows={2}
              />
              <Button type="submit" size="sm" disabled={addingNote}>
                <Send className="h-4 w-4" />
              </Button>
            </form>
          )}
          {noteError && <p role="alert" className="mt-2 text-sm text-destructive">{noteError}</p>}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-3">
            <CardTitle className="text-base">{t("caseDetail.caseFile.title")}</CardTitle>
            <Button type="button" variant="outline" size="sm" onClick={handleExportCaseFile} disabled={caseFileExportBusy} aria-busy={caseFileExportBusy}>
              {t("caseDetail.caseFile.export")}
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-6">
          {caseFileExportError && <p role="alert" className="text-sm text-destructive">{caseFileExportError}</p>}
          {caseFileLoadError && <div role="alert" className="text-sm text-destructive"><p>{caseFileLoadError}</p><Button type="button" variant="outline" size="sm" className="mt-2" onClick={() => void loadCaseFile()}>{t("caseDetail.retry")}</Button></div>}
          {!caseFileLoadError && !caseFile && <p className="text-sm text-muted-foreground">{t("caseDetail.loading")}</p>}
          {!caseFileLoadError && caseFile && <>
          <section>
            <h3 className="mb-2 text-sm font-semibold">{t("caseDetail.caseFile.timeline")}</h3>
            {events.length > 0 ? (
              <ol className="space-y-2 border-l pl-4">
                {events.map((event) => (
                  <li key={event.id} className="text-sm">
                    <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
                      <span>{formatDateTime(event.created_at, i18n.language)}</span>
                      <span>{event.actor || "-"}</span>
                      <span className="font-medium">{event.event_type}</span>
                    </div>
                    {event.reason && <p className="mt-1">{event.reason}</p>}
                    {(event.before || event.after) && <details className="mt-1 text-xs text-muted-foreground"><summary>{t("caseDetail.caseFile.changeDetails")}</summary><pre className="mt-1 max-w-full overflow-auto whitespace-pre-wrap">{JSON.stringify({ before: event.before, after: event.after }, null, 2)}</pre></details>}
                    {(event.related_alert_ids?.length || event.related_case_ids?.length || event.related_report_ids?.length) ? <p className="mt-1 text-xs text-muted-foreground">{[...(event.related_alert_ids ?? []).map((ref) => `alert:${ref}`), ...(event.related_case_ids ?? []).map((ref) => `case:${ref}`), ...(event.related_report_ids ?? []).map((ref) => `report:${ref}`)].join(" · ")}</p> : null}
                  </li>
                ))}
              </ol>
            ) : <p className="text-sm text-muted-foreground">{t("caseDetail.caseFile.timelineEmpty")}</p>}
          </section>

          <section>
            <h3 className="mb-2 text-sm font-semibold">{t("caseDetail.caseFile.evidence")}</h3>
            {evidence.length > 0 ? (
              <div className="space-y-2">
                {evidence.map((item) => <div key={item.id} className="rounded-md bg-muted/50 p-3 text-sm"><div className="flex items-start justify-between gap-2"><p>{item.description}</p><Button type="button" variant="ghost" size="sm" onClick={() => { setCorrectingEvidenceID(item.id); setEvidenceDescription(item.description); setEvidenceSource(item.source); setEvidenceType(item.evidence_type); setEvidenceCollector(item.collected_by); setEvidenceHash(item.integrity_hash ?? ""); setEvidenceCorrectionReason(""); setEvidenceError(null) }}>{t("caseDetail.caseFile.evidenceCorrect")}</Button></div><p className="mt-1 text-xs text-muted-foreground">{item.source} · {item.evidence_type} · {item.collected_by} · v{item.version}{item.integrity_hash ? ` · ${item.integrity_hash}` : ""}</p></div>)}
              </div>
            ) : <p className="text-sm text-muted-foreground">{t("caseDetail.caseFile.evidenceEmpty")}</p>}
            <form onSubmit={handleAddEvidence} className="mt-3 grid gap-2 sm:grid-cols-5">
              <input required aria-label={t("caseDetail.caseFile.evidenceDescription")} value={evidenceDescription} onChange={(e) => setEvidenceDescription(e.target.value)} placeholder={t("caseDetail.caseFile.evidenceDescription")} className="rounded-md border bg-background px-2 py-2 text-sm" />
              <input required aria-label={t("caseDetail.caseFile.evidenceSource")} value={evidenceSource} onChange={(e) => setEvidenceSource(e.target.value)} placeholder={t("caseDetail.caseFile.evidenceSource")} className="rounded-md border bg-background px-2 py-2 text-sm" />
              <input required aria-label={t("caseDetail.caseFile.evidenceType")} value={evidenceType} onChange={(e) => setEvidenceType(e.target.value)} placeholder={t("caseDetail.caseFile.evidenceType")} className="rounded-md border bg-background px-2 py-2 text-sm" />
              <input aria-label={t("caseDetail.caseFile.evidenceHash")} value={evidenceHash} onChange={(e) => setEvidenceHash(e.target.value)} placeholder={t("caseDetail.caseFile.evidenceHash")} className="rounded-md border bg-background px-2 py-2 text-sm" />
              <div className="flex gap-2"><input required aria-label={t("caseDetail.caseFile.evidenceCollector")} value={evidenceCollector} onChange={(e) => setEvidenceCollector(e.target.value)} placeholder={t("caseDetail.caseFile.evidenceCollector")} className="min-w-0 flex-1 rounded-md border bg-background px-2 py-2 text-sm" /><Button type="submit" size="sm" disabled={evidenceBusy}>{correctingEvidenceID ? t("caseDetail.caseFile.evidenceCorrect") : t("caseDetail.caseFile.evidenceAdd")}</Button>{correctingEvidenceID && <Button type="button" variant="outline" size="sm" onClick={() => { setCorrectingEvidenceID(null); setEvidenceCorrectionReason(""); setEvidenceDescription(""); setEvidenceSource(""); setEvidenceType(""); setEvidenceCollector(""); setEvidenceHash("") }}>{t("caseDetail.caseFile.evidenceCorrectionCancel")}</Button>}</div>
              {correctingEvidenceID && <input value={evidenceCorrectionReason} onChange={(e) => setEvidenceCorrectionReason(e.target.value)} aria-required="true" aria-label={t("caseDetail.caseFile.evidenceCorrectionReason")} placeholder={t("caseDetail.caseFile.evidenceCorrectionReason")} className="rounded-md border bg-background px-2 py-2 text-sm sm:col-span-5" />}
            </form>
            {evidenceError && <p role="alert" className="mt-2 text-sm text-destructive">{evidenceError}</p>}
          </section>

          <section>
            <h3 className="mb-2 text-sm font-semibold">{t("caseDetail.caseFile.checklist")}</h3>
            <div className="space-y-2">
              {checklistRows.map((checklistRow) => {
                const item = checklistRow.item
                return <label key={checklistRow.key} className="flex items-center gap-2 text-sm"><input type="checkbox" checked={item?.completed ?? false} disabled={checklistBusy === checklistRow.key} onChange={(e) => handleChecklistChange(checklistRow.key, checklistRow.label, e.target.checked)} />{item?.label ?? checklistRow.label}</label>
              })}
            </div>
            {checklist.length === 0 && <p className="mt-2 text-xs text-muted-foreground">{t("caseDetail.caseFile.checklistEmpty")}</p>}
          </section>

          <section>
            <h3 className="mb-2 text-sm font-semibold">{t("caseDetail.caseFile.workItems")}</h3>
            {workItems.length > 0 ? <div className="space-y-2">{workItems.map((item) => <div key={item.id} className="flex items-center justify-between gap-3 rounded-md bg-muted/50 p-3 text-sm"><span>{item.title}</span><select aria-label={`${item.title} ${t("caseDetail.caseFile.workStatus")}`} value={item.status} disabled={workBusy} onChange={(e) => void handleWorkStatusChange(item, e.target.value)} className="rounded-md border bg-background px-2 py-1"><option value="open">open</option><option value="in_progress">in_progress</option><option value="completed">completed</option><option value="cancelled">cancelled</option></select></div>)}</div> : <p className="text-sm text-muted-foreground">{t("caseDetail.caseFile.workItemsEmpty")}</p>}
            <form onSubmit={handleAddWorkItem} className="mt-3 flex gap-2"><input required aria-label={t("caseDetail.caseFile.workTitle")} value={workTitle} onChange={(e) => setWorkTitle(e.target.value)} placeholder={t("caseDetail.caseFile.workTitle")} className="flex-1 rounded-md border bg-background px-3 py-2 text-sm" /><Button type="submit" size="sm" disabled={workBusy}>{t("caseDetail.caseFile.workAdd")}</Button></form>
            {workError && <p role="alert" className="mt-2 text-sm text-destructive">{workError}</p>}
          </section>
          </>}
        </CardContent>
      </Card>
    </div>
  )
}
