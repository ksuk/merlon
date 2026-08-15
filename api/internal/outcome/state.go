package outcome

import (
	"sort"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// HistoricalStateAt reconstructs the label inputs from append-only decision
// and STR history plus the score history effective at the requested snapshot.
// The alert supplied by the caller must itself be an as-of snapshot; this
// function never reads a mutable current customer tier as a fallback.
func HistoricalStateAt(alert domain.Alert, decisions []domain.AlertDecisionEvent, cases []domain.Case, reports []domain.STRReport, scores []domain.ScoreRecord, snapshot time.Time) HistoricalState {
	state := HistoricalState{AlertStatus: alert.Status}
	ordered := append([]domain.AlertDecisionEvent(nil), decisions...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
		}
		return ordered[i].ID < ordered[j].ID
	})
	for i := range ordered {
		decision := ordered[i]
		if decision.AlertID != alert.ID || afterSnapshot(decision.CreatedAt, snapshot) {
			continue
		}
		copy := decision
		state.Decision = &copy
		state.AlertStatus = decision.ToStatus
	}
	for _, item := range cases {
		if !caseContainsAlert(item, alert.ID) || afterSnapshot(item.UpdatedAt, snapshot) {
			continue
		}
		if item.Status == domain.CaseStatusEscalated || item.Status == domain.CaseStatusStrFiled || item.STRFiledAt != nil && !afterSnapshot(*item.STRFiledAt, snapshot) {
			state.CaseStatus = item.Status
		}
	}
	for _, report := range reports {
		if report.AlertID != alert.ID || report.Status != domain.ReportStatusSubmitted || report.SubmittedAt == nil || afterSnapshot(*report.SubmittedAt, snapshot) {
			continue
		}
		state.STRFiled = true
	}
	if tier, ok := TierAt(scores, snapshot); ok {
		state.ScoreTier, state.ScoreTierKnown = tier, true
	}
	return state
}

func caseContainsAlert(item domain.Case, alertID string) bool {
	for _, candidate := range item.AlertIDs {
		if candidate == alertID {
			return true
		}
	}
	return false
}
