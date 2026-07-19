package demogen

import (
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// auditSeq assigns sequential int64 IDs, mirroring idSeq for the one JSON
// field (AuditEntry.ID) that isn't a string.
type auditSeq struct{ n int64 }

// sampleAuditCustomers picks a deterministic representative subset of the
// population for the customer-creation/scoring audit_logs entries (A9 needs
// "200件強" total, not one entry per customer): every story and screening
// customer plus a fixed-stride sample of the rest.
func sampleAuditCustomers(all []domain.Customer) []domain.Customer {
	var out []domain.Customer
	const strideSampleCount = 20
	stride := len(all) / strideSampleCount
	if stride < 1 {
		stride = 1
	}
	for i, c := range all {
		switch {
		case strings.HasPrefix(c.ID, "demo-story-"), strings.HasPrefix(c.ID, "demo-screening-"):
			out = append(out, c)
		case i%stride == 0:
			out = append(out, c)
		}
	}
	return out
}

func (s *auditSeq) next() int64 {
	s.n++
	return s.n
}

// buildAuditLogs assembles A9's 200-ish time-coherent audit_logs: rule
// registration, a representative sample of customer creation/scoring
// events, case lifecycle events (creation, notes, status changes),
// screening_results creation/review, and story 5's freeze action (A6: "発火後
// status=frozen + audit_logsに凍結操作"). Every timestamp is derived from
// the anchor/story data already generated, never time.Now().
func buildAuditLogs(
	anchor time.Time,
	rules []domain.RuleDefinition,
	sampleCustomers []domain.Customer,
	cases []domain.Case,
	notes []caseNoteRecord,
	screeningResults []domain.ScreeningResultRecord,
) []domain.AuditEntry {
	seq := &auditSeq{}
	var out []domain.AuditEntry

	add := func(userID, action, resourceType, resourceID string, at time.Time) {
		out = append(out, domain.AuditEntry{
			ID: seq.next(), UserID: userID, Action: action, ResourceType: resourceType, ResourceID: resourceID,
			Details:   map[string]string{"synthetic": "true"},
			IPAddress: "192.0.2.10", UserAgent: "merlon-demogen",
			CreatedAt: at,
		})
	}

	// Rule registration (A9: "rule_definitions(...) registered状態").
	for _, r := range rules {
		add("m.sato", "register", "rule_definitions", r.ID, r.CreatedAt)
	}

	// Representative customer creation + scoring sample.
	for _, c := range sampleCustomers {
		add("demo-seed", "create", "customers", c.ID, c.CreatedAt)
		scoredAt := c.CreatedAt.Add(1 * time.Hour)
		if c.LastScoredAt != nil {
			scoredAt = *c.LastScoredAt
		}
		add("demo-seed", "score", "customers", c.ID, scoredAt)
	}

	// Case lifecycle.
	for _, c := range cases {
		add(c.AssignedTo, "create", "cases", c.ID, c.CreatedAt)
	}
	for _, n := range notes {
		add(n.Author, "add_note", "cases", n.CaseID, n.CreatedAt)
	}
	for _, c := range cases {
		if c.Status == domain.CaseStatusClosed && c.ClosedAt != nil {
			add(c.AssignedTo, "close", "cases", c.ID, *c.ClosedAt)
		} else if c.Status == domain.CaseStatusInvestigating {
			add(c.AssignedTo, "status_change", "cases", c.ID, c.UpdatedAt)
		}
	}

	// Screening results creation + review.
	for _, r := range screeningResults {
		add("demo-seed", "screen", "customers", r.CustomerID, r.ScreenedAt)
		if r.ReviewedAt != nil {
			add(r.ReviewedBy, "review", "screening_results", r.ID, *r.ReviewedAt)
		}
	}

	// Story 5: dormant reactivation alert -> account frozen (A6).
	add("k.watanabe", "freeze", "customers", "demo-story-05", anchor.AddDate(0, 0, -419))

	return out
}
