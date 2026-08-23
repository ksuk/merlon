package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/policy"
	inboundwebhook "github.com/ksuk/merlon/api/internal/webhook"
)

// InboundRecordHandler exposes the same repository-backed mutation seam used
// by the ordinary customer/transaction APIs.  The webhook transport calls it
// only after the signed envelope has been durably encrypted.
func (s *Server) InboundRecordHandler() inboundwebhook.RecordHandler {
	return func(ctx context.Context, kind inboundwebhook.Kind, index int, raw json.RawMessage) (inboundwebhook.RecordOutcome, error) {
		switch kind {
		case inboundwebhook.KindCustomers:
			return s.ingestInboundCustomer(ctx, index, raw)
		case inboundwebhook.KindTransactions:
			return s.ingestInboundTransaction(ctx, index, raw)
		default:
			return inboundwebhook.RecordOutcome{Index: index, Status: inboundwebhook.RecordRejected, Reason: "unsupported_kind"}, nil
		}
	}
}

type inboundCustomerRecord struct {
	ExternalID      string                `json:"external_id"`
	CustomerType    domain.CustomerType   `json:"customer_type"`
	CountryCode     string                `json:"country_code"`
	ProductTypes    []string              `json:"product_types"`
	Status          domain.CustomerStatus `json:"status"`
	Attributes      map[string]any        `json:"attributes"`
	SourceUpdatedAt *time.Time            `json:"source_updated_at,omitempty"`
}

