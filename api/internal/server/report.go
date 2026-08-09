package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
)

type CreateSTRRequest struct {
	AlertID            string `json:"alert_id"`
	CaseID             string `json:"case_id"`
	SuspiciousPoint    string `json:"suspicious_point"`
	CreatedBy          string `json:"created_by"`
	CorrectsReportID   string `json:"corrects_report_id,omitempty"`
	SupersedesReportID string `json:"supersedes_report_id,omitempty"`
}

type UpdateSTRRequest struct {
	SuspiciousPoint   string     `json:"suspicious_point"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

type SubmitSTRRequest struct {
	SubmittedBy        string `json:"submitted_by"`
	SubmissionEvidence string `json:"submission_evidence"`
	// FilingReference is accepted as an additive alias for deployments whose
	// configured filing process calls the evidence a filing reference.
	FilingReference string `json:"filing_reference"`
}

func strReportCursor(report domain.STRReport) Cursor {
	return Cursor{CreatedAt: report.CreatedAt, ID: report.ID}
}

func (s *Server) handleCreateSTR(w http.ResponseWriter, r *http.Request) {
	if s.reports == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "STR report storage is not configured")
		return
	}
	if s.alerts == nil || s.transactions == nil || s.customers == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "STR source storage is not configured")
		return
	}

	var req CreateSTRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}

	if strings.TrimSpace(req.AlertID) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "alert_id required")
		return
	}
	if strings.TrimSpace(req.SuspiciousPoint) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "suspicious_point required")
		return
	}

	alert, err := s.alerts.Get(r.Context(), req.AlertID)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	transactionSnapshot, totalAmount, currency, err := s.buildSTRTransactionSnapshot(r.Context(), alert.TransactionIDs)
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, apierr.CodeValidationFailed, err.Error())
		return
	}

	caseID, err := s.findSTRCase(r.Context(), alert, req.CaseID)
	if err != nil {
		var validation *caseWorkflowValidationError
		var conflict *domain.ErrConflict
		switch {
		case errors.As(err, &validation):
			writeErrorCode(w, http.StatusUnprocessableEntity, apierr.CodeValidationFailed, validation.Error())
		case errors.As(err, &conflict):
			writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, err.Error())
		default:
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "source case lookup failed")
		}
		return
	}
	customer, err := s.customers.Get(r.Context(), alert.CustomerID)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusUnprocessableEntity, apierr.CodeValidationFailed, "STR source customer is unavailable")
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "source customer lookup failed")
		return
	}

	now := time.Now().UTC()
	report := domain.STRReport{
		ID:                  generateID(),
		AlertID:             alert.ID,
		CustomerID:          alert.CustomerID,
		CaseID:              caseID,
		ReportType:          domain.ReportTypeSTR,
		Status:              domain.ReportStatusDraft,
		SuspiciousPoint:     strings.TrimSpace(req.SuspiciousPoint),
		TransactionIDs:      append([]string(nil), alert.TransactionIDs...),
		TransactionSnapshot: transactionSnapshot,
		TotalAmount:         totalAmount,
		Currency:            currency,
		CreatedAt:           now,
		UpdatedAt:           now,
		CreatedBy:           strings.TrimSpace(req.CreatedBy),
		CorrectsReportID:    domain.CanonicalIdentifier(req.CorrectsReportID),
		SupersedesReportID:  domain.CanonicalIdentifier(req.SupersedesReportID),
		AlertSnapshot: domain.STRAlertSnapshot{
			ID: alert.ID, CustomerID: alert.CustomerID, ScenarioID: alert.ScenarioID,
			Severity: alert.Severity, Status: alert.Status, Score: alert.Score,
			Description: alert.Description, TransactionIDs: append([]string(nil), alert.TransactionIDs...), DetectedAt: alert.DetectedAt,
		},
		CustomerSnapshot: domain.STRCustomerSnapshot{
			ID: customer.ID, ExternalID: customer.ExternalID, CustomerType: customer.CustomerType, CountryCode: customer.CountryCode,
		},
	}
	if report.CreatedBy == "" {
		report.CreatedBy = resolveAuditUserID(r)
	}
	if report.CorrectsReportID != "" && report.SupersedesReportID == "" {
		report.SupersedesReportID = report.CorrectsReportID
	}
	if report.SupersedesReportID != "" {
		original, getErr := s.reports.Get(r.Context(), report.SupersedesReportID)
		if getErr != nil {
			writeReportRepositoryError(w, getErr)
			return
		}
		if !domain.SameIdentifier(original.CaseID, report.CaseID) || !domain.SameIdentifier(original.CustomerID, report.CustomerID) || !domain.SameIdentifier(original.AlertID, report.AlertID) {
			writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, "STR correction must preserve the original case, customer, and alert")
			return
		}
		if original.Status != domain.ReportStatusSubmitted {
			writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, "STR correction must reference a submitted original report")
			return
		}
		if report.CorrectsReportID == "" {
			report.CorrectsReportID = original.ID
		}
	}

	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Reports == nil || repos.Investigation == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if err := repos.Reports.Create(r.Context(), &report); err != nil {
			return err
		}
		if err := appendRequiredSTRReportHistory(r.Context(), r, repos, &report, "created", "STR draft created", nil, strReportEventState(&report)); err != nil {
			return err
		}
		if err := appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{
			CaseID: report.CaseID, EventType: "str_report_created", Reason: "STR draft created",
			After: strReportEventState(&report), RelatedAlertIDs: nonEmptyIDs(report.AlertID), RelatedReportIDs: nonEmptyIDs(report.ID),
		}); err != nil {
			return err
		}
		return appendRequiredReportAudit(r.Context(), r, repos, "create_str", &report, map[string]string{
			"status": string(report.Status), "alert_id": report.AlertID, "customer_id": report.CustomerID,
		})
	}); err != nil {
		writeAtomicMutationError(w, err)
		return
	}
	setAuditDetail(r, "report_id", report.ID)

	s.dispatchWebhook(r.Context(), domain.WebhookEventSTRCreated, report)

	writeJSON(w, http.StatusCreated, report)
}

func (s *Server) buildSTRTransactionSnapshot(ctx context.Context, transactionIDs []string) ([]domain.STRTransactionSnapshot, float64, string, error) {
	if s.transactions == nil {
		return nil, 0, "", errors.New("STR transaction storage is not configured")
	}
	var (
		snapshot    []domain.STRTransactionSnapshot
		currency    string
		totalAmount float64
	)
	for _, txnID := range transactionIDs {
		txn, err := s.transactions.Get(ctx, txnID)
		if err != nil {
			return nil, 0, "", fmt.Errorf("STR transaction %s is unavailable", txnID)
		}
		if currency == "" {
			currency = txn.Currency
		} else if txn.Currency != currency {
			return nil, 0, "", errors.New("STR requires transactions in one currency")
		}
		totalAmount += txn.Amount
		snapshot = append(snapshot, domain.STRTransactionSnapshot{
			ID:                  txn.ID,
			ExternalID:          txn.ExternalID,
			Amount:              txn.Amount,
			Currency:            txn.Currency,
			Direction:           txn.Direction,
			CounterpartyID:      txn.CounterpartyID,
			CounterpartyCountry: txn.CounterpartyCountry,
			Channel:             txn.Channel,
			AccountID:           cloneStringPointer(txn.AccountID),
			Counterparty:        txn.Counterparty,
			Metadata:            txn.Metadata,
			ExecutedAt:          txn.ExecutedAt,
			CreatedAt:           txn.CreatedAt,
		})
	}
	if currency == "" {
		return nil, 0, "", errors.New("STR requires at least one available transaction")
	}
	return snapshot, totalAmount, currency, nil
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (s *Server) findSTRCase(ctx context.Context, alert *domain.Alert, requestedID string) (string, error) {
	if s.cases == nil {
		return "", &caseWorkflowValidationError{reason: "case_id is required for an STR report"}
	}
	cases, err := s.cases.ListByCustomer(ctx, alert.CustomerID)
	if err != nil {
		return "", err
	}
	requestedID = domain.CanonicalIdentifier(requestedID)
	if requestedID == "" {
		return "", &caseWorkflowValidationError{reason: "case_id is required for an STR report"}
	}
	var candidates []domain.Case
	for _, kase := range cases {
		if !domain.SameIdentifier(kase.CustomerID, alert.CustomerID) {
			continue
		}
		linked := false
		for _, alertID := range kase.AlertIDs {
			if domain.SameIdentifier(alertID, alert.ID) {
				linked = true
				break
			}
		}
		if linked && kase.STRCandidate && domain.IsCaseUnresolved(kase.Status) {
			if requestedID == "" || requestedID == domain.CanonicalIdentifier(kase.ID) {
				candidates = append(candidates, kase)
			}
		}
	}
	for _, candidate := range candidates {
		if domain.CanonicalIdentifier(candidate.ID) == requestedID {
			return domain.CanonicalIdentifier(candidate.ID), nil
		}
	}
	return "", &caseWorkflowValidationError{reason: "case_id is not an active STR candidate linked to the alert and customer"}
}

func (s *Server) handleListSTR(w http.ResponseWriter, r *http.Request) {
	if s.reports == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "STR report storage is not configured")
		return
	}
	status, err := parseSTRReportStatus(r.URL.Query().Get("status"))
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	if useCursorPagination(r) {
		pageReq, err := ParsePageRequest(r)
		if err != nil {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
			return
		}
		reports, err := s.reports.List(r.Context(), domain.ReportListFilter{
			Status: status, CustomerID: r.URL.Query().Get("customer_id"), AlertID: r.URL.Query().Get("alert_id"),
			Cursor: toDomainCursor(pageReq.Cursor), Limit: pageReq.Limit + 1,
		})
		if err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		page, meta := BuildPaginationMeta(reports, pageReq.Limit, strReportCursor)
		writePaginatedJSON(w, http.StatusOK, page, meta)
		return
	}

	offsetParam := r.URL.Query().Get("offset")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(offsetParam)
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	reports, err := s.reports.List(r.Context(), domain.ReportListFilter{
		Status: status, CustomerID: r.URL.Query().Get("customer_id"), AlertID: r.URL.Query().Get("alert_id"),
		Limit: limit + 1, Offset: offset,
	})
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if offsetParam != "" {
		setOffsetDeprecationHeaders(w)
	}
	page, meta := BuildPaginationMeta(reports, limit, strReportCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

func parseSTRReportStatus(raw string) (domain.ReportStatus, error) {
	if raw == "" {
		return "", nil
	}
	status := domain.ReportStatus(raw)
	if status != domain.ReportStatusDraft && status != domain.ReportStatusSubmitted {
		return "", errors.New("status must be draft or submitted")
	}
	return status, nil
}

func (s *Server) handleGetSTR(w http.ResponseWriter, r *http.Request) {
	if s.reports == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "STR report storage is not configured")
		return
	}
	report, err := s.reports.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeReportRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleUpdateSTR(w http.ResponseWriter, r *http.Request) {
	if s.reports == nil || s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "STR report storage is not configured")
		return
	}
	var req UpdateSTRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.SuspiciousPoint) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "suspicious_point required")
		return
	}
	report, err := s.reports.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeReportRepositoryError(w, err)
		return
	}
	before := *report
	report.SuspiciousPoint = strings.TrimSpace(req.SuspiciousPoint)
	var updated domain.STRReport
	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Reports == nil || repos.Investigation == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		var err error
		if req.ExpectedUpdatedAt != nil {
			lockRepo, ok := repos.Reports.(domain.ReportOptimisticLockRepository)
			if !ok {
				return errAtomicMutationUnavailable
			}
			err = lockRepo.UpdateIfUnmodified(r.Context(), report, req.ExpectedUpdatedAt.UTC())
		} else {
			err = repos.Reports.Update(r.Context(), report)
		}
		if err != nil {
			return err
		}
		updated = *report
		if err := appendRequiredSTRReportHistory(r.Context(), r, repos, &updated, "updated", "STR draft updated", strReportEventState(&before), strReportEventState(&updated)); err != nil {
			return err
		}
		if err := appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{
			CaseID: updated.CaseID, EventType: "str_report_updated", Reason: "STR draft updated",
			Before: strReportEventState(&before), After: strReportEventState(&updated),
			RelatedAlertIDs: nonEmptyIDs(updated.AlertID), RelatedReportIDs: nonEmptyIDs(updated.ID),
		}); err != nil {
			return err
		}
		return appendRequiredReportAudit(r.Context(), r, repos, "update_str", &updated, map[string]string{"status": string(updated.Status)})
	}); err != nil {
		writeAtomicMutationError(w, err)
		return
	}
	setAuditDetail(r, "report_id", updated.ID)
	writeJSON(w, http.StatusOK, &updated)
}

func (s *Server) handleSubmitSTR(w http.ResponseWriter, r *http.Request) {
	if s.reports == nil || s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "STR report storage is not configured")
		return
	}
	var req SubmitSTRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	evidence := strings.TrimSpace(req.SubmissionEvidence)
	if evidence == "" {
		evidence = strings.TrimSpace(req.FilingReference)
	}
	if evidence == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "submission_evidence required")
		return
	}
	submittedBy := strings.TrimSpace(req.SubmittedBy)
	if submittedBy == "" {
		submittedBy = resolveAuditUserID(r)
	}
	var report *domain.STRReport
	var before *domain.STRReport
	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Reports == nil || repos.Investigation == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		current, err := repos.Reports.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		beforeCopy := *current
		before = &beforeCopy
		updated, err := repos.Reports.Submit(r.Context(), r.PathValue("id"), submittedBy, evidence)
		if err != nil {
			return err
		}
		report = updated
		if before.Status == domain.ReportStatusSubmitted && before.SubmissionEvidence == evidence {
			return nil
		}
		if err := appendRequiredSTRReportHistory(r.Context(), r, repos, report, "submitted", "STR report submitted", strReportEventState(before), strReportEventState(report)); err != nil {
			return err
		}
		if err := appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{
			CaseID: report.CaseID, EventType: "str_report_submitted", Reason: "STR report submitted",
			Before: strReportEventState(before), After: strReportEventState(report),
			RelatedAlertIDs: nonEmptyIDs(report.AlertID), RelatedReportIDs: nonEmptyIDs(report.ID),
		}); err != nil {
			return err
		}
		return appendRequiredReportAudit(r.Context(), r, repos, "submit_str", report, map[string]string{
			"status": string(report.Status), "submitted_by": report.SubmittedBy, "submission_evidence": report.SubmissionEvidence,
		})
	}); err != nil {
		writeAtomicMutationError(w, err)
		return
	}
	setAuditDetail(r, "report_id", report.ID)
	setAuditDetail(r, "submission_evidence", report.SubmissionEvidence)
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) recordSTRLifecycleAudit(r *http.Request, action, reportID string, details map[string]string) error {
	if s.audit == nil {
		return errors.New("audit storage is not configured")
	}
	return s.audit.Create(r.Context(), &domain.AuditEntry{
		UserID:       resolveAuditUserID(r),
		Action:       action,
		ResourceType: "reports",
		ResourceID:   reportID,
		Details:      details,
		IPAddress:    extractIP(r),
		UserAgent:    r.UserAgent(),
		CreatedAt:    time.Now().UTC(),
	})
}

func strReportEventState(report *domain.STRReport) map[string]any {
	if report == nil {
		return nil
	}
	return map[string]any{
		"report_id": report.ID, "status": report.Status, "alert_id": report.AlertID,
		"customer_id": report.CustomerID, "case_id": report.CaseID,
		"suspicious_point": report.SuspiciousPoint, "submitted_at": report.SubmittedAt,
		"submitted_by": report.SubmittedBy, "submission_evidence": report.SubmissionEvidence,
	}
}

func writeReportRepositoryError(w http.ResponseWriter, err error) {
	var notFound *domain.ErrNotFound
	var conflict *domain.ErrConflict
	var invalidTransition *domain.ErrInvalidStateTransition
	switch {
	case errors.As(err, &notFound):
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
	case errors.As(err, &conflict):
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, err.Error())
	case errors.As(err, &invalidTransition):
		writeErrorCode(w, http.StatusConflict, apierr.CodeInvalidStateTransition, err.Error())
	default:
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
	}
}

func (s *Server) handleExportSTR(w http.ResponseWriter, r *http.Request) {
	if reportID := strings.TrimSpace(r.URL.Query().Get("report_id")); reportID != "" {
		if s.reports == nil {
			writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "STR report storage is not configured")
			return
		}
		report, err := s.reports.Get(r.Context(), reportID)
		if err != nil {
			writeReportRepositoryError(w, err)
			return
		}
		if err := validateSTRReportSnapshot(report); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, apierr.CodeValidationFailed, err.Error())
			return
		}
		if len(report.TransactionSnapshot) == 0 {
			writeErrorCode(w, http.StatusUnprocessableEntity, apierr.CodeValidationFailed, "STR transaction snapshot is unavailable")
			return
		}
		setAuditDetail(r, "report_id", report.ID)
		setAuditDetail(r, "export_version", "str-v1")
		if err := s.recordSTRLifecycleAudit(r, "export_str", report.ID, map[string]string{
			"report_id":      report.ID,
			"export_version": "str-v1",
			"format":         exportFormat(r),
			"status":         string(report.Status),
		}); err != nil {
			writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, err.Error())
			return
		}
		if exportFormat(r) == "csv" {
			// Durable report exports are generated from the pinned snapshot. Live
			// alert/customer rows may be edited or retained separately without
			// changing the reviewed report.
			s.exportSTRReportCSV(w, report, nil, nil)
			return
		}
		if exportFormat(r) == "json" {
			s.exportSTRReportJSON(w, report, nil, nil)
			return
		}
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "unsupported format: "+exportFormat(r))
		return
	}

	// alert_id is retained for the contract-stability window. New callers must
	// use report_id so both formats are generated from one durable report.
	alertID := r.URL.Query().Get("alert_id")
	if alertID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "alert_id query parameter required")
		return
	}
	if s.alerts == nil || s.customers == nil || s.transactions == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "legacy STR source storage is not configured")
		return
	}
	setLegacySTRExportHeaders(w)

	alert, err := s.alerts.Get(r.Context(), alertID)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	customer, err := s.customers.Get(r.Context(), alert.CustomerID)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "customer lookup failed")
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}

	switch format {
	case "csv":
		if err := s.exportSTRCSV(r.Context(), w, alert, customer); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, apierr.CodeValidationFailed, err.Error())
		}
	case "json":
		if err := s.exportSTRJSON(r.Context(), w, alert, customer); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, apierr.CodeValidationFailed, err.Error())
		}
	default:
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "unsupported format: "+format)
	}
}

func setLegacySTRExportHeaders(w http.ResponseWriter) {
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Sunset", time.Now().Add(deprecatedOffsetSunsetWindow).UTC().Format(http.TimeFormat))
	w.Header().Set("Link", `</api/v1/reports/str/export>; rel="successor-version"`)
}

func exportFormat(r *http.Request) string {
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		return "csv"
	}
	return format
}

func (s *Server) exportSTRReportCSV(w http.ResponseWriter, report *domain.STRReport, alert *domain.Alert, customer *domain.Customer) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=str_%s.csv", report.ID))

	writer := csv.NewWriter(w)
	defer writer.Flush()
	_ = writer.Write([]string{
		"report_id", "report_status", "report_type", "alert_id", "case_id", "customer_id", "external_id", "customer_type",
		"country_code", "scenario_id", "severity", "alert_status", "suspicious_point", "description", "score", "detected_at",
		"created_at", "updated_at", "submitted_at", "submitted_by", "submission_evidence", "corrects_report_id", "supersedes_report_id", "transaction_ids", "total_amount", "currency",
		"export_version", "exported_at",
	})
	transactionIDs := make([]string, 0, len(report.TransactionSnapshot))
	for _, transaction := range report.TransactionSnapshot {
		transactionIDs = append(transactionIDs, transaction.ID)
	}
	alertSnapshot := reportAlertSnapshot(report, alert)
	customerSnapshot := reportCustomerSnapshot(report, customer)
	exportedAt := time.Now().UTC()
	_ = writer.Write([]string{
		sanitizeCSVCell(report.ID), sanitizeCSVCell(string(report.Status)), sanitizeCSVCell(string(report.ReportType)),
		sanitizeCSVCell(report.AlertID), sanitizeCSVCell(report.CaseID), sanitizeCSVCell(report.CustomerID), sanitizeCSVCell(customerSnapshot.ExternalID),
		sanitizeCSVCell(string(customerSnapshot.CustomerType)), sanitizeCSVCell(customerSnapshot.CountryCode), sanitizeCSVCell(alertSnapshot.ScenarioID),
		sanitizeCSVCell(string(alertSnapshot.Severity)), sanitizeCSVCell(string(alertSnapshot.Status)), sanitizeCSVCell(report.SuspiciousPoint),
		sanitizeCSVCell(alertSnapshot.Description), fmt.Sprintf("%.2f", alertSnapshot.Score), sanitizeCSVCell(alertSnapshot.DetectedAt.Format(time.RFC3339)),
		sanitizeCSVCell(report.CreatedAt.Format(time.RFC3339)), sanitizeCSVCell(report.UpdatedAt.Format(time.RFC3339)),
		formatOptionalTime(report.SubmittedAt), sanitizeCSVCell(report.SubmittedBy), sanitizeCSVCell(report.SubmissionEvidence),
		sanitizeCSVCell(report.CorrectsReportID), sanitizeCSVCell(report.SupersedesReportID),
		sanitizeCSVCell(strings.Join(transactionIDs, ";")), fmt.Sprintf("%.2f", report.TotalAmount), sanitizeCSVCell(report.Currency),
		"str-v1", sanitizeCSVCell(exportedAt.Format(time.RFC3339)),
	})
}

func (s *Server) exportSTRReportJSON(w http.ResponseWriter, report *domain.STRReport, alert *domain.Alert, customer *domain.Customer) {
	transactions := make([]strTransactionExport, 0, len(report.TransactionSnapshot))
	for _, transaction := range report.TransactionSnapshot {
		transactions = append(transactions, strTransactionExport{
			ID: transaction.ID, ExternalID: transaction.ExternalID, Amount: transaction.Amount,
			Currency: transaction.Currency, Direction: string(transaction.Direction), Channel: transaction.Channel,
			ExecutedAt: transaction.ExecutedAt.Format(time.RFC3339), CreatedAt: transaction.CreatedAt.Format(time.RFC3339),
		})
	}
	alertSnapshot := reportAlertSnapshot(report, alert)
	customerSnapshot := reportCustomerSnapshot(report, customer)
	export := strExportJSON{
		ReportID: report.ID, ReportStatus: string(report.Status), ReportType: string(report.ReportType), AlertID: report.AlertID,
		CaseID: report.CaseID, CorrectsReportID: report.CorrectsReportID, SupersedesReportID: report.SupersedesReportID, SuspiciousPoint: report.SuspiciousPoint, CreatedAt: report.CreatedAt,
		UpdatedAt: report.UpdatedAt, SubmittedAt: report.SubmittedAt, SubmittedBy: report.SubmittedBy,
		SubmissionEvidence: report.SubmissionEvidence, TotalAmount: report.TotalAmount, Currency: report.Currency,
		TransactionIDs: append([]string(nil), report.TransactionIDs...),
		Customer:       strCustomerExport{ID: customerSnapshot.ID, ExternalID: customerSnapshot.ExternalID, CustomerType: string(customerSnapshot.CustomerType), CountryCode: customerSnapshot.CountryCode},
		Alert:          strAlertExport{AlertID: alertSnapshot.ID, TransactionIDs: append([]string(nil), alertSnapshot.TransactionIDs...), ScenarioID: alertSnapshot.ScenarioID, Severity: string(alertSnapshot.Severity), Status: string(alertSnapshot.Status), Description: alertSnapshot.Description, Score: alertSnapshot.Score, DetectedAt: alertSnapshot.DetectedAt.Format(time.RFC3339)},
		Transactions:   transactions, ExportedAt: time.Now().UTC(), ExportVersion: "str-v1",
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=str_%s.json", report.ID))
	writeJSON(w, http.StatusOK, export)
}

func reportAlertSnapshot(report *domain.STRReport, alert *domain.Alert) domain.STRAlertSnapshot {
	if report != nil && report.AlertSnapshot.ID != "" {
		return report.AlertSnapshot
	}
	return domain.STRAlertSnapshot{
		ID: alert.ID, CustomerID: alert.CustomerID, ScenarioID: alert.ScenarioID,
		Severity: alert.Severity, Status: alert.Status, Score: alert.Score,
		Description: alert.Description, TransactionIDs: append([]string(nil), alert.TransactionIDs...), DetectedAt: alert.DetectedAt,
	}
}

func reportCustomerSnapshot(report *domain.STRReport, customer *domain.Customer) domain.STRCustomerSnapshot {
	if report != nil && report.CustomerSnapshot.ID != "" {
		return report.CustomerSnapshot
	}
	return domain.STRCustomerSnapshot{ID: customer.ID, ExternalID: customer.ExternalID, CustomerType: customer.CustomerType, CountryCode: customer.CountryCode}
}

// validateSTRReportSnapshot prevents a durable report export from silently
// falling back to mutable live source rows. Reports created after the
// snapshot migration must contain every source row required to reproduce the
// operator's reviewed evidence; malformed or legacy rows fail explicitly.
func validateSTRReportSnapshot(report *domain.STRReport) error {
	if report == nil {
		return errors.New("STR report is unavailable")
	}
	if report.AlertSnapshot.ID == "" || !domain.SameIdentifier(report.AlertSnapshot.ID, report.AlertID) {
		return errors.New("STR alert snapshot is unavailable")
	}
	if report.CustomerSnapshot.ID == "" || !domain.SameIdentifier(report.CustomerSnapshot.ID, report.CustomerID) {
		return errors.New("STR customer snapshot is unavailable")
	}
	if !domain.SameIdentifier(report.AlertSnapshot.CustomerID, report.CustomerID) {
		return errors.New("STR alert snapshot customer link is incomplete")
	}
	if len(report.TransactionIDs) == 0 || len(report.TransactionSnapshot) == 0 {
		return errors.New("STR transaction snapshot is unavailable")
	}
	if len(report.TransactionIDs) != len(report.TransactionSnapshot) {
		return errors.New("STR transaction snapshot is incomplete")
	}
	if len(report.AlertSnapshot.TransactionIDs) == 0 || len(report.AlertSnapshot.TransactionIDs) != len(report.TransactionSnapshot) {
		return errors.New("STR alert transaction snapshot is incomplete")
	}
	var totalAmount float64
	for index, transaction := range report.TransactionSnapshot {
		if strings.TrimSpace(transaction.ID) == "" || strings.TrimSpace(transaction.Currency) == "" {
			return errors.New("STR transaction snapshot is incomplete")
		}
		if !domain.SameIdentifier(report.TransactionIDs[index], transaction.ID) {
			return errors.New("STR transaction snapshot does not match transaction links")
		}
		if !domain.SameIdentifier(report.AlertSnapshot.TransactionIDs[index], transaction.ID) {
			return errors.New("STR alert transaction snapshot does not match transaction links")
		}
		if report.Currency != "" && report.Currency != transaction.Currency {
			return errors.New("STR transaction snapshot currency does not match report")
		}
		totalAmount += transaction.Amount
	}
	if math.Abs(totalAmount-report.TotalAmount) > 0.01 {
		return errors.New("STR transaction snapshot total does not match report")
	}
	return nil
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}

func (s *Server) exportSTRCSV(ctx context.Context, w http.ResponseWriter, alert *domain.Alert, customer *domain.Customer) error {
	if err := s.validateLegacySTRTransactions(ctx, alert.TransactionIDs); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=str_%s.csv", alert.ID))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	writer.Write([]string{
		"report_id", "alert_id", "customer_id", "external_id", "customer_type",
		"country_code", "scenario_id", "severity", "description",
		"transaction_ids", "score", "detected_at",
	})

	writer.Write([]string{
		sanitizeCSVCell("STR-" + alert.ID),
		sanitizeCSVCell(alert.ID),
		sanitizeCSVCell(alert.CustomerID),
		sanitizeCSVCell(customer.ExternalID),
		sanitizeCSVCell(string(customer.CustomerType)),
		sanitizeCSVCell(customer.CountryCode),
		sanitizeCSVCell(alert.ScenarioID),
		sanitizeCSVCell(string(alert.Severity)),
		sanitizeCSVCell(alert.Description),
		sanitizeCSVCell(strings.Join(alert.TransactionIDs, ";")),
		fmt.Sprintf("%.2f", alert.Score),
		sanitizeCSVCell(alert.DetectedAt.Format(time.RFC3339)),
	})
	return nil
}

func sanitizeCSVCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r', '\n':
		return "'" + s
	}
	return s
}

type strExportJSON struct {
	ReportID           string                 `json:"report_id"`
	ReportStatus       string                 `json:"report_status"`
	ReportType         string                 `json:"report_type"`
	AlertID            string                 `json:"alert_id"`
	CaseID             string                 `json:"case_id,omitempty"`
	CorrectsReportID   string                 `json:"corrects_report_id,omitempty"`
	SupersedesReportID string                 `json:"supersedes_report_id,omitempty"`
	SuspiciousPoint    string                 `json:"suspicious_point"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
	SubmittedAt        *time.Time             `json:"submitted_at,omitempty"`
	SubmittedBy        string                 `json:"submitted_by,omitempty"`
	SubmissionEvidence string                 `json:"submission_evidence,omitempty"`
	TotalAmount        float64                `json:"total_amount"`
	Currency           string                 `json:"currency"`
	TransactionIDs     []string               `json:"transaction_ids"`
	Customer           strCustomerExport      `json:"customer"`
	Alert              strAlertExport         `json:"alert"`
	Transactions       []strTransactionExport `json:"transactions"`
	ExportVersion      string                 `json:"export_version"`
	ExportedAt         time.Time              `json:"exported_at"`
}

