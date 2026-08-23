package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/adapter"
	"github.com/ksuk/merlon/api/internal/domain"
	enginecontract "github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/engine/native"
	"github.com/ksuk/merlon/api/internal/store"
)

func TestExternalCoreToCaseIngestionSeam(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/customers":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"id": "C-1", "name": "Example", "country": "JP", "type": "individual"}}})
		case "/transactions":
			items := []any{}
			for n := 1; n <= 3; n++ {
				items = append(items, map[string]any{"id": "T-" + string(rune('0'+n)), "customer_id": "C-1", "amount": "400000", "currency": "JPY", "direction": "inbound", "type": "deposit", "executed_at": "2026-01-01T00:0" + string(rune('0'+n)) + ":00Z"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
		default:
			http.NotFound(w, r)
		}
	}))
	defer core.Close()

	cfg := &adapter.AdapterConfig{Type: "rest", BaseURL: core.URL, Auth: adapter.AuthConfig{Type: "none"}, Endpoints: map[string]adapter.EndpointConfig{
		"fetch_customers":    {Method: http.MethodGet, Path: "/customers", ResponseRoot: "$.items", FieldMapping: map[string]string{"external_id": "$.id", "name": "$.name", "country": "$.country", "customer_type": "$.type"}},
		"fetch_transactions": {Method: http.MethodGet, Path: "/transactions", ResponseRoot: "$.items", FieldMapping: map[string]string{"external_id": "$.id", "customer_external_id": "$.customer_id", "amount": "$.amount", "currency": "$.currency", "direction": "$.direction", "type": "$.type", "executed_at": "$.executed_at"}},
	}, Sync: adapter.SyncConfig{PageSize: 500, InitialLookback: time.Hour}}
	rest, err := adapter.NewRESTAdapter(cfg, adapter.SecurityConfig{})
	if err != nil {
		t.Fatal(err)
	}
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	checkpoints := adapter.NewMemoryCheckpointRepository()
	run, err := (&adapter.SyncService{AdapterID: "fixture-core", Config: cfg, Adapter: rest, Deps: adapter.SyncDependencies{Customers: customers, Transactions: transactions, Checkpoints: checkpoints}}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if run.CustomerAccepted != 1 || run.TransactionAccepted != 3 {
		t.Fatalf("sync run=%+v", run)
	}
	loaded, err := customers.GetByExternalID(context.Background(), "C-1")
	if err != nil {
		t.Fatal(err)
	}
	txns, err := transactions.ListByCustomer(context.Background(), loaded.ID, 20, 0)
	if err != nil || len(txns) != 3 {
		t.Fatalf("transactions=%d err=%v", len(txns), err)
	}

	engine := fixtureEngine(t)
	score, err := engine.ScoreCustomer(context.Background(), loaded, "")
	if err != nil {
		t.Fatal(err)
	}
	if score.Tier != domain.RiskTierHigh {
		t.Fatalf("score=%+v, want high tier for TM threshold path", score)
	}
	alerts, err := engine.Evaluate(context.Background(), enginecontract.MonitoringRequest{CustomerID: loaded.ID, CustomerType: loaded.CustomerType, RiskTier: score.Tier, Transactions: txns, Mode: enginecontract.EvaluationModeRealtime, EvaluatedAt: time.Now().UTC(), ConfigDigests: map[string]string{"adapter": run.AdapterDigest}})
	if err != nil || len(alerts) == 0 {
		t.Fatalf("alerts=%+v err=%v", alerts, err)
	}
	alertsRepo := store.NewMemoryAlertRepo()
	alert := alerts[0]
	alert.ID = "000000000000000000000000000000a1"
	if _, _, err := alertsRepo.CreateIfNotDuplicate(context.Background(), &alert); err != nil {
		t.Fatal(err)
	}
	cases := store.NewMemoryCaseRepo()
	c := &domain.Case{ID: "000000000000000000000000000000c1", CustomerID: loaded.ID, AlertIDs: []string{alert.ID}, Status: domain.CaseStatusNew, Priority: domain.CasePriorityHigh, Summary: "fixture ingestion alert", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := cases.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if got, err := cases.ListByCustomer(context.Background(), loaded.ID); err != nil || len(got) != 1 || got[0].AlertIDs[0] != alert.ID {
		t.Fatalf("cases=%+v err=%v", got, err)
	}
	if alert.Provenance == nil || alert.Provenance.EngineVersion == "" {
		t.Fatalf("alert provenance=%+v", alert.Provenance)
	}
}

func fixtureEngine(t *testing.T) *native.Engine {
	t.Helper()
	dir := t.TempDir()
	cdd := filepath.Join(dir, "cdd.yaml")
	tm := filepath.Join(dir, "tm")
	if err := os.Mkdir(tm, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cdd, []byte("schema_version: cdd_weight_v1\npreset_id: integration\nrisk_factors:\n  customer_type:\n    weight: 1\n    values: {individual: 4}\ntier_thresholds: {LOW: {max: 2}, MEDIUM: {min: 2, max: 3.5}, HIGH: {min: 3.5}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmYAML := `schema_version: "2.1"
scenario_id: tm_structuring_basic
detector: structuring
name: integration
description: integration
type: aggregation
conditions:
  transaction_type: [deposit]
  aggregation: {field: amount, function: sum, period: 24h, group_by: customer_id}
  threshold: {by_customer_type: {individual: {by_risk_tier: {HIGH: 500000}}}}
  additional: {min_transaction_count: 3, individual_below: 500000}
evaluation_mode: both
severity: HIGH
`
	if err := os.WriteFile(filepath.Join(tm, "structuring.yaml"), []byte(tmYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := native.New(cdd, tm, filepath.Join(dir, "missing-lists"), "")
	if err != nil {
		t.Fatal(err)
	}
	return engine
}
