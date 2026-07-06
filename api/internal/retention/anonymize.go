// Package retention implements the data retention lifecycle (audit.md
// RET-001〜004): the automatic purge framework (purge.go) and APPI
// individual-deletion anonymization (anonymize.go).
package retention

import (
	"context"
	"errors"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

// anonymizedPlaceholder replaces direct-PII attribute values (data-model.md
// §3.1: 氏名、住所、生年月日、電話番号、メール、口座情報、身分証番号).
const anonymizedPlaceholder = "[ANONYMIZED]"

// directPIIAttributeKeys are the customers.attributes JSONB keys classified
// as 直接PII (data-model.md §3.1). Keys not in this set (occupation,
// incorporation_country, beneficial_owners, etc.) are 準PII or AML risk
// attributes kept in plaintext for statistics/backtesting even after
// anonymization (audit.md §11: 匿名化後のデータは統計・バックテスト目的で
// 保持可能).
var directPIIAttributeKeys = map[string]bool{
	"name":            true,
	"full_name":       true,
	"address":         true,
	"date_of_birth":   true,
	"birth_date":      true,
	"phone":           true,
	"phone_number":    true,
	"email":           true,
	"account_number":  true,
	"id_number":       true,
	"passport_number": true,
}

// maxTransactionScanForAnonymize bounds the ListByCustomer scan used to find
// the most recent transaction (the RET-004 statutory-period anchor,
// audit.md §6). A single compliance-driven anonymization request is rare
// enough that this bound is not a real cap in practice; if a customer ever
// exceeds it, ListByCustomerCursor pagination should replace this scan.
const maxTransactionScanForAnonymize = 100000

// ErrWithinStatutoryPeriod is returned when an APPI anonymization request
// targets a customer whose data is still within the 犯収法 statutory
// retention period (RET-004: 保存義務期間内のデータは削除対象外).
var ErrWithinStatutoryPeriod = errors.New("customer data is within the statutory retention period and cannot be anonymized")

// AnonymizeRequest is an APPI (個人情報保護法) deletion request for a single
// customer (RET-004).
type AnonymizeRequest struct {
	CustomerID  string
	Reason      string
	RequestedBy string
}

// Anonymize replaces a customer's direct-PII attributes with a fixed
// placeholder and sets AnonymizedAt, unless the customer's data is still
// within the statutory retention period anchored at their last transaction
// (audit.md §6: 顧客データの起算点は最終取引日). Deviates from the task
// document's narrower CustomerRepository/AuditRepository-only signature by
// also taking TransactionRepository (to resolve the last-transaction anchor)
// and RetentionRepository (to read the configurable customer_data retention
// period rather than hardcoding it), since RET-004's "保存義務期間内は削除
// 対象外" cannot be evaluated correctly without them.
func Anonymize(ctx context.Context, customers domain.CustomerRepository, transactions domain.TransactionRepository, retention domain.RetentionRepository, audit domain.AuditRepository, req AnonymizeRequest) error {
	customer, err := customers.Get(ctx, req.CustomerID)
	if err != nil {
		return err
	}

	policy, err := retention.Get(ctx, "customer_data")
	if err != nil {
		return err
	}

	anchor := customer.CreatedAt
	txs, err := transactions.ListByCustomer(ctx, req.CustomerID, maxTransactionScanForAnonymize, 0)
	if err != nil {
		return err
	}
	for _, tx := range txs {
		if tx.ExecutedAt.After(anchor) {
			anchor = tx.ExecutedAt
		}
	}

	cutoff := time.Now().AddDate(0, 0, -policy.RetentionDays)
	if anchor.After(cutoff) {
		return ErrWithinStatutoryPeriod
	}

	if customer.Attributes == nil {
		customer.Attributes = map[string]any{}
	}
	for key := range customer.Attributes {
		if directPIIAttributeKeys[key] {
			customer.Attributes[key] = anonymizedPlaceholder
		}
	}
	now := time.Now()
	customer.AnonymizedAt = &now

	if err := customers.Update(ctx, customer); err != nil {
		return err
	}

	// Global Constraint (Auditability First): failing to record the audit
	// entry fails the operation itself, even though the customer mutation
	// already succeeded — callers should treat a non-nil error here as
	// requiring investigation/retry, not silently accept a partially
	// recorded anonymization.
	return audit.Create(ctx, &domain.AuditEntry{
		UserID:       req.RequestedBy,
		Action:       "anonymize_customer",
		ResourceType: "customer",
		ResourceID:   req.CustomerID,
		Details:      map[string]string{"reason": req.Reason},
		CreatedAt:    now,
	})
}
