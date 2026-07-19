package demogen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ksuk/merlon/api/internal/domain"
)

// storyIDsInput is everything buildStoryIDsMarkdown needs to render
// STORY_IDS.md: the fixed IDs and a short human summary of each A6 story,
// A8 screening hit, plus the case each rolls up into.
type storyIDsInput struct {
	Anchor string
	Seed   int64

	Stories []storyIDRow
	Screens []screeningIDRow
}

type storyIDRow struct {
	Story      int
	CustomerID string
	Name       string
	Purpose    string
	AlertIDs   []string
	CaseID     string
	FirstTxnID string
	LastTxnID  string
	TxnCount   int
}

type screeningIDRow struct {
	CustomerID string
	Name       string
	EntryID    string
	Status     string
}

// buildStoryIDsMarkdown renders deploy/seed/demo/STORY_IDS.md: T1-W2
// instructions require this file (and screening_lists/*.yaml) to be a
// golden-tested, committed artifact, so its content must be a pure function
// of Generate's output — no formatting choice here may depend on wall-clock
// time or map iteration order.
func buildStoryIDsMarkdown(in storyIDsInput) string {
	var b strings.Builder
	b.WriteString("# Synthetic demo story IDs\n\n")
	b.WriteString("Fixed IDs for the PH7 demo tour (T5'/T6'). Every name, company, and\n")
	b.WriteString("transaction below is synthetic (DD3) — regenerate with `make demogen`\n")
	b.WriteString(fmt.Sprintf("(seed=%d, anchor=%s).\n\n", in.Seed, in.Anchor))

	b.WriteString("## A6 stories\n\n")
	b.WriteString("| # | Customer ID | Name | Purpose | Alert ID(s) | Case ID | Transactions |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for _, s := range in.Stories {
		txnRange := "-"
		if s.TxnCount > 0 {
			txnRange = fmt.Sprintf("%s..%s (%d)", s.FirstTxnID, s.LastTxnID, s.TxnCount)
		}
		b.WriteString(fmt.Sprintf("| %d | `%s` | %s | %s | %s | `%s` | %s |\n",
			s.Story, s.CustomerID, s.Name, s.Purpose, formatIDList(s.AlertIDs), s.CaseID, txnRange))
	}

	b.WriteString("\n## A8 screening hits\n\n")
	b.WriteString("| Customer ID | Name | List entry | Status |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, s := range in.Screens {
		b.WriteString(fmt.Sprintf("| `%s` | %s | `%s` | %s |\n", s.CustomerID, s.Name, s.EntryID, s.Status))
	}

	b.WriteString("\nThe generator is deterministic (fixed seed/anchor) and keeps these IDs\n")
	b.WriteString("unchanged across regenerations.\n")
	return b.String()
}

func formatIDList(ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = "`" + id + "`"
	}
	return strings.Join(quoted, ", ")
}

// customerDisplayName reads attributes.name for STORY_IDS.md display.
func customerDisplayName(c domain.Customer) string {
	if name, ok := c.Attributes["name"].(string); ok {
		return name
	}
	return c.ID
}

// storyPurposes is A6's one-line narrative label per story, in order.
var storyPurposes = []string{
	"ストラクチャリング(送金取りまとめ屋)",
	"高頻度小口取引(売り口座ミュール)",
	"ハイリスク国送金(中古車輸出)",
	"急速資金移動(パススルー)",
	"休眠口座再活性化",
	"複合(structuring x2 + rapid_movement x1)",
}

// assembleStoryIDsInput gathers everything buildStoryIDsMarkdown needs from
// Generate's already-built data: story customers keep their fixed
// "demo-story-0N" IDs in A6 order, and (per storyCaseSeeds/finalizeCases)
// story cases are always the first len(storyPurposes) entries of cases.
func assembleStoryIDsInput(seed int64, anchorStr string, customers []domain.Customer, storyIDs []string, allTxns []domain.Transaction, storyAlertsByCustomer map[string][]domain.Alert, cases []domain.Case, screeningResults []domain.ScreeningResultRecord, screeningLists []screeningListSeed) storyIDsInput {
	byID := make(map[string]domain.Customer, len(customers))
	for _, c := range customers {
		byID[c.ID] = c
	}
	txnsByCustomer := map[string][]string{}
	for _, t := range allTxns {
		txnsByCustomer[t.CustomerID] = append(txnsByCustomer[t.CustomerID], t.ID)
	}

	var rows []storyIDRow
	for i, custID := range storyIDs {
		ids := txnsByCustomer[custID]
		sort.Strings(ids)
		row := storyIDRow{
			Story:      i + 1,
			CustomerID: custID,
			Name:       customerDisplayName(byID[custID]),
			Purpose:    storyPurposes[i],
			TxnCount:   len(ids),
		}
		if len(ids) > 0 {
			row.FirstTxnID = ids[0]
			row.LastTxnID = ids[len(ids)-1]
		}
		for _, a := range storyAlertsByCustomer[custID] {
			row.AlertIDs = append(row.AlertIDs, a.ID)
		}
		if i < len(cases) {
			row.CaseID = cases[i].ID
		}
		rows = append(rows, row)
	}

	var screens []screeningIDRow
	for _, r := range screeningResults {
		screens = append(screens, screeningIDRow{
			CustomerID: r.CustomerID,
			Name:       customerDisplayName(byID[r.CustomerID]),
			EntryID:    r.EntryID,
			Status:     string(r.Status),
		})
	}

	return storyIDsInput{Anchor: anchorStr, Seed: seed, Stories: rows, Screens: screens}
}
