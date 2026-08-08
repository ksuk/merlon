package store

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// The memory store matches transaction_ids in Go and PostgreSQL matches it in
// SQL. Two implementations of one filter is how a queue and its production
// backend come to disagree, so this asserts the PostgreSQL side answers the
// same question -- including the case that matters most, an id that matches
// nothing returning nothing rather than everything.
func TestPostgresQueueFiltersByTransactionID(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	var customerID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
		 VALUES ($1, 'individual', 'JP', '{}', '{}') RETURNING id`, "txn-filter-"+newTestUUID(),
	).Scan(&customerID); err != nil {
		t.Fatalf("create customer: %v", err)
	}

	created := time.Date(2099, 8, 1, 0, 0, 0, 0, time.UTC)
	wantedTxn := newTestUUID()
	otherTxn := newTestUUID()
	matchingAlert := newTestUUID()
	unrelatedAlert := newTestUUID()
	prefix := newTestUUID()
	linkedCase := "txn-filter-" + prefix + "-linked"
	unlinkedCase := "txn-filter-" + prefix + "-unlinked"

	for _, row := range []struct {
		alertID string
		txnID   string
	}{{matchingAlert, wantedTxn}, {unrelatedAlert, otherTxn}} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO alerts (id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, created_at, updated_at)
			 VALUES ($1, $2, 'txn_filter_test', 'high', 'open', 1, '', ARRAY[$3]::uuid[], $4, $4, $4)`,
			row.alertID, customerID, row.txnID, created); err != nil {
			t.Fatalf("create alert: %v", err)
		}
	}
	for _, row := range []struct {
		caseID  string
		alertID string
	}{{linkedCase, matchingAlert}, {unlinkedCase, unrelatedAlert}} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO cases (id, customer_id, alert_ids, status, priority, assigned_to, summary, created_at, updated_at)
			 VALUES ($1, $2, ARRAY[$3]::text[], 'investigating', 'high', '', 'txn filter test', $4, $4)`,
			row.caseID, customerID, row.alertID, created); err != nil {
			t.Fatalf("create case: %v", err)
		}
	}
	t.Cleanup(func() {
		bg := context.Background()
		pool.Exec(bg, `DELETE FROM cases WHERE id = ANY($1)`, []string{linkedCase, unlinkedCase})
		pool.Exec(bg, `DELETE FROM alerts WHERE id = ANY($1::uuid[])`, []string{matchingAlert, unrelatedAlert})
		pool.Exec(bg, `DELETE FROM customers WHERE id = $1`, customerID)
	})

	alertRepo := NewPgAlertRepo(pool)
	alerts, err := alertRepo.ListQueue(ctx, domain.AlertQueueFilter{TransactionID: wantedTxn, AsOf: time.Now().UTC()}, 100, 0)
	if err != nil {
		t.Fatalf("ListQueue by transaction: %v", err)
	}
	if len(alerts) != 1 || alerts[0].ID != matchingAlert {
		t.Fatalf("alerts for transaction = %v, want only %s", alertIDsOf(alerts), matchingAlert)
	}

	// The keyset variant shares the builder; assert it agrees rather than
	// trusting that it still does.
	cursored, err := alertRepo.ListQueueCursor(ctx, domain.AlertQueueFilter{TransactionID: wantedTxn, AsOf: time.Now().UTC()}, 100, nil)
	if err != nil {
		t.Fatalf("ListQueueCursor by transaction: %v", err)
	}
	if len(cursored) != 1 || cursored[0].ID != matchingAlert {
		t.Fatalf("cursored alerts for transaction = %v, want only %s", alertIDsOf(cursored), matchingAlert)
	}

	unknown, err := alertRepo.ListQueue(ctx, domain.AlertQueueFilter{TransactionID: newTestUUID(), AsOf: time.Now().UTC()}, 100, 0)
	if err != nil {
		t.Fatalf("ListQueue by unknown transaction: %v", err)
	}
	if len(unknown) != 0 {
		t.Fatalf("alerts for an unknown transaction = %v, want none", alertIDsOf(unknown))
	}

	caseRepo := NewPgCaseRepo(pool)
	cases, err := caseRepo.ListQueue(ctx, domain.CaseQueueFilter{AlertIDs: []string{matchingAlert}, AsOf: time.Now().UTC()}, 100, 0)
	if err != nil {
		t.Fatalf("ListQueue by alert ids: %v", err)
	}
	if len(cases) != 1 || cases[0].ID != linkedCase {
		t.Fatalf("cases for alert = %v, want only %s", caseIDsOf(cases), linkedCase)
	}

	cursoredCases, err := caseRepo.ListQueueCursor(ctx, domain.CaseQueueFilter{AlertIDs: []string{matchingAlert}, AsOf: time.Now().UTC()}, 100, nil)
	if err != nil {
		t.Fatalf("ListQueueCursor by alert ids: %v", err)
	}
	if len(cursoredCases) != 1 || cursoredCases[0].ID != linkedCase {
		t.Fatalf("cursored cases for alert = %v, want only %s", caseIDsOf(cursoredCases), linkedCase)
	}
}

func alertIDsOf(alerts []domain.Alert) []string {
	ids := make([]string, 0, len(alerts))
	for _, a := range alerts {
		ids = append(ids, a.ID)
	}
	return ids
}

func caseIDsOf(cases []domain.Case) []string {
	ids := make([]string, 0, len(cases))
	for _, c := range cases {
		ids = append(ids, c.ID)
	}
	return ids
}