type strCustomerExport struct {
	ID           string `json:"id"`
	ExternalID   string `json:"external_id"`
	CustomerType string `json:"customer_type"`
	CountryCode  string `json:"country_code"`
}

type strAlertExport struct {
	AlertID        string   `json:"alert_id"`
	TransactionIDs []string `json:"transaction_ids"`
	ScenarioID     string   `json:"scenario_id"`
	Severity       string   `json:"severity"`
	Status         string   `json:"status"`
	Description    string   `json:"description"`
	Score          float64  `json:"score"`
	DetectedAt     string   `json:"detected_at"`
}

type strTransactionExport struct {
	ID         string  `json:"id"`
	ExternalID string  `json:"external_id,omitempty"`
	Amount     float64 `json:"amount"`
	Currency   string  `json:"currency"`
	Direction  string  `json:"direction"`
	Channel    string  `json:"channel"`
	ExecutedAt string  `json:"executed_at,omitempty"`
	CreatedAt  string  `json:"created_at,omitempty"`
}

func (s *Server) exportSTRJSON(ctx context.Context, w http.ResponseWriter, alert *domain.Alert, customer *domain.Customer) error {
	var txnExports []strTransactionExport
	for _, txnID := range alert.TransactionIDs {
		txn, err := s.transactions.Get(ctx, txnID)
		if err != nil {
			return fmt.Errorf("STR transaction %s is unavailable: %w", txnID, err)
		}
		txnExports = append(txnExports, strTransactionExport{
			ID: txn.ID, ExternalID: txn.ExternalID,
			Amount:    txn.Amount,
			Currency:  txn.Currency,
			Direction: string(txn.Direction),
			Channel:   txn.Channel, ExecutedAt: txn.ExecutedAt.Format(time.RFC3339), CreatedAt: txn.CreatedAt.Format(time.RFC3339),
		})
	}

	export := strExportJSON{
		ReportID:       "STR-" + alert.ID,
		AlertID:        alert.ID,
		TransactionIDs: append([]string(nil), alert.TransactionIDs...),
		Customer: strCustomerExport{
			ID:           customer.ID,
			ExternalID:   customer.ExternalID,
			CustomerType: string(customer.CustomerType),
			CountryCode:  customer.CountryCode,
		},
		Alert: strAlertExport{
			AlertID:        alert.ID,
			TransactionIDs: append([]string(nil), alert.TransactionIDs...),
			ScenarioID:     alert.ScenarioID,
			Severity:       string(alert.Severity),
			Description:    alert.Description,
			Score:          alert.Score,
			DetectedAt:     alert.DetectedAt.Format(time.RFC3339),
		},
		Transactions: txnExports,
		ExportedAt:   time.Now(),
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=str_%s.json", alert.ID))
	writeJSON(w, http.StatusOK, export)
	return nil
}

func (s *Server) validateLegacySTRTransactions(ctx context.Context, transactionIDs []string) error {
	if len(transactionIDs) == 0 {
		return errors.New("STR transaction links are empty")
	}
	for _, transactionID := range transactionIDs {
		if strings.TrimSpace(transactionID) == "" {
			return errors.New("STR transaction links are incomplete")
		}
		if _, err := s.transactions.Get(ctx, transactionID); err != nil {
			return fmt.Errorf("STR transaction %s is unavailable: %w", transactionID, err)
		}
	}
	return nil
}
