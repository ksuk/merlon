package store

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestPostgresRiskSortMatchesDomainRanksAndCursor(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	var customerID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
		 VALUES ($1, 'individual', 'JP', '{}', '{}') RETURNING id`, "risk-sort-"+newTestUUID(),
	).Scan(&customerID); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	created := time.Date(2099, 7, 1, 0, 0, 0, 0, time.UTC)
	severities := []domain.AlertSeverity{
		domain.AlertSeverityLow,
		domain.AlertSeverityMedium,
		domain.AlertSeverityHigh,
		domain.AlertSeverityCritical,
	}
	priorities := []domain.CasePriority{
		domain.CasePriorityLow,
		domain.CasePriorityMedium,
		domain.CasePriorityHigh,
		domain.CasePriorityCritical,
	}
	alertIDs := make([]string, len(severities))
	caseIDs := make([]string, len(priorities))
	prefix := newTestUUID()
	for i := range severities {
		alertIDs[i] = newTestUUID()
		caseIDs[i] = "risk-sort-" + prefix + "-" + string(rune('a'+i))
		if _, err := pool.Exec(ctx,
			`INSERT INTO alerts (id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, created_at, updated_at)
			 VALUES ($1, $2, 'risk_sort_test', $3, 'open', 1, '', '{}', $4, $4, $4)`,
			alertIDs[i], customerID, string(severities[i]), created); err != nil {
			t.Fatalf("create %s alert: %v", severities[i], err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO cases (id, customer_id, alert_ids, status, priority, assigned_to, summary, created_at, updated_at)
			 VALUES ($1, $2, '{}', 'investigating', $3, '', 'risk sort test', $4, $4)`,
			caseIDs[i], customerID, string(priorities[i]), created); err != nil {
			t.Fatalf("create %s case: %v", priorities[i], err)
		}
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM cases WHERE id = ANY($1)`, caseIDs)
		pool.Exec(context.Background(), `DELETE FROM alerts WHERE id = ANY($1::uuid[])`, alertIDs)
		pool.Exec(context.Background(), `DELETE FROM customers WHERE id = $1`, customerID)
	})

	alertRepo := NewPgAlertRepo(pool)
	alertPage, err := alertRepo.ListOpenByRisk(ctx, 100000, 0)
	if err != nil {
		t.Fatalf("ListOpenByRisk alerts: %v", err)
	}
	assertRelativeAlertOrder(t, alertPage, []string{alertIDs[3], alertIDs[2], alertIDs[1], alertIDs[0]})
	alertAfter := &domain.Cursor{Sort: "risk", Rank: domain.AlertSeverityRank(domain.AlertSeverityHigh), CreatedAt: created, ID: alertIDs[2]}
	alertRest, err := alertRepo.ListOpenByRiskCursor(ctx, 100000, alertAfter)
	if err != nil {
		t.Fatalf("ListOpenByRiskCursor alerts: %v", err)
	}
	assertRelativeAlertOrder(t, alertRest, []string{alertIDs[1], alertIDs[0]})

	caseRepo := NewPgCaseRepo(pool)
	casePage, err := caseRepo.ListOpenByRisk(ctx, 100000, 0)
	if err != nil {
		t.Fatalf("ListOpenByRisk cases: %v", err)
	}
	assertRelativeCaseOrder(t, casePage, []string{caseIDs[3], caseIDs[2], caseIDs[1], caseIDs[0]})
	caseAfter := &domain.Cursor{Sort: "risk", Rank: domain.CasePriorityRank(domain.CasePriorityHigh), CreatedAt: created, ID: caseIDs[2]}
	caseRest, err := caseRepo.ListOpenByRiskCursor(ctx, 100000, caseAfter)
	if err != nil {
		t.Fatalf("ListOpenByRiskCursor cases: %v", err)
	}
	assertRelativeCaseOrder(t, caseRest, []string{caseIDs[1], caseIDs[0]})
}

func assertRelativeAlertOrder(t *testing.T, got []domain.Alert, want []string) {
	t.Helper()
	positions := make(map[string]int, len(want))
	for i, alert := range got {
		positions[alert.ID] = i
	}
	assertIncreasingPositions(t, positions, want)
}

func assertRelativeCaseOrder(t *testing.T, got []domain.Case, want []string) {
	t.Helper()
	positions := make(map[string]int, len(want))
	for i, c := range got {
		positions[c.ID] = i
	}
	assertIncreasingPositions(t, positions, want)
}

func assertIncreasingPositions(t *testing.T, positions map[string]int, want []string) {
	t.Helper()
	previous := -1
	for _, id := range want {
		position, ok := positions[id]
		if !ok {
			t.Fatalf("seeded record %s missing from risk page", id)
		}
		if position <= previous {
			t.Fatalf("risk order positions for %v = %v", want, positions)
		}
		previous = position
	}
}