func (s *Server) ingestInboundCustomer(ctx context.Context, index int, raw json.RawMessage) (inboundwebhook.RecordOutcome, error) {
	outcome := inboundwebhook.RecordOutcome{Index: index, EntityType: "customer"}
	var req inboundCustomerRecord
	if err := json.Unmarshal(raw, &req); err != nil {
		outcome.Status, outcome.Reason = inboundwebhook.RecordRejected, "invalid_customer_json"
		return outcome, nil
	}
	outcome.ExternalID = strings.TrimSpace(req.ExternalID)
	if outcome.ExternalID == "" {
		outcome.Status, outcome.Reason = inboundwebhook.RecordRejected, "external_id_required"
		return outcome, nil
	}
	if req.SourceUpdatedAt == nil || req.SourceUpdatedAt.IsZero() {
		outcome.Status, outcome.Reason = inboundwebhook.RecordRejected, "source_updated_at_required"
		return outcome, nil
	}
	if !domain.IsValidCustomerType(req.CustomerType) {
		outcome.Status, outcome.Reason = inboundwebhook.RecordRejected, "invalid_customer_type"
		return outcome, nil
	}
	if req.Status == "" {
		req.Status = domain.CustomerStatusActive
	}
	if !isValidCustomerStatus(req.Status) {
		outcome.Status, outcome.Reason = inboundwebhook.RecordRejected, "invalid_status"
		return outcome, nil
	}
	if req.Attributes == nil {
		req.Attributes = map[string]any{}
	}
	if err := validateAttributes(req.Attributes); err != nil {
		outcome.Status, outcome.Reason = inboundwebhook.RecordRejected, "invalid_attributes"
		return outcome, nil
	}

	existing, err := s.customers.GetByExternalID(ctx, req.ExternalID)
	if err == nil && existing != nil {
		beforeAttributes := cloneAnyMap(existing.Attributes)
		beforeCountry, beforeStatus, beforeProducts := existing.CountryCode, existing.EffectiveStatus(), append([]string(nil), existing.ProductTypes...)
		outcome.EntityID = existing.ID
		if req.SourceUpdatedAt != nil && existing.SourceUpdatedAt != nil && !req.SourceUpdatedAt.After(*existing.SourceUpdatedAt) {
			outcome.Status, outcome.Reason = inboundwebhook.RecordSkipped, "stale_source_updated_at"
			return outcome, nil
		}
		// A complete source record is an upsert.  The source timestamp, when
		// present, is retained separately from transport UpdatedAt.
		existing.CustomerType, existing.CountryCode = req.CustomerType, req.CountryCode
		existing.ProductTypes, existing.Status, existing.Attributes = req.ProductTypes, req.Status, req.Attributes
		existing.SourceUpdatedAt = req.SourceUpdatedAt
		if missing := s.kycMissingFields(existing); len(missing) > 0 && s.policies.KYC().Enforce() == policy.KYCEnforcementReject {
			outcome.Status, outcome.Reason = inboundwebhook.RecordRejected, "missing_required_identity_fields"
			return outcome, nil
		}
		if err := s.runAtomic(ctx, func(repos domain.AtomicMutationRepositories) error {
			if repos.Customers == nil || repos.Audit == nil {
				return errAtomicMutationUnavailable
			}
			if err := repos.Customers.Update(ctx, existing); err != nil {
				return err
			}
			if err := s.scoreInboundCustomerWithRepo(ctx, existing, repos.Customers); err != nil {
				return err
			}
			if repos.IdentityHistory != nil {
				if err := repos.IdentityHistory.AppendCustomerIdentityHistory(ctx, &domain.CustomerIdentityHistoryEntry{ID: generateID(), CustomerID: existing.ID, ChangedFields: map[string]any{"before": beforeAttributes, "after": existing.Attributes, "before_country_code": beforeCountry, "after_country_code": existing.CountryCode, "before_status": beforeStatus, "after_status": existing.EffectiveStatus(), "before_product_types": beforeProducts, "after_product_types": existing.ProductTypes}, Actor: "system:inbound-webhook", Rationale: "inbound customer update", CreatedAt: time.Now().UTC()}); err != nil {
					return err
				}
			}
			return createInboundAudit(ctx, repos.Audit, "update", "customers", existing.ID, req.ExternalID)
		}); err != nil {
			return outcome, fmt.Errorf("atomic customer update: %w", err)
		}
		outcome.Status = inboundwebhook.RecordUpdated
		return outcome, nil
	}
	if err != nil && !isNotFoundInbound(err) {
		return outcome, fmt.Errorf("lookup customer %s: %w", req.ExternalID, err)
	}
	now := time.Now().UTC()
	customer := &domain.Customer{ID: generateID(), ExternalID: req.ExternalID, CustomerType: req.CustomerType,
		CountryCode: req.CountryCode, ProductTypes: req.ProductTypes, Status: req.Status, Attributes: req.Attributes,
		SourceUpdatedAt: req.SourceUpdatedAt, CreatedAt: now, UpdatedAt: now}
	if missing := s.kycMissingFields(customer); len(missing) > 0 && s.policies.KYC().Enforce() == policy.KYCEnforcementReject {
		outcome.Status, outcome.Reason = inboundwebhook.RecordRejected, "missing_required_identity_fields"
		return outcome, nil
	}
	if err := s.runAtomic(ctx, func(repos domain.AtomicMutationRepositories) error {
		if repos.Customers == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if err := repos.Customers.Create(ctx, customer); err != nil {
			return err
		}
		if err := s.scoreInboundCustomerWithRepo(ctx, customer, repos.Customers); err != nil {
			return err
		}
		if repos.IdentityHistory != nil {
			if err := repos.IdentityHistory.AppendCustomerIdentityHistory(ctx, &domain.CustomerIdentityHistoryEntry{ID: generateID(), CustomerID: customer.ID, ChangedFields: map[string]any{"after": customer.Attributes, "country_code": customer.CountryCode, "status": customer.EffectiveStatus()}, Actor: "system:inbound-webhook", Rationale: "inbound customer created", CreatedAt: now}); err != nil {
				return err
			}
		}
		return createInboundAudit(ctx, repos.Audit, "create", "customers", customer.ID, req.ExternalID)
	}); err != nil {
		return outcome, fmt.Errorf("atomic customer create: %w", err)
	}
	outcome.EntityID, outcome.Status = customer.ID, inboundwebhook.RecordAccepted
	return outcome, nil
}

