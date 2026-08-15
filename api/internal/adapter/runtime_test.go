package adapter_test

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/adapter"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

type fakeRuntimeAdapter struct {
	customerPages    []*adapter.CustomerPage
	transactionPages []*adapter.TransactionPage
	customerCalls    int
	transactionCalls int
}

func (f *fakeRuntimeAdapter) FetchCustomer(context.Context, string) (*adapter.CustomerData, error) {
	return nil, nil
}
func (f *fakeRuntimeAdapter) FetchTransactions(context.Context, map[string]string) ([]adapter.TransactionData, error) {
	return nil, nil
}
func (f *fakeRuntimeAdapter) DryRun(context.Context) (*adapter.DryRunResult, error) {
	return &adapter.DryRunResult{ConfigValid: true}, nil
}
func (f *fakeRuntimeAdapter) FetchCustomersPage(context.Context, map[string]string) (*adapter.CustomerPage, error) {
	page := f.customerPages[f.customerCalls]
	f.customerCalls++
	return page, nil
}
func (f *fakeRuntimeAdapter) FetchTransactionsPage(context.Context, map[string]string) (*adapter.TransactionPage, error) {
	page := f.transactionPages[f.transactionCalls]
	f.transactionCalls++
	return page, nil
}

func runtimeConfig() *adapter.AdapterConfig {
	return &adapter.AdapterConfig{Type: "rest", BaseURL: "https://core.example", Endpoints: map[string]adapter.EndpointConfig{
		"fetch_customers":    {Method: "GET", Path: "/customers", ResponseRoot: "$.items", FieldMapping: map[string]string{"external_id": "$.id", "name": "$.name", "country": "$.country", "customer_type": "$.type"}},
		"fetch_transactions": {Method: "GET", Path: "/transactions", ResponseRoot: "$.items", FieldMapping: map[string]string{"external_id": "$.id", "amount": "$.amount", "currency": "$.currency", "type": "$.type", "customer_external_id": "$.customer_id", "direction": "$.direction", "executed_at": "$.executed_at"}},
	}, Sync: adapter.SyncConfig{Interval: time.Minute, PageSize: 2, InitialLookback: time.Hour}}
}

func TestSyncServiceProcessesCustomersBeforeTransactionsAndPersistsCheckpoint(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	fake := &fakeRuntimeAdapter{
		customerPages:    []*adapter.CustomerPage{{Customers: []adapter.CustomerData{{ExternalID: "C-1", Name: "Example", Country: "JP", CustomerType: "individual"}}}},
		transactionPages: []*adapter.TransactionPage{{Transactions: []adapter.TransactionData{{ExternalID: "T-1", CustomerExternalID: "C-1", Amount: "100", Currency: "JPY", Direction: "inbound", ExecutedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}}},
	}
	checkpoints := adapter.NewMemoryCheckpointRepository()
	run, err := (&adapter.SyncService{AdapterID: "core", Config: runtimeConfig(), Adapter: fake, Deps: adapter.SyncDependencies{Customers: customers, Transactions: transactions, Checkpoints: checkpoints}, Now: func() time.Time { return time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC) }}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if run.CustomerAccepted != 1 || run.TransactionAccepted != 1 || run.Checkpoint.AdapterDigest == "" {
		t.Fatalf("run=%+v", run)
	}
	if fake.customerCalls != 1 || fake.transactionCalls != 1 {
		t.Fatalf("calls customer=%d transaction=%d", fake.customerCalls, fake.transactionCalls)
	}
	checkpoint, _ := checkpoints.Get(context.Background(), "core")
	if checkpoint.AdapterDigest == "" {
		t.Fatal("checkpoint digest missing")
	}
}

func TestSyncServiceKeepsTransactionPendingWhenCustomerIsMissing(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	checkpoints := adapter.NewMemoryCheckpointRepository()
	if err := checkpoints.Save(context.Background(), &adapter.SyncCheckpoint{AdapterID: "rest", TransactionCursor: "before", TransactionWatermark: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeRuntimeAdapter{customerPages: []*adapter.CustomerPage{{}}, transactionPages: []*adapter.TransactionPage{{NextCursor: "after", Watermark: "2026-01-02T00:00:00Z", Transactions: []adapter.TransactionData{{ExternalID: "T-1", CustomerExternalID: "missing", Amount: "1", Direction: string(domain.DirectionInbound)}}}}}
	run, err := (&adapter.SyncService{Config: runtimeConfig(), Adapter: fake, Deps: adapter.SyncDependencies{Customers: customers, Transactions: transactions, Checkpoints: checkpoints}}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if run.WaitingDependency != 1 || len(run.Outcomes) != 1 || run.Outcomes[0].Status != "waiting_dependency" {
		t.Fatalf("run=%+v", run)
	}
	checkpoint, _ := checkpoints.Get(context.Background(), "rest")
	if checkpoint.TransactionCursor != "before" || checkpoint.TransactionWatermark != "2026-01-01T00:00:00Z" {
		t.Fatalf("waiting dependency advanced checkpoint: %#v", checkpoint)
	}
}

func TestSyncServiceLeasePreventsConcurrentRun(t *testing.T) {
	checkpoints := adapter.NewMemoryCheckpointRepository()
	now := time.Now().UTC()
	ok, err := checkpoints.Acquire(context.Background(), "core", "worker-a", now, time.Minute)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := checkpoints.Acquire(context.Background(), "core", "worker-b", now, time.Minute); err != nil || ok {
		t.Fatalf("second lease ok=%v err=%v", ok, err)
	}
}

func TestSyncServicePreservesWatermarksWhenTerminalPageOmitsThem(t *testing.T) {
	checkpoints := adapter.NewMemoryCheckpointRepository()
	previousCustomer := "2026-01-01T00:00:00Z"
	previousTransaction := "2026-01-02T00:00:00Z"
	if err := checkpoints.Save(context.Background(), &adapter.SyncCheckpoint{AdapterID: "core", CustomerWatermark: previousCustomer, TransactionWatermark: previousTransaction}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeRuntimeAdapter{customerPages: []*adapter.CustomerPage{{Watermark: ""}}, transactionPages: []*adapter.TransactionPage{{Watermark: ""}}}
	_, err := (&adapter.SyncService{AdapterID: "core", Config: runtimeConfig(), Adapter: fake, Deps: adapter.SyncDependencies{Customers: store.NewMemoryCustomerRepo(), Transactions: store.NewMemoryTransactionRepo(), Checkpoints: checkpoints}}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, _ := checkpoints.Get(context.Background(), "core")
	if checkpoint.CustomerWatermark != previousCustomer || checkpoint.TransactionWatermark != previousTransaction {
		t.Fatalf("watermarks regressed: %#v", checkpoint)
	}
}
