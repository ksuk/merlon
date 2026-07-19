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

// storyIDRow carries both the human-readable generation-time label (for
// narrative reference within this document) and the deterministic UUID
// (uuidFor(label)) that is the entity's *actual* primary key once
// remapIDsToUUIDs runs — the ID T5'/T6' must use to build a working demo-
// tour URL (e.g. GET /api/v1/customers/{CustomerID}).
type storyIDRow struct {
	Story         int
	CustomerLabel string
	CustomerID    string
	Name          string
	Purpose       string
	AlertIDs      []string // UUIDs
	CaseID        string   // UUID
	FirstTxnID    string   // UUID
	LastTxnID     string   // UUID
	TxnCount      int
}

type screeningIDRow struct {
	CustomerLabel string
	CustomerID    string
	Name          string
	EntryID       string
	Status        string
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
	b.WriteString("Each row's **Label** is this generator's internal, human-readable name for\n")
	b.WriteString("the entity (used only in this document and in the generator's own source);\n")
	b.WriteString("the actual primary key the HTTP API expects (e.g.\n")
	b.WriteString("`GET /api/v1/customers/{id}`) is the UUID column, derived deterministically\n")
	b.WriteString("as `uuidFor(label)` — an RFC 4122 v5 (SHA-1, fixed namespace + label) UUID.\n")
	b.WriteString("Regenerating reproduces the same UUID for the same label every time.\n\n")

	b.WriteString("## A6 stories\n\n")
	b.WriteString("| # | Label | Customer ID | Name | Purpose | Alert ID(s) | Case ID | Transactions |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, s := range in.Stories {
		txnRange := "-"
		if s.TxnCount > 0 {
			txnRange = fmt.Sprintf("%s..%s (%d)", s.FirstTxnID, s.LastTxnID, s.TxnCount)
		}
		b.WriteString(fmt.Sprintf("| %d | %s | `%s` | %s | %s | %s | `%s` | %s |\n",
			s.Story, s.CustomerLabel, s.CustomerID, s.Name, s.Purpose, formatIDList(s.AlertIDs), s.CaseID, txnRange))
	}

	b.WriteString("\n## A8 screening hits\n\n")
	b.WriteString("| Label | Customer ID | Name | List entry | Status |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, s := range in.Screens {
		b.WriteString(fmt.Sprintf("| %s | `%s` | %s | `%s` | %s |\n", s.CustomerLabel, s.CustomerID, s.Name, s.EntryID, s.Status))
	}

	b.WriteString("\nThe generator is deterministic (fixed seed/anchor) and keeps these labels\n")
	b.WriteString("(and therefore these UUIDs) unchanged across regenerations.\n")
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
		// ids holds the transaction *labels* (e.g. "demo-txn-0000001"),
		// sorted lexicographically — meaningful since they're zero-padded
		// sequential numbers; the UUIDs derived from them below are not in
		// any particular order, so the label sort must happen first.
		ids := txnsByCustomer[custID]
		sort.Strings(ids)
		row := storyIDRow{
			Story:         i + 1,
			CustomerLabel: custID,
			CustomerID:    uuidFor(custID),
			Name:          customerDisplayName(byID[custID]),
			Purpose:       storyPurposes[i],
			TxnCount:      len(ids),
		}
		if len(ids) > 0 {
			row.FirstTxnID = uuidFor(ids[0])
			row.LastTxnID = uuidFor(ids[len(ids)-1])
		}
		for _, a := range storyAlertsByCustomer[custID] {
			row.AlertIDs = append(row.AlertIDs, uuidFor(a.ID))
		}
		if i < len(cases) {
			row.CaseID = uuidFor(cases[i].ID)
		}
		rows = append(rows, row)
	}

	var screens []screeningIDRow
	for _, r := range screeningResults {
		screens = append(screens, screeningIDRow{
			CustomerLabel: r.CustomerID,
			CustomerID:    uuidFor(r.CustomerID),
			Name:          customerDisplayName(byID[r.CustomerID]),
			EntryID:       r.EntryID,
			Status:        string(r.Status),
		})
	}

	return storyIDsInput{Anchor: anchorStr, Seed: seed, Stories: rows, Screens: screens}
}