func (s *Server) scoreInboundCustomerWithRepo(ctx context.Context, customer *domain.Customer, customers domain.CustomerRepository) error {
	if s.scoring == nil || customer == nil {
		return nil
	}
	score, err := s.scoring.ScoreCustomer(ctx, customer, "")
	if err != nil {
		return fmt.Errorf("%w: score customer: %v", inboundwebhook.ErrDependency, err)
	}
	if score == nil {
		return nil
	}
	if err := customers.SaveScoreRecord(ctx, score); err != nil {
		return fmt.Errorf("%w: save customer score: %v", inboundwebhook.ErrDependency, err)
	}
	customer.RiskScore, customer.RiskTier, customer.LastScoredAt = &score.Score, &score.Tier, &score.ScoredAt
	if err := customers.Update(ctx, customer); err != nil {
		return fmt.Errorf("%w: update customer score: %v", inboundwebhook.ErrDependency, err)
	}
	return nil
}

type inboundTransactionRecord struct {
	ExternalID          string                      `json:"external_id"`
	CustomerID          string                      `json:"customer_id,omitempty"`
	CustomerExternalID  string                      `json:"customer_external_id,omitempty"`
	AccountID           string                      `json:"account_id,omitempty"`
	AccountExternalID   string                      `json:"account_external_id,omitempty"`
	Amount              float64                     `json:"amount"`
	Currency            string                      `json:"currency"`
	Direction           domain.TransactionDirection `json:"direction"`
	TransactionType     domain.TransactionType      `json:"transaction_type,omitempty"`
	CounterpartyID      string                      `json:"counterparty_id,omitempty"`
	CounterpartyCountry string                      `json:"counterparty_country,omitempty"`
	Channel             string                      `json:"channel,omitempty"`
	Metadata            map[string]any              `json:"metadata,omitempty"`
	ExecutedAt          time.Time                   `json:"executed_at"`
}

