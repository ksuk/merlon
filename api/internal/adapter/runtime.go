package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// SyncCheckpoint is the durable position at which a source page was fully
// applied. Payloads are never acknowledged by moving this position before
// the repository writes complete.
type SyncCheckpoint struct {
	AdapterID            string
	CustomerCursor       string
	TransactionCursor    string
	CustomerWatermark    string
	TransactionWatermark string
	UpdatedAt            time.Time
	AdapterDigest        string
}

type CheckpointRepository interface {
	Acquire(ctx context.Context, adapterID, owner string, now time.Time, lease time.Duration) (bool, error)
	Get(ctx context.Context, adapterID string) (*SyncCheckpoint, error)
	Save(ctx context.Context, checkpoint *SyncCheckpoint) error
	Release(ctx context.Context, adapterID, owner string) error
}

type MemoryCheckpointRepository struct {
	mu          sync.Mutex
	checkpoints map[string]*SyncCheckpoint
	leases      map[string]lease
}
type lease struct {
	owner string
	until time.Time
}

func NewMemoryCheckpointRepository() *MemoryCheckpointRepository {
	return &MemoryCheckpointRepository{checkpoints: map[string]*SyncCheckpoint{}, leases: map[string]lease{}}
}
func (r *MemoryCheckpointRepository) Acquire(_ context.Context, id, owner string, now time.Time, duration time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.leases[id]
	if current.owner != "" && current.until.After(now) && current.owner != owner {
		return false, nil
	}
	r.leases[id] = lease{owner: owner, until: now.Add(duration)}
	return true, nil
}
func (r *MemoryCheckpointRepository) Get(_ context.Context, id string) (*SyncCheckpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	checkpoint := r.checkpoints[id]
	if checkpoint == nil {
		return &SyncCheckpoint{AdapterID: id}, nil
	}
	cp := *checkpoint
	return &cp, nil
}
func (r *MemoryCheckpointRepository) Save(_ context.Context, checkpoint *SyncCheckpoint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *checkpoint
	r.checkpoints[checkpoint.AdapterID] = &cp
	return nil
}
func (r *MemoryCheckpointRepository) Release(_ context.Context, id, owner string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current := r.leases[id]; current.owner == owner {
		delete(r.leases, id)
	}
	return nil
}

type SyncDependencies struct {
	Customers    domain.CustomerRepository
	Transactions domain.TransactionRepository
	Accounts     domain.AccountRepository
	Checkpoints  CheckpointRepository
	Runs         SyncRunRepository
}

type SyncRunRepository interface {
	StartSyncRun(context.Context, *SyncRun) error
	FinishSyncRun(context.Context, *SyncRun, error) error
}

