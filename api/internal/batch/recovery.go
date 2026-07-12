package batch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
)

// maxPendingRetries bounds how many times RunOnce re-attempts a PENDING_REVIEW
// record before giving up and marking it FAILED, so a permanently-broken
// customer/transaction record cannot retry forever (the operational design §4.4 does not
// mandate a specific count for this application-level retry, unlike the
// circuit breaker's timeout/retry/open-duration values).
const maxPendingRetries = 5

// defaultPollInterval is the Run loop's polling cadence between RunOnce
// sweeps of the PENDING_REVIEW queue.
const defaultPollInterval = 30 * time.Second

// RecoveryJob re-evaluates transactions queued as PENDING_REVIEW (OPS-005,
// the operational design §4.4 Fail-Alert) once the monitoring engine becomes reachable
// again, so detection resumes automatically instead of requiring manual
// intervention.
type RecoveryJob struct {
	pending      domain.PendingEvaluationRepository
	monitoring   engine.MonitoringEngine
	alerts       domain.AlertRepository
	transactions domain.TransactionRepository
	customers    domain.CustomerRepository
	pollInterval time.Duration
}

func NewRecoveryJob(
	pending domain.PendingEvaluationRepository,
	monitoring engine.MonitoringEngine,
	alerts domain.AlertRepository,
	transactions domain.TransactionRepository,
	customers domain.CustomerRepository,
) *RecoveryJob {
	return &RecoveryJob{
		pending:      pending,
		monitoring:   monitoring,
		alerts:       alerts,
		transactions: transactions,
		customers:    customers,
		pollInterval: defaultPollInterval,
	}
}

// Run polls the PENDING_REVIEW queue every pollInterval until ctx is
// canceled, invoking RunOnce on each tick.
func (j *RecoveryJob) Run(ctx context.Context) error {
	ticker := time.NewTicker(j.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := j.RunOnce(ctx); err != nil {
				continue
			}
		}
	}
}

// RunOnce re-evaluates every PENDING_REVIEW record once. Records that
// succeed are marked RESOLVED and generate alerts; records that fail again
// have retry_count incremented and, past maxPendingRetries, are marked
// FAILED so they stop being retried.
func (j *RecoveryJob) RunOnce(ctx context.Context) (processed int, err error) {
	const listPageSize = 100
	records, err := j.pending.ListByStatus(ctx, domain.PendingEvaluationStatusPendingReview, listPageSize, 0)
	if err != nil {
		return 0, err
	}

	for _, pe := range records {
		if j.reevaluate(ctx, &pe) {
			processed++
		}
	}
	return processed, nil
}

// reevaluate attempts to re-run monitoring for a single PENDING_REVIEW
// record, returning true if it was resolved.
func (j *RecoveryJob) reevaluate(ctx context.Context, pe *domain.PendingEvaluation) bool {
	c, err := j.customers.Get(ctx, pe.CustomerID)
	if err != nil {
		j.recordFailure(ctx, pe)
		return false
	}

	var txns []domain.Transaction
	for _, id := range pe.TransactionIDs {
		t, err := j.transactions.Get(ctx, id)
		if err != nil {
			continue
		}
		txns = append(txns, *t)
	}

	riskTier := domain.RiskTierLow
	if c.RiskTier != nil {
		riskTier = *c.RiskTier
	}

	alerts, err := j.monitoring.EvaluateTransactions(ctx, pe.CustomerID, riskTier, txns, nil)
	if err != nil {
		j.recordFailure(ctx, pe)
		return false
	}

	for _, a := range alerts {
		a.ID = generateID()
		now := time.Now()
		a.CreatedAt = now
		a.UpdatedAt = now
		if err := j.alerts.Create(ctx, &a); err != nil {
			continue
		}
	}

	_ = j.pending.UpdateStatus(ctx, pe.ID, domain.PendingEvaluationStatusResolved)
	return true
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// recordFailure increments retry_count and, once maxPendingRetries is
// exceeded, transitions the record to FAILED.
func (j *RecoveryJob) recordFailure(ctx context.Context, pe *domain.PendingEvaluation) {
	_ = j.pending.IncrementRetry(ctx, pe.ID)
	if pe.RetryCount+1 >= maxPendingRetries {
		_ = j.pending.UpdateStatus(ctx, pe.ID, domain.PendingEvaluationStatusFailed)
	}
}
