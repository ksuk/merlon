package coverage

import (
	"sort"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/outcome"
)

// BuildKnownMatterUnion turns durable internal evidence into the reference
// side of a coverage comparison. A qualifying case is the primary matter;
// submitted STRs without such a case are next, and a closed true-positive
// alert is the final fallback. Linked rows are intentionally de-duplicated so
// one investigation cannot inflate the denominator three times.
//
// The function is pure and deterministic. A composition root can load the
// source rows as-of a snapshot, then pass this union to the shared matcher.
func BuildKnownMatterUnion(alerts []domain.Alert, cases []domain.Case, reports []domain.STRReport) []outcome.Reference {
	alertsByID := make(map[string]domain.Alert, len(alerts))
	for _, alert := range alerts {
		alertsByID[alert.ID] = alert
	}
	claimedAlerts := map[string]struct{}{}
	claimedCases := map[string]struct{}{}
	result := make([]outcome.Reference, 0)

	orderedCases := append([]domain.Case(nil), cases...)
	sort.Slice(orderedCases, func(i, j int) bool { return orderedCases[i].ID < orderedCases[j].ID })
	for _, item := range orderedCases {
		if !qualifyingCase(item) || item.ID == "" {
			continue
		}
		claimedCases[item.ID] = struct{}{}
		detection := caseDetection(item, alertsByID)
		result = append(result, outcome.Reference{Detection: detection, State: outcome.HistoricalState{CaseStatus: item.Status, STRFiled: item.STRFiledAt != nil}, Provenance: map[string]string{"source": "case", "case_id": item.ID}})
		for _, alertID := range item.AlertIDs {
			claimedAlerts[alertID] = struct{}{}
		}
	}

	orderedReports := append([]domain.STRReport(nil), reports...)
	sort.Slice(orderedReports, func(i, j int) bool { return orderedReports[i].ID < orderedReports[j].ID })
	for _, report := range orderedReports {
		if report.ID == "" || report.Status != domain.ReportStatusSubmitted {
			continue
		}
		if report.CaseID != "" {
			if _, exists := claimedCases[report.CaseID]; exists {
				continue
			}
		}
		if report.AlertID != "" {
			if _, exists := claimedAlerts[report.AlertID]; exists {
				continue
			}
		}
		detection := outcome.Detection{ID: "str:" + report.ID, CustomerID: report.CustomerID, ScenarioID: report.AlertSnapshot.ScenarioID, TransactionIDs: append([]string(nil), report.TransactionIDs...), DetectedAt: report.AlertSnapshot.DetectedAt}
		if detection.CustomerID == "" {
			detection.CustomerID = report.AlertSnapshot.CustomerID
		}
		result = append(result, outcome.Reference{Detection: detection, State: outcome.HistoricalState{STRFiled: true}, Provenance: map[string]string{"source": "str", "report_id": report.ID}})
		if report.AlertID != "" {
			claimedAlerts[report.AlertID] = struct{}{}
		}
	}

	orderedAlerts := append([]domain.Alert(nil), alerts...)
	sort.Slice(orderedAlerts, func(i, j int) bool { return orderedAlerts[i].ID < orderedAlerts[j].ID })
	for _, alert := range orderedAlerts {
		if alert.ID == "" || alert.Status != domain.AlertStatusClosedTruePositive {
			continue
		}
		if _, exists := claimedAlerts[alert.ID]; exists {
			continue
		}
		result = append(result, outcome.Reference{Detection: outcome.Detection{ID: "alert:" + alert.ID, CustomerID: alert.CustomerID, ScenarioID: alert.ScenarioID, TransactionIDs: append([]string(nil), alert.TransactionIDs...), DetectedAt: alert.DetectedAt}, State: outcome.HistoricalState{AlertStatus: alert.Status}, Provenance: map[string]string{"source": "alert", "alert_id": alert.ID}})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func qualifyingCase(item domain.Case) bool {
	return item.Status == domain.CaseStatusEscalated || item.Status == domain.CaseStatusStrFiled || item.STRFiledAt != nil
}

func caseDetection(item domain.Case, alerts map[string]domain.Alert) outcome.Detection {
	detection := outcome.Detection{ID: "case:" + item.ID, CustomerID: item.CustomerID}
	for _, alertID := range item.AlertIDs {
		alert, ok := alerts[alertID]
		if !ok {
			continue
		}
		if detection.CustomerID == "" {
			detection.CustomerID = alert.CustomerID
		}
		detection.TransactionIDs = append(detection.TransactionIDs, alert.TransactionIDs...)
		if detection.DetectedAt.IsZero() || (!alert.DetectedAt.IsZero() && alert.DetectedAt.Before(detection.DetectedAt)) {
			detection.DetectedAt = alert.DetectedAt
		}
	}
	return detection
}