type SyncOutcome struct {
	ExternalID string `json:"external_id"`
	EntityType string `json:"entity_type"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
}

type SyncRun struct {
	ID                  string         `json:"id"`
	AdapterID           string         `json:"adapter_id"`
	Status              string         `json:"status"`
	Error               string         `json:"error,omitempty"`
	StartedAt           time.Time      `json:"started_at"`
	CompletedAt         time.Time      `json:"completed_at"`
	CustomerAccepted    int            `json:"customer_accepted"`
	CustomerSkipped     int            `json:"customer_skipped"`
	TransactionAccepted int            `json:"transaction_accepted"`
	TransactionSkipped  int            `json:"transaction_skipped"`
	WaitingDependency   int            `json:"waiting_dependency"`
	Failed              int            `json:"failed"`
	AdapterDigest       string         `json:"adapter_digest"`
	Checkpoint          SyncCheckpoint `json:"checkpoint"`
	Outcomes            []SyncOutcome  `json:"outcomes"`
}

type SyncService struct {
	AdapterID string
	Config    *AdapterConfig
	Adapter   Adapter
	Deps      SyncDependencies
	Owner     string
	Now       func() time.Time
}

const adapterLeaseDuration = 30 * time.Second

func (s *SyncService) Run(ctx context.Context) (run *SyncRun, runErr error) {
	if s.Config == nil || s.Adapter == nil {
		return nil, fmt.Errorf("adapter config and runtime adapter are required")
	}
	if err := s.Config.ValidateSync(); err != nil {
		return nil, err
	}
	if s.Deps.Customers == nil || s.Deps.Transactions == nil || s.Deps.Checkpoints == nil {
		return nil, fmt.Errorf("customers, transactions, and checkpoint repositories are required")
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	owner := s.Owner
	if owner == "" {
		owner = "adapter-worker"
	}
	adapterID := s.AdapterID
	if adapterID == "" {
		adapterID = "rest"
	}
	if acquired, err := s.Deps.Checkpoints.Acquire(ctx, adapterID, owner, now, adapterLeaseDuration); err != nil {
		return nil, err
	} else if !acquired {
		return nil, fmt.Errorf("adapter sync lease is held")
	}
	defer s.Deps.Checkpoints.Release(context.Background(), adapterID, owner)
	runCtx, cancel := context.WithCancel(ctx)
	leaseErrors := make(chan error, 1)
	go s.heartbeatLease(runCtx, cancel, adapterID, owner, leaseErrors)
	defer func() { cancel() }()
	checkpoint, err := s.Deps.Checkpoints.Get(runCtx, adapterID)
	if err != nil {
		return nil, err
	}
	run = &SyncRun{ID: stableAdapterID("adapter-sync-run", adapterID+"\x00"+now.Format(time.RFC3339Nano)), AdapterID: adapterID, Status: "running", StartedAt: now, AdapterDigest: digestConfig(s.Config)}
	if s.Deps.Runs != nil {
		if err := s.Deps.Runs.StartSyncRun(ctx, run); err != nil {
			return run, err
		}
		defer func() {
			if run.CompletedAt.IsZero() {
				run.CompletedAt = time.Now().UTC()
			}
			run.Checkpoint = *checkpoint
			if finishErr := s.Deps.Runs.FinishSyncRun(context.WithoutCancel(ctx), run, runErr); finishErr != nil && runErr == nil {
				runErr = finishErr
			}
		}()
	}
	if err := s.syncCustomers(runCtx, checkpoint, run); err != nil {
		run.Failed++
		return run, err
	}
	if err := s.syncTransactions(runCtx, checkpoint, run); err != nil {
		run.Failed++
		return run, err
	}
	checkpoint.UpdatedAt = now
	checkpoint.AdapterDigest = run.AdapterDigest
	select {
	case leaseErr := <-leaseErrors:
		return run, leaseErr
	default:
	}
	if err := s.Deps.Checkpoints.Save(runCtx, checkpoint); err != nil {
		return run, err
	}
	run.Checkpoint = *checkpoint
	run.CompletedAt = time.Now().UTC()
	if s.Now != nil {
		run.CompletedAt = s.Now().UTC()
	}
	return run, nil
}

func (s *SyncService) heartbeatLease(ctx context.Context, cancel context.CancelFunc, adapterID, owner string, failures chan<- error) {
	ticker := time.NewTicker(adapterLeaseDuration / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-ticker.C:
			if s.Now != nil {
				at = s.Now().UTC()
			}
			acquired, err := s.Deps.Checkpoints.Acquire(ctx, adapterID, owner, at.UTC(), adapterLeaseDuration)
			if err == nil && !acquired {
				err = fmt.Errorf("adapter sync lease ownership was lost")
			}
			if err != nil {
				select {
				case failures <- err:
				default:
				}
				cancel()
				return
			}
		}
	}
}

func (s *SyncService) syncCustomers(ctx context.Context, checkpoint *SyncCheckpoint, run *SyncRun) error {
	fetcher, ok := s.Adapter.(CustomerPageFetcher)
	if !ok {
		return fmt.Errorf("adapter does not implement paginated customer fetch")
	}
	cursor := checkpoint.CustomerCursor
	for {
		params := pageParams(s.Config.Sync, cursor, checkpoint.CustomerWatermark, false)
		page, err := fetcher.FetchCustomersPage(ctx, params)
		if err != nil {
			return err
		}
		for _, data := range page.Customers {
			if err := s.upsertCustomer(ctx, data, run); err != nil {
				return err
			}
		}
		checkpoint.CustomerCursor = page.NextCursor
		checkpoint.CustomerWatermark = advanceWatermark(checkpoint.CustomerWatermark, page.Watermark)
		if err := s.Deps.Checkpoints.Save(ctx, checkpoint); err != nil {
			return err
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			return nil
		}
		cursor = page.NextCursor
	}
}

func (s *SyncService) upsertCustomer(ctx context.Context, data CustomerData, run *SyncRun) error {
	if strings.TrimSpace(data.ExternalID) == "" {
		run.Failed++
		run.Outcomes = append(run.Outcomes, SyncOutcome{EntityType: "customer", Status: "rejected", Reason: "missing_external_id"})
		return nil
	}
	existing, err := s.Deps.Customers.GetByExternalID(ctx, data.ExternalID)
	if err == nil && existing != nil {
		if existing.CountryCode == data.Country && existing.CustomerType == domain.CustomerType(data.CustomerType) && existing.Attributes["name"] == data.Name {
			run.CustomerSkipped++
			run.Outcomes = append(run.Outcomes, SyncOutcome{EntityType: "customer", ExternalID: data.ExternalID, Status: "skipped", Reason: "same_payload"})
			return nil
		}
		existing.CountryCode, existing.CustomerType = data.Country, domain.CustomerType(data.CustomerType)
		if existing.Attributes == nil {
			existing.Attributes = map[string]any{}
		}
		if data.Name != "" {
			existing.Attributes["name"] = data.Name
		}
		if err := s.Deps.Customers.Update(ctx, existing); err != nil {
			return err
		}
		run.CustomerAccepted++
		run.Outcomes = append(run.Outcomes, SyncOutcome{EntityType: "customer", ExternalID: data.ExternalID, Status: "updated"})
		return nil
	}
	if err != nil && !isNotFoundAdapter(err) {
		return err
	}
	customer := &domain.Customer{ID: stableAdapterID("customer", data.ExternalID), ExternalID: data.ExternalID, CountryCode: data.Country, CustomerType: domain.CustomerType(data.CustomerType), Status: domain.CustomerStatusActive, Attributes: data.RawFields, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if customer.Attributes == nil {
		customer.Attributes = map[string]any{}
	}
	if data.Name != "" {
		customer.Attributes["name"] = data.Name
	}
	if err := s.Deps.Customers.Create(ctx, customer); err != nil {
		return err
	}
	run.CustomerAccepted++
	run.Outcomes = append(run.Outcomes, SyncOutcome{EntityType: "customer", ExternalID: data.ExternalID, Status: "accepted"})
	return nil
}

func (s *SyncService) syncTransactions(ctx context.Context, checkpoint *SyncCheckpoint, run *SyncRun) error {
	fetcher, ok := s.Adapter.(TransactionPageFetcher)
	if !ok {
		return fmt.Errorf("adapter does not implement paginated transaction fetch")
	}
	cursor := checkpoint.TransactionCursor
	for {
		params := pageParams(s.Config.Sync, cursor, checkpoint.TransactionWatermark, true)
		page, err := fetcher.FetchTransactionsPage(ctx, params)
		if err != nil {
			return err
		}
		waitingBefore := run.WaitingDependency
		for _, data := range page.Transactions {
			if err := s.ingestTransaction(ctx, data, run); err != nil {
				return err
			}
		}
		if run.WaitingDependency > waitingBefore {
			// Do not acknowledge a page containing unresolved dependencies.
			// Already accepted records are idempotently skipped on the retry.
			return nil
		}
		checkpoint.TransactionCursor = page.NextCursor
		checkpoint.TransactionWatermark = advanceWatermark(checkpoint.TransactionWatermark, page.Watermark)
		if err := s.Deps.Checkpoints.Save(ctx, checkpoint); err != nil {
			return err
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			return nil
		}
		cursor = page.NextCursor
	}
}

func advanceWatermark(current, candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return current
	}
	candidateAt, candidateErr := time.Parse(time.RFC3339, candidate)
	if candidateErr != nil {
		return current
	}
	currentAt, currentErr := time.Parse(time.RFC3339, strings.TrimSpace(current))
	if currentErr != nil || candidateAt.After(currentAt) {
		return candidate
	}
	return current
}

func (s *SyncService) ingestTransaction(ctx context.Context, data TransactionData, run *SyncRun) error {
	if data.ExternalID == "" {
		run.Failed++
		run.Outcomes = append(run.Outcomes, SyncOutcome{EntityType: "transaction", Status: "rejected", Reason: "missing_external_id"})
		return nil
	}
	customer, err := s.Deps.Customers.GetByExternalID(ctx, data.CustomerExternalID)
	if err != nil {
		if isNotFoundAdapter(err) {
			run.WaitingDependency++
			run.Outcomes = append(run.Outcomes, SyncOutcome{EntityType: "transaction", ExternalID: data.ExternalID, Status: "waiting_dependency", Reason: "customer_not_found"})
			return nil
		}
		return err
	}
	if repo, ok := s.Deps.Transactions.(domain.TransactionExternalIDRepository); ok {
		existing, lookupErr := repo.GetByExternalID(ctx, data.ExternalID)
		if lookupErr == nil && existing != nil {
			run.TransactionSkipped++
			run.Outcomes = append(run.Outcomes, SyncOutcome{EntityType: "transaction", ExternalID: data.ExternalID, Status: "skipped", Reason: "immutable_duplicate"})
			return nil
		}
		if lookupErr != nil && !isNotFoundAdapter(lookupErr) {
			return lookupErr
		}
	}
	amount, err := strconv.ParseFloat(data.Amount, 64)
	if err != nil {
		run.Failed++
		run.Outcomes = append(run.Outcomes, SyncOutcome{EntityType: "transaction", ExternalID: data.ExternalID, Status: "rejected", Reason: "invalid_amount"})
		return nil
	}
	tx := &domain.Transaction{ID: stableAdapterID("transaction", data.ExternalID), ExternalID: data.ExternalID, CustomerID: customer.ID, Amount: amount, Currency: data.Currency, Direction: domain.TransactionDirection(data.Direction), TransactionType: domain.TransactionType(data.Type), ExecutedAt: data.ExecutedAt, CreatedAt: time.Now().UTC(), Metadata: data.RawFields}
	if err := s.Deps.Transactions.Create(ctx, tx); err != nil {
		return err
	}
	run.TransactionAccepted++
	run.Outcomes = append(run.Outcomes, SyncOutcome{EntityType: "transaction", ExternalID: data.ExternalID, Status: "accepted"})
	return nil
}

func pageParams(cfg SyncConfig, cursor, watermark string, transaction bool) map[string]string {
	params := map[string]string{"limit": strconv.Itoa(cfg.PageSize), "page_size": strconv.Itoa(cfg.PageSize)}
	if cfg.CursorParam != "" && cursor != "" {
		params[cfg.CursorParam] = cursor
	}
	if cfg.WatermarkParam != "" {
		if watermark != "" {
			params[cfg.WatermarkParam] = watermark
		} else {
			params[cfg.WatermarkParam] = time.Now().Add(-cfg.InitialLookback).UTC().Format(time.RFC3339)
		}
	}
	_ = transaction
	return params
}
func digestConfig(cfg *AdapterConfig) string {
	b := []byte(fmt.Sprintf("%s|%s|%v|%d|%s", cfg.Type, cfg.BaseURL, cfg.Endpoints, cfg.Sync.PageSize, cfg.Sync.Interval))
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func stableAdapterID(kind, external string) string {
	h := sha256.Sum256([]byte(kind + "\x00" + external))
	b := h[:16]
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
func isNotFoundAdapter(err error) bool { var nf *domain.ErrNotFound; return errors.As(err, &nf) }
