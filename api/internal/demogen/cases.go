package demogen

import (
	"fmt"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// analysts are A9's 4 fixed case-management analysts.
var analysts = []string{"m.sato", "k.watanabe", "a.ito", "r.kobayashi"}

// caseNoteRecord is case_notes.json's row shape: domain.CaseNote plus the
// case_id CaseRepository.AddNote takes as a separate argument (CaseNote
// itself carries no case_id field), so a flat file can be replayed as a
// sequence of AddNote calls.
type caseNoteRecord struct {
	CaseID    string    `json:"case_id"`
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// caseSeed is one case-to-be, before ID/status/priority/notes are finalized.
type caseSeed struct {
	CustomerID string
	AlertIDs   []string
	Priority   domain.CasePriority
	Status     domain.CaseStatus
	Summary    string
	// anchorTime is the latest DetectedAt among AlertIDs, used to date
	// case_notes chronologically after the alert(s) that opened the case.
	AnchorTime time.Time
}

// storyCaseSeeds returns the 6 story-linked case seeds (A6 narrative
// order), unfinalized: callers must run finalizeCases exactly once over the
// concatenation of these and backgroundCaseSeeds' output, so case IDs
// (demo-case-01..24) are assigned continuously across both groups rather
// than restarting at 01 for each (see Generate in demogen.go).
func storyCaseSeeds(storyAlerts map[string][]domain.Alert) []caseSeed {
	var seeds []caseSeed

	latestDetected := func(alerts []domain.Alert) time.Time {
		var t time.Time
		for _, a := range alerts {
			if a.DetectedAt.After(t) {
				t = a.DetectedAt
			}
		}
		return t
	}
	ids := func(alerts []domain.Alert) []string {
		out := make([]string, len(alerts))
		for i, a := range alerts {
			out[i] = a.ID
		}
		return out
	}

	// Story-linked cases, in A6 narrative order (fixed IDs demo-case-01..06).
	story1 := storyAlerts["demo-story-01"]
	seeds = append(seeds, caseSeed{CustomerID: "demo-story-01", AlertIDs: ids(story1), Priority: domain.CasePriorityMedium, Status: domain.CaseStatusClosed,
		Summary: "ストラクチャリング(送金取りまとめ屋)の疑いで検知。3件の入金を統合して送金していた実態を確認し、対応済み。", AnchorTime: latestDetected(story1)})

	story2 := storyAlerts["demo-story-02"]
	seeds = append(seeds, caseSeed{CustomerID: "demo-story-02", AlertIDs: ids(story2), Priority: domain.CasePriorityHigh, Status: domain.CaseStatusClosed,
		Summary: "高頻度小口取引(売り口座ミュールの疑い)。開設2ヶ月・無職の属性と取引パターンを確認し、対応済み。", AnchorTime: latestDetected(story2)})

	story3 := storyAlerts["demo-story-03"]
	seeds = append(seeds, caseSeed{CustomerID: "demo-story-03", AlertIDs: ids(story3), Priority: domain.CasePriorityMedium, Status: domain.CaseStatusClosed,
		Summary: "ハイリスク国(MM)向け送金の検知。中古車輸出業の実態を確認し、対応済み。", AnchorTime: latestDetected(story3)})

	story4 := storyAlerts["demo-story-04"]
	seeds = append(seeds, caseSeed{CustomerID: "demo-story-04", AlertIDs: ids(story4), Priority: domain.CasePriorityCritical, Status: domain.CaseStatusInvestigating,
		Summary: "急速資金移動(パススルー構造の疑い)。設立1年未満の法人による短期間での資金の入出を調査中。", AnchorTime: latestDetected(story4)})

	story5 := storyAlerts["demo-story-05"]
	seeds = append(seeds, caseSeed{CustomerID: "demo-story-05", AlertIDs: ids(story5), Priority: domain.CasePriorityLow, Status: domain.CaseStatusClosed,
		Summary: "休眠口座(420日無取引)の急な再活性化を検知。本人確認の上、口座を凍結済み。", AnchorTime: latestDetected(story5)})

	story6 := storyAlerts["demo-story-06"]
	seeds = append(seeds, caseSeed{CustomerID: "demo-story-06", AlertIDs: ids(story6), Priority: domain.CasePriorityHigh, Status: domain.CaseStatusInvestigating,
		Summary: "ストラクチャリング(異なる集計窓で2件)+急速資金移動(1件)の複合検知。3件のアラートを統合し、リスクの時系列悪化を調査中。", AnchorTime: latestDetected(story6)})

	return seeds
}

// backgroundCaseSeeds builds A9's remaining 18 background case seeds from a
// deterministically-chosen slice of the FP alert population
// (fpAlertsByCustomer/order), distributing priority so that the *overall*
// 24-case population (story cases included) lands at low4/medium12/high6/
// critical2. See storyCaseSeeds' doc comment on why this returns seeds
// rather than finalized cases.
func backgroundCaseSeeds(fpAlertsByCustomer map[string][]domain.Alert, order []string) []caseSeed {
	// Story cases already contribute low1/medium2/high2/critical1 (see
	// buildCases): remaining targets are low3/medium10/high4/critical1 = 18.
	priorities := make([]domain.CasePriority, 0, 18)
	for i := 0; i < 3; i++ {
		priorities = append(priorities, domain.CasePriorityLow)
	}
	for i := 0; i < 10; i++ {
		priorities = append(priorities, domain.CasePriorityMedium)
	}
	for i := 0; i < 4; i++ {
		priorities = append(priorities, domain.CasePriorityHigh)
	}
	priorities = append(priorities, domain.CasePriorityCritical)

	statusForAlert := func(status domain.AlertStatus) domain.CaseStatus {
		switch status {
		case domain.AlertStatusClosedFalsePositive, domain.AlertStatusClosedTruePositive:
			return domain.CaseStatusClosed
		case domain.AlertStatusEscalated:
			return domain.CaseStatusEscalated
		case domain.AlertStatusInvestigating:
			return domain.CaseStatusInvestigating
		default:
			return domain.CaseStatusNew
		}
	}

	var seeds []caseSeed
	for i, customerID := range order {
		if len(seeds) >= len(priorities) {
			break
		}
		alerts := fpAlertsByCustomer[customerID]
		if len(alerts) == 0 {
			continue
		}
		seeds = append(seeds, caseSeed{
			CustomerID: customerID,
			AlertIDs:   []string{alerts[0].ID},
			Priority:   priorities[len(seeds)],
			Status:     statusForAlert(alerts[0].Status),
			Summary:    fmt.Sprintf("%sシナリオのアラートを調査。誤検知の可能性を含め精査。", alerts[0].ScenarioID),
			AnchorTime: alerts[0].DetectedAt,
		})
		_ = i
	}
	return seeds
}

func finalizeCases(anchor time.Time, seeds []caseSeed) ([]domain.Case, []caseNoteRecord) {
	var cases []domain.Case
	var notes []caseNoteRecord
	for i, s := range seeds {
		caseID := fmt.Sprintf("demo-case-%02d", i+1)
		createdAt := s.AnchorTime.Add(2 * time.Hour)
		if createdAt.IsZero() || s.AnchorTime.IsZero() {
			createdAt = anchor
		}
		analyst := analysts[i%len(analysts)]
		c := domain.Case{
			ID:         caseID,
			CustomerID: s.CustomerID,
			AlertIDs:   s.AlertIDs,
			Status:     s.Status,
			Priority:   s.Priority,
			AssignedTo: analyst,
			Summary:    s.Summary,
			CreatedAt:  createdAt,
			UpdatedAt:  createdAt,
		}
		if s.Status == domain.CaseStatusClosed {
			closedAt := createdAt.Add(48 * time.Hour)
			c.ClosedAt = &closedAt
			c.UpdatedAt = closedAt
		}
		cases = append(cases, c)

		noteCount := 1 + i%4 // 1-4 notes per case (A9)
		for n := 0; n < noteCount; n++ {
			noteAuthor := analysts[(i+n)%len(analysts)]
			noteAt := createdAt.Add(time.Duration(n+1) * 6 * time.Hour)
			notes = append(notes, caseNoteRecord{
				CaseID:    caseID,
				ID:        fmt.Sprintf("demo-note-%s-%02d", caseID[len("demo-case-"):], n+1),
				Author:    noteAuthor,
				Content:   caseNoteContent(s.Summary, n, noteCount),
				CreatedAt: noteAt,
			})
		}
	}
	return cases, notes
}

func caseNoteContent(summary string, n, total int) string {
	switch {
	case n == 0:
		return "アラート内容を確認。" + summary
	case n == total-1:
		return "調査完了。判定結果を記録しクローズ処理。"
	default:
		return "追加調査を実施。関連取引・顧客属性を確認中。"
	}
}
