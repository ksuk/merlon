package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func writeCDDReviewPolicy(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cdd-review.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const validCDDReviewYAML = `schema_version: cdd_review_policy_v1
policy_version: test-v1
intervals: {high: 365, medium: 730, low: 1095}
anchor_precedence: [last_completed_review, last_scored_at, customer_created_at]
tier_increase_early: true
completion:
  requires_rationale: true
  roles: [analyst, admin]
grace_days: 30
cold_start_spread: {high: 30, medium: 90, low: 180}
`

func TestLoadCDDReviewRejectsUnknownAndInvalidDocuments(t *testing.T) {
	if _, err := LoadCDDReviewPolicy(writeCDDReviewPolicy(t, validCDDReviewYAML+"unknown: true\n")); err == nil || !strings.Contains(err.Error(), "field unknown") {
		t.Fatalf("unknown field error = %v", err)
	}
	bad := strings.Replace(validCDDReviewYAML, "high: 365", "high: 0", 1)
	if _, err := LoadCDDReviewPolicy(writeCDDReviewPolicy(t, bad)); err == nil || !strings.Contains(err.Error(), "intervals.high") {
		t.Fatalf("invalid interval error = %v", err)
	}
	missing := strings.Replace(validCDDReviewYAML, "low: 1095", "", 1)
	if _, err := LoadCDDReviewPolicy(writeCDDReviewPolicy(t, missing)); err == nil || !strings.Contains(err.Error(), "intervals.low") {
		t.Fatalf("missing interval error = %v", err)
	}
}

func TestCDDReviewScheduleAnchorIncreaseGraceAndDigest(t *testing.T) {
	p, err := LoadCDDReviewPolicy(writeCDDReviewPolicy(t, validCDDReviewYAML))
	if err != nil {
		t.Fatal(err)
	}
	completed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	scored := completed.AddDate(0, 0, 10)
	created := completed.AddDate(-1, 0, 0)
	asOf := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	schedule := p.Schedule(CDDReviewInput{CustomerID: "customer-1", Tier: domain.RiskTierMedium, PreviousTier: domain.RiskTierLow, LastCompletedReview: &completed, LastScoredAt: &scored, CustomerCreatedAt: created, AsOf: asOf})
	if schedule.Anchor != AnchorLastCompletedReview || !schedule.AnchorAt.Equal(completed) {
		t.Fatalf("anchor = %#v, want completed review", schedule)
	}
	if !schedule.TierIncreased || !schedule.NextReviewAt.Equal(asOf) {
		t.Fatalf("tier increase schedule = %#v, want immediate at as_of", schedule)
	}
	if !schedule.GraceUntil.Equal(asOf.AddDate(0, 0, 30)) || schedule.PolicyDigest == "" {
		t.Fatalf("grace/digest = %#v", schedule)
	}
	if schedule.PolicyDigest != p.Schedule(CDDReviewInput{CustomerID: "customer-1", Tier: domain.RiskTierMedium, PreviousTier: domain.RiskTierLow, LastCompletedReview: &completed, LastScoredAt: &scored, CustomerCreatedAt: created, AsOf: asOf}).PolicyDigest {
		t.Fatal("same policy/input produced different digest")
	}
}

func TestCDDReviewColdStartIsDeterministicAndUnscoredIsHigh(t *testing.T) {
	p := DefaultCDDReviewPolicy()
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	input := CDDReviewInput{CustomerID: "cold-start-customer", CustomerCreatedAt: created}
	a := p.Schedule(input)
	b := p.Schedule(input)
	if a.Tier != domain.RiskTierHigh || a.Anchor != AnchorCustomerCreatedAt || a.ColdStartOffset != b.ColdStartOffset || a.ColdStartOffset < 0 || a.ColdStartOffset >= 30 {
		t.Fatalf("cold-start schedules = %#v / %#v", a, b)
	}
	if err := p.ValidateCompletion("viewer", "rationale"); err == nil {
		t.Fatal("viewer unexpectedly allowed to complete review")
	}
	if err := p.ValidateCompletion("analyst", ""); err == nil {
		t.Fatal("empty rationale unexpectedly accepted")
	}
	if err := p.ValidateCompletion("analyst", "reviewed"); err != nil {
		t.Fatal(err)
	}
}
