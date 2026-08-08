package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

// #71 requires the cohort to be previewed *before* execution, warning or
// blocking on an empty one. The only preview that existed was computed inside
// POST /backtests -- after the job was already created -- and it counted
// transactions only when the caller listed customer ids explicitly, so the
// filter and all-customer cohorts got no transaction count at all. An operator
// could start a comparison over a cohort with no transactions and learn about
// it from an empty result.

type backtestPreviewFixture struct {
	server    *Server
	withTxns  string
	withoutTx string
}

func newBacktestPreviewFixture(t *testing.T) *backtestPreviewFixture {
	t.Helper()
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	rules := store.NewMemoryRuleRepo()
	now := time.Now().UTC()

	for _, id := range []string{"baseline_rules", "candidate_rules"} {
		if err := rules.Create(ctx, &domain.RuleDefinition{
			ID: id, Name: id, Type: domain.RuleTypeTMScenario, Version: 1, IsActive: true,
			Definition: json.RawMessage(`{"scenarios":[]}`), CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	withTxns := "00000000000000000000000000000a01"
	withoutTx := "00000000000000000000000000000a02"
	for _, c := range []struct {
		id   string
		tier domain.RiskTier
	}{{withTxns, domain.RiskTierHigh}, {withoutTx, domain.RiskTierLow}} {
		tier := c.tier
		if err := customers.Create(ctx, &domain.Customer{
			ID: c.id, ExternalID: "bt-" + c.id[30:], CustomerType: domain.CustomerTypeIndividual,
			CountryCode: "JP", Status: domain.CustomerStatusActive, RiskTier: &tier,
			CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		if err := transactions.Create(ctx, &domain.Transaction{
			ID: generateID(), CustomerID: withTxns, Amount: 1000, Currency: "JPY",
			Direction: "inbound", ExecutedAt: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}

	s := New(":0", Deps{
		Customers: customers, Transactions: transactions, Rules: rules,
		Alerts: store.NewMemoryAlertRepo(), Cases: store.NewMemoryCaseRepo(),
		Audit: store.NewMemoryAuditRepo(), Wave3: store.NewMemoryWave3Repo(),
		BacktestJobs: store.NewMemoryBacktestJobRepo(),
		Backtest:     &engine.MockBacktestEngine{},
	})
	return &backtestPreviewFixture{server: s, withTxns: withTxns, withoutTx: withoutTx}
}

type cohortPreviewResponse struct {
	CustomerCount     int      `json:"customer_count"`
	TransactionCount  int      `json:"transaction_count"`
	SampleCustomerIDs []string `json:"sample_customer_ids"`
	Empty             bool     `json:"empty"`
	Warnings          []string `json:"warnings"`
}

func preview(t *testing.T, s *Server, body string) (*httptest.ResponseRecorder, cohortPreviewResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backtests/preview", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var out cohortPreviewResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode preview: %v (body %s)", err, rec.Body.String())
		}
	}
	return rec, out
}

func TestBacktestPreviewCountsSelectedCohort(t *testing.T) {
	f := newBacktestPreviewFixture(t)

	rec, out := preview(t, f.server, `{"customer_ids":["`+f.withTxns+`"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if out.CustomerCount != 1 {
		t.Errorf("customer_count = %d, want 1", out.CustomerCount)
	}
	if out.TransactionCount != 3 {
		t.Errorf("transaction_count = %d, want 3", out.TransactionCount)
	}
	if out.Empty {
		t.Error("empty = true for a cohort with transactions")
	}
}

func TestBacktestPreviewCountsTransactionsForFilterCohort(t *testing.T) {
	// The defect: a cohort selected by filter (or all customers) got no
	// transaction count at all, so the preview could not distinguish
	// "no transactions" from "not counted".
	f := newBacktestPreviewFixture(t)

	rec, out := preview(t, f.server, `{"customer_filter":{"risk_tier":"high"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if out.CustomerCount != 1 {
		t.Errorf("customer_count = %d, want 1 (only the high-tier customer)", out.CustomerCount)
	}
	if out.TransactionCount != 3 {
		t.Errorf("transaction_count = %d, want 3: a filter cohort must be counted too", out.TransactionCount)
	}
}

func TestBacktestPreviewWarnsOnEmptyTransactionCohort(t *testing.T) {
	f := newBacktestPreviewFixture(t)

	rec, out := preview(t, f.server, `{"customer_ids":["`+f.withoutTx+`"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if out.CustomerCount != 1 {
		t.Errorf("customer_count = %d, want 1", out.CustomerCount)
	}
	if out.TransactionCount != 0 {
		t.Errorf("transaction_count = %d, want 0", out.TransactionCount)
	}
	if !out.Empty {
		t.Error("empty = false for a cohort whose customers have no transactions; the operator has nothing to compare")
	}
	if len(out.Warnings) == 0 {
		t.Error("no warning for an empty cohort")
	}
}

func TestBacktestPreviewWarnsOnEmptyCustomerCohort(t *testing.T) {
	f := newBacktestPreviewFixture(t)

	rec, out := preview(t, f.server, `{"customer_filter":{"country_code":"ZZ"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if out.CustomerCount != 0 || !out.Empty || len(out.Warnings) == 0 {
		t.Errorf("customer_count=%d empty=%v warnings=%v, want an explicitly empty cohort", out.CustomerCount, out.Empty, out.Warnings)
	}
}

func TestCreateBacktestCountsTransactionsForEveryCohortMode(t *testing.T) {
	// The stored cohort_preview must carry the same numbers the preview
	// endpoint reports, or the record of what was run disagrees with what the
	// operator was shown.
	f := newBacktestPreviewFixture(t)

	body := `{"from":"2020-01-01T00:00:00Z","to":"2099-01-01T00:00:00Z","customer_filter":{"risk_tier":"high"},` +
		`"baseline_rule_set_id":"baseline_rules","candidate_rule_set_id":"candidate_rules","rationale":"cohort parity"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backtests", strings.NewReader(body))
	rec := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body: %s", rec.Code, rec.Body.String())
	}
	var job domain.BacktestJob
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	metadataRepo, ok := f.server.wave3.(domain.BacktestMetadataRepository)
	if !ok {
		t.Fatal("metadata repository not wired")
	}
	metadata, err := metadataRepo.GetBacktestMetadata(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	count, ok := metadata.CohortPreview["transaction_count"]
	if !ok {
		t.Fatalf("cohort_preview has no transaction_count for a filter cohort: %v", metadata.CohortPreview)
	}
	if toInt(count) != 3 {
		t.Errorf("cohort_preview.transaction_count = %v, want 3", count)
	}
	if toInt(metadata.CohortPreview["count"]) != 1 {
		t.Errorf("cohort_preview.count = %v, want 1", metadata.CohortPreview["count"])
	}
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return -1
}
