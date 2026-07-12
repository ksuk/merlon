package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ksuk/merlon/api/internal/apierr"
	"net/http"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

type CreateSTRRequest struct {
	AlertID         string `json:"alert_id"`
	SuspiciousPoint string `json:"suspicious_point"`
	CreatedBy       string `json:"created_by"`
}

func (s *Server) handleCreateSTR(w http.ResponseWriter, r *http.Request) {
	var req CreateSTRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}

	if req.AlertID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "alert_id required")
		return
	}
	if req.SuspiciousPoint == "" {
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

	var currency string
	var totalAmount float64
	for _, txnID := range alert.TransactionIDs {
		txn, err := s.transactions.Get(r.Context(), txnID)
		if err != nil {
			continue
		}
		if currency == "" {
			currency = txn.Currency
		} else if txn.Currency != currency {
			writeErrorCode(w, http.StatusUnprocessableEntity, apierr.CodeValidationFailed, "STR requires transactions in one currency")
			return
		}
		totalAmount += txn.Amount
	}

	if currency == "" {
		writeErrorCode(w, http.StatusUnprocessableEntity, apierr.CodeValidationFailed, "STR requires at least one available transaction")
		return
	}

	now := time.Now()
	report := domain.STRReport{
		ID:              generateID(),
		AlertID:         alert.ID,
		CustomerID:      alert.CustomerID,
		ReportType:      domain.ReportTypeSTR,
		Status:          domain.ReportStatusDraft,
		SuspiciousPoint: req.SuspiciousPoint,
		TransactionIDs:  alert.TransactionIDs,
		TotalAmount:     totalAmount,
		Currency:        currency,
		CreatedAt:       now,
		CreatedBy:       req.CreatedBy,
	}

	s.dispatchWebhook(r.Context(), domain.WebhookEventSTRCreated, report)

	writeJSON(w, http.StatusCreated, report)
}

func (s *Server) handleExportSTR(w http.ResponseWriter, r *http.Request) {
	alertID := r.URL.Query().Get("alert_id")
	if alertID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "alert_id query parameter required")
		return
	}

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
		s.exportSTRCSV(w, alert, customer)
	case "json":
		s.exportSTRJSON(r.Context(), w, alert, customer)
	default:
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "unsupported format: "+format)
	}
}

func (s *Server) exportSTRCSV(w http.ResponseWriter, alert *domain.Alert, customer *domain.Customer) {
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
	ReportID     string                 `json:"report_id"`
	AlertID      string                 `json:"alert_id"`
	Customer     strCustomerExport      `json:"customer"`
	Alert        strAlertExport         `json:"alert"`
	Transactions []strTransactionExport `json:"transactions"`
	ExportedAt   time.Time              `json:"exported_at"`
}

type strCustomerExport struct {
	ID           string `json:"id"`
	ExternalID   string `json:"external_id"`
	CustomerType string `json:"customer_type"`
	CountryCode  string `json:"country_code"`
}

type strAlertExport struct {
	ScenarioID  string  `json:"scenario_id"`
	Severity    string  `json:"severity"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
	DetectedAt  string  `json:"detected_at"`
}

type strTransactionExport struct {
	ID        string  `json:"id"`
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
	Direction string  `json:"direction"`
	Channel   string  `json:"channel"`
}

func (s *Server) exportSTRJSON(ctx context.Context, w http.ResponseWriter, alert *domain.Alert, customer *domain.Customer) {
	var txnExports []strTransactionExport
	for _, txnID := range alert.TransactionIDs {
		txn, err := s.transactions.Get(ctx, txnID)
		if err != nil {
			continue
		}
		txnExports = append(txnExports, strTransactionExport{
			ID:        txn.ID,
			Amount:    txn.Amount,
			Currency:  txn.Currency,
			Direction: string(txn.Direction),
			Channel:   txn.Channel,
		})
	}

	export := strExportJSON{
		ReportID: "STR-" + alert.ID,
		AlertID:  alert.ID,
		Customer: strCustomerExport{
			ID:           customer.ID,
			ExternalID:   customer.ExternalID,
			CustomerType: string(customer.CustomerType),
			CountryCode:  customer.CountryCode,
		},
		Alert: strAlertExport{
			ScenarioID:  alert.ScenarioID,
			Severity:    string(alert.Severity),
			Description: alert.Description,
			Score:       alert.Score,
			DetectedAt:  alert.DetectedAt.Format(time.RFC3339),
		},
		Transactions: txnExports,
		ExportedAt:   time.Now(),
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=str_%s.json", alert.ID))
	writeJSON(w, http.StatusOK, export)
}