func (s *Server) ingestInboundTransaction(ctx context.Context, index int, raw json.RawMessage) (inboundwebhook.RecordOutcome, error) {
	outcome := inboundwebhook.RecordOutcome{Index: index, EntityType: "transaction"}
	var req inboundTransactionRecord
	if err := json.Unmarshal(raw, &req); err != nil {
		outcome.Status, outcome.Reason = inboundwebhook.RecordRejected, "invalid_transaction_json"
		return outcome, nil
	}
	outcome.ExternalID = strings.TrimSpace(req.ExternalID)
	if outcome.ExternalID == "" {
		outcome.Status, outcome.Reason = inboundwebhook.RecordRejected, "external_id_required"
		return outcome, nil
	}
	if req.Amount <= 0 || req.Currency == "" || !isValidDirection(req.Direction) {
		outcome.Status, outcome.Reason = inboundwebhook.RecordRejected, "invalid_transaction_fields"
		return outcome, nil
	}
	var customer *domain.Customer
	var err error
	if req.CustomerID != "" {
		customer, err = s.customers.Get(ctx, req.CustomerID)
	} else if req.CustomerExternalID != "" {
		customer, err = s.customers.GetByExternalID(ctx, req.CustomerExternalID)
	} else {
		return outcome, fmt.Errorf("%w: customer reference required", inboundwebhook.ErrDependency)
	}
	if err != nil {
		if isNotFoundInbound(err) {
			return outcome, fmt.Errorf("%w: customer dependency is not available", inboundwebhook.ErrDependency)
		}
		return outcome, fmt.Errorf("lookup transaction customer: %w", err)
	}
	var accountID *string
	if req.AccountID != "" {
		id := req.AccountID
		accountID = &id
	} else if req.AccountExternalID != "" && s.accounts != nil {
		accountRepo, ok := s.accounts.(domain.AccountExternalIDRepository)
		if !ok {
			return outcome, fmt.Errorf("%w: account external lookup is unavailable", inboundwebhook.ErrDependency)
		}
		account, accountErr := accountRepo.GetByExternalID(ctx, req.AccountExternalID)
		if accountErr != nil {
			if isNotFoundInbound(accountErr) {
				return outcome, fmt.Errorf("%w: account dependency is not available", inboundwebhook.ErrDependency)
			}
			return outcome, accountErr
		}
		accountID = &account.ID
	}
	if req.ExecutedAt.IsZero() {
		outcome.Status, outcome.Reason = inboundwebhook.RecordRejected, "executed_at_required"
		return outcome, nil
	}
	if repo, ok := s.transactions.(domain.TransactionExternalIDRepository); ok {
		existing, lookupErr := repo.GetByExternalID(ctx, req.ExternalID)
		if lookupErr == nil && existing != nil {
			outcome.EntityID = existing.ID
			if inboundTransactionEqual(existing, customer.ID, accountID, &req) {
				outcome.Status, outcome.Reason = inboundwebhook.RecordSkipped, "duplicate_external_id"
				return outcome, nil
			}
			outcome.Status, outcome.Reason = inboundwebhook.RecordRejected, "immutable_conflict"
			return outcome, nil
		}
		if lookupErr != nil && !isNotFoundInbound(lookupErr) {
			return outcome, lookupErr
		}
	}
	now := time.Now().UTC()
	txn := &domain.Transaction{ID: generateID(), CustomerID: customer.ID, ExternalID: req.ExternalID, Amount: req.Amount,
		Currency: req.Currency, Direction: req.Direction, TransactionType: req.TransactionType, AccountID: accountID,
		CounterpartyID: req.CounterpartyID, CounterpartyCountry: req.CounterpartyCountry, Channel: req.Channel,
		Metadata: req.Metadata, ExecutedAt: req.ExecutedAt, CreatedAt: now}
	if err := s.runAtomic(ctx, func(repos domain.AtomicMutationRepositories) error {
		if repos.Transactions == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if err := repos.Transactions.Create(ctx, txn); err != nil {
			return err
		}
		return createInboundAudit(ctx, repos.Audit, "create", "transactions", txn.ID, req.ExternalID)
	}); err != nil {
		return outcome, fmt.Errorf("atomic transaction create: %w", err)
	}
	s.monitorCreatedTransaction(ctx, customer, txn)
	outcome.EntityID, outcome.Status = txn.ID, inboundwebhook.RecordAccepted
	return outcome, nil
}

func inboundTransactionEqual(existing *domain.Transaction, customerID string, accountID *string, req *inboundTransactionRecord) bool {
	if existing == nil || existing.CustomerID != customerID || existing.ExternalID != req.ExternalID || existing.Amount != req.Amount ||
		!strings.EqualFold(existing.Currency, req.Currency) || existing.Direction != req.Direction || existing.TransactionType != req.TransactionType ||
		existing.CounterpartyID != req.CounterpartyID || existing.CounterpartyCountry != req.CounterpartyCountry || existing.Channel != req.Channel ||
		existing.ExecutedAt.UnixNano() != req.ExecutedAt.UnixNano() || !reflect.DeepEqual(existing.Metadata, req.Metadata) {
		return false
	}
	if existing.AccountID == nil || accountID == nil {
		return existing.AccountID == nil && accountID == nil
	}
	return *existing.AccountID == *accountID
}

func createInboundAudit(ctx context.Context, audit domain.AuditRepository, action, resourceType, resourceID, externalID string) error {
	return audit.Create(ctx, &domain.AuditEntry{UserID: "system:inbound-webhook", Action: action, ResourceType: resourceType, ResourceID: resourceID,
		Details: map[string]string{"external_id": externalID, "source": "inbound_webhook"}, CreatedAt: time.Now().UTC()})
}

func isNotFoundInbound(err error) bool {
	var nf *domain.ErrNotFound
	return errors.As(err, &nf)
}
