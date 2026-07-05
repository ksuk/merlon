package retention

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/store"
)

func newAnonymizeFixtures(t *testing.T, lastTransactionAt time.Time) (domain.CustomerRepository, domain.TransactionRepository, domain.RetentionRepository, domain.AuditRepository, string) {
	t.Helper()
	ctx := context.Background()

	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	retention := store.NewMemoryRetentionRepo()
	audit := store.NewMemoryAuditRepo()

	c := &domain.Customer{
		ID:           "cust-anon-1",
		ExternalID:   "EXT-ANON-1",
		CustomerType: domain.CustomerTypeIndividual,
		CountryCode:  "JP",
		Attributes: map[string]string{
			"name":       "Taro Yamada",
			"address":    "1-1 Chiyoda, Tokyo",
			"email":      "taro@example.com",
			"occupation": "engineer",
		},
		CreatedAt: lastTransactionAt,
		UpdatedAt: lastTransactionAt,
	}
	if err := customers.Create(ctx, c); err != nil {
		t.Fatalf("create customer: %v", err)
	}

	tx := &domain.Transaction{
		ID:         "tx-anon-1",
		CustomerID: c.ID,
		ExternalID: "TX-ANON-1",
		Amount:     1000,
		Currency:   "JPY",
		Direction:  domain.DirectionInbound,
		ExecutedAt: lastTransactionAt,
		CreatedAt:  lastTransactionAt,
	}
	if err := transactions.Create(ctx, tx); err != nil {
		t.Fatalf("create transaction: %v", err)
	}

	return customers, transactions, retention, audit, c.ID
}

// TestAnonymizeRejectsWithinStatutoryPeriod verifies an APPI deletion
// request for a customer whose last transaction is within the customer_data
// statutory retention period (2555 days / 7 years, audit.md §6) is rejected
// (RET-004: 保存義務期間内のデータは削除対象外).
func TestAnonymizeRejectsWithinStatutoryPeriod(t *testing.T) {
	lastTransactionAt := time.Now().AddDate(0, 0, -100) // well within 7 years
	customers, transactions, retention, audit, customerID := newAnonymizeFixtures(t, lastTransactionAt)

	err := Anonymize(context.Background(), customers, transactions, retention, audit, AnonymizeRequest{
		CustomerID:  customerID,
		Reason:      "APPI deletion request",
		RequestedBy: "compliance-officer",
	})

	if !errors.Is(err, ErrWithinStatutoryPeriod) {
		t.Fatalf("err = %v, want ErrWithinStatutoryPeriod", err)
	}

	got, getErr := customers.Get(context.Background(), customerID)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if got.AnonymizedAt != nil {
		t.Error("customer should not be anonymized while within statutory period")
	}
	if got.Attributes["name"] != "Taro Yamada" {
		t.Error("attributes should be untouched when rejected")
	}
}

// TestAnonymizeSucceedsAfterStatutoryPeriod verifies a customer whose last
// transaction predates the statutory retention period can be anonymized:
// direct-PII attributes fields (data-model.md §3.1) are replaced with a
// fixed placeholder and anonymized_at is set.
func TestAnonymizeSucceedsAfterStatutoryPeriod(t *testing.T) {
	lastTransactionAt := time.Now().AddDate(0, 0, -3000) // more than 2555 days ago
	customers, transactions, retention, audit, customerID := newAnonymizeFixtures(t, lastTransactionAt)

	err := Anonymize(context.Background(), customers, transactions, retention, audit, AnonymizeRequest{
		CustomerID:  customerID,
		Reason:      "APPI deletion request",
		RequestedBy: "compliance-officer",
	})
	if err != nil {
		t.Fatalf("Anonymize: %v", err)
	}

	got, err := customers.Get(context.Background(), customerID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AnonymizedAt == nil {
		t.Fatal("anonymized_at should be set")
	}
	if got.Attributes["name"] != anonymizedPlaceholder {
		t.Errorf("attributes[name] = %q, want %q", got.Attributes["name"], anonymizedPlaceholder)
	}
	if got.Attributes["address"] != anonymizedPlaceholder {
		t.Errorf("attributes[address] = %q, want %q", got.Attributes["address"], anonymizedPlaceholder)
	}
	if got.Attributes["email"] != anonymizedPlaceholder {
		t.Errorf("attributes[email] = %q, want %q", got.Attributes["email"], anonymizedPlaceholder)
	}
	// occupation is 準PII (data-model.md §3.1), retained for statistical use.
	if got.Attributes["occupation"] != "engineer" {
		t.Errorf("attributes[occupation] should be retained, got %q", got.Attributes["occupation"])
	}
}

// TestAnonymizeRecordsAuditLog verifies the anonymization operation itself
// is recorded in the audit log (data-model.md §3.7: APPI 削除要求自体も監査
// ログに記録する).
func TestAnonymizeRecordsAuditLog(t *testing.T) {
	lastTransactionAt := time.Now().AddDate(0, 0, -3000)
	customers, transactions, retention, audit, customerID := newAnonymizeFixtures(t, lastTransactionAt)

	req := AnonymizeRequest{
		CustomerID:  customerID,
		Reason:      "APPI deletion request",
		RequestedBy: "compliance-officer",
	}
	if err := Anonymize(context.Background(), customers, transactions, retention, audit, req); err != nil {
		t.Fatalf("Anonymize: %v", err)
	}

	entries, err := audit.List(context.Background(), "customer", customerID, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Action == "anonymize_customer" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an anonymize_customer audit entry, got %+v", entries)
	}
}
