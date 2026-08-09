package server

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
)

// exportPendingEvaluationPageSize is the read size used to drain the queue for
// an export. Export has no natural page size -- it must return every row
// matching the filter -- so it pages until exhausted rather than truncating.
const exportPendingEvaluationPageSize = 500

// maxExportedPendingEvaluations bounds one export. Beyond it the operator is
// told to narrow the filter rather than handed a file that silently stops.
const maxExportedPendingEvaluations = 50000

// handleExportPendingEvaluations serves the monitoring-gap queue as evidence.
// A PENDING_REVIEW backlog is a record of transactions that were not screened
// when they arrived, which is exactly the kind of thing an examiner asks to
// see in full; reading it a page at a time through the UI is not an answer.
//
// It reuses the listing endpoint's filter and the audit export's writers, so
// the exported set is precisely what the operator was looking at, in the
// format the rest of the system already produces.
func (s *Server) handleExportPendingEvaluations(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.pendingEvals.(domain.PendingEvaluationWorkflowRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "pending evaluation workflow not configured")
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "unsupported format: "+format)
		return
	}
	filter, status, message := parsePendingEvaluationFilter(r)
	if status != 0 {
		writeErrorCode(w, status, apierr.CodeValidationFailed, message)
		return
	}

	items := make([]domain.PendingEvaluation, 0)
	for {
		page, err := repo.ListPendingEvaluations(r.Context(), filter, exportPendingEvaluationPageSize)
		if err != nil {
			writeWave3Error(w, err)
			return
		}
		items = append(items, page...)
		if len(page) < exportPendingEvaluationPageSize {
			break
		}
		if len(items) >= maxExportedPendingEvaluations {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "export exceeds "+strconv.Itoa(maxExportedPendingEvaluations)+" records; narrow the filter")
			return
		}
		last := page[len(page)-1]
		filter.Cursor = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}

	setAuditDetail(r, "export_format", format)
	setAuditDetail(r, "export_count", strconv.Itoa(len(items)))

	if format == "json" {
		writeJSON(w, http.StatusOK, items)
		return
	}
	writePendingEvaluationCSV(w, items)
}

func writePendingEvaluationCSV(w http.ResponseWriter, items []domain.PendingEvaluation) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=pending_evaluations.csv")

	writer := csv.NewWriter(w)
	defer writer.Flush()

	writer.Write([]string{
		"id", "customer_id", "status", "reason", "batch_run_id", "transaction_ids", "alert_ids",
		"retry_count", "manual_retry_count", "created_at", "last_attempt_at", "next_retry_at",
		"escalated_at", "resolved_at",
	})
	for _, item := range items {
		batchRunID := ""
		if item.BatchRunID != nil {
			batchRunID = *item.BatchRunID
		}
		writer.Write([]string{
			sanitizeCSVCell(item.ID),
			sanitizeCSVCell(item.CustomerID),
			sanitizeCSVCell(string(item.Status)),
			sanitizeCSVCell(item.Reason),
			sanitizeCSVCell(batchRunID),
			sanitizeCSVCell(strings.Join(item.TransactionIDs, " ")),
			sanitizeCSVCell(strings.Join(item.AlertIDs, " ")),
			strconv.Itoa(item.RetryCount),
			strconv.Itoa(item.ManualRetryCount),
			item.CreatedAt.Format(time.RFC3339),
			formatOptionalTime(item.LastAttemptAt),
			formatOptionalTime(item.NextRetryAt),
			formatOptionalTime(item.EscalatedAt),
			formatOptionalTime(item.ResolvedAt),
		})
	}
}
