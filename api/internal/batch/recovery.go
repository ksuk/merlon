package batch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/metrics"
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
	pending       domain.PendingEvaluationRepository
	monitoring    engine.MonitoringEngine
	alerts        domain.AlertRepository
	transactions  domain.TransactionRepository
	customers     domain.CustomerRepository
	atomic        domain.AtomicMutationRepository
	audit         domain.AuditRepository
	eventOutbox   domain.EventOutboxRepository
	ConfigDigests map[string]string
	pollInterval  time.Duration
}

const pendingRecoveryActor = "system:pending-recovery"

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

// SetPersistence wires the Wave 3 transaction and its required audit/outbox
// appenders without changing the constructor used by existing workers/tests.
func (j *RecoveryJob) SetPersistence(atomic domain.AtomicMutationRepository, audit domain.AuditRepository, eventOutbox domain.EventOutboxRepository) {
	j.atomic = atomic
	j.audit = audit
	j.eventOutbox = eventOutbox
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

	var joined error
	for _, pe := range records {
		if err := j.reevaluate(ctx, &pe); err == nil {
			processed++
		} else {
			slog.ErrorContext(ctx, "pending evaluation recovery failed", "pending_evaluation_id", pe.ID, "error", err)
			if joined == nil {
				joined = err
			} else {
				joined = errors.Join(joined, err)
			}
		}
	}
	return processed, joined
}

// reevaluate attempts to re-run monitoring for a single PENDING_REVIEW
// record, returning nil only after all persistence and status updates succeed.
func (j *RecoveryJob) reevaluate(ctx context.Context, pe *domain.PendingEvaluation) error {
	c, err := j.customers.Get(ctx, pe.CustomerID)
	if err != nil {
		return j.recordFailure(ctx, pe, fmt.Errorf("load customer: %w", err))
	}

	var txns []domain.Transaction
	for _, id := range pe.TransactionIDs {
		t, err := j.transactions.Get(ctx, id)
		if err != nil {
			return j.recordFailure(ctx, pe, fmt.Errorf("load transaction %s: %w", id, err))
		}
		txns = append(txns, *t)
	}

	riskTier := domain.RiskTierLow
	if c.RiskTier != nil {
		riskTier = *c.RiskTier
	}

	alerts, err := engine.EvaluateCompat(ctx, j.monitoring, engine.MonitoringRequest{CustomerID: pe.CustomerID, CustomerType: c.CustomerType, RiskTier: riskTier, Transactions: txns, Mode: engine.EvaluationModeRealtime, EvaluatedAt: time.Now().UTC(), ConfigDigests: copyDigests(j.ConfigDigests)})
	if err != nil {
		return j.recordFailure(ctx, pe, fmt.Errorf("evaluate transactions: %w", err))
	}

	if j.atomic != nil {
		if err := j.persistRecoveredAtomic(ctx, pe, alerts); err != nil {
			return err
		}
		return nil
	}

	// Compatibility path for small legacy compositions that do not provide an
	// atomic repository. Production wiring always supplies one; retaining this
	// path keeps the pre-Wave-3 unit contract usable for adapters that have not
	// adopted the durable workflow yet.
	for _, a := range alerts {
		if err := prepareRecoveryAlert(&a); err != nil {
			return j.recordFailure(ctx, pe, err)
		}
		created, existing, err := j.alerts.CreateIfNotDuplicate(ctx, &a)
		if err != nil {
			return j.recordFailure(ctx, pe, fmt.Errorf("persist alert: %w", err))
		}
		if !created && existing != nil {
			if err := j.alerts.AnnotateBatchReviewed(ctx, existing.ID, pe.ID); err != nil {
				return j.recordFailure(ctx, pe, fmt.Errorf("annotate duplicate alert: %w", err))
			}
		}
	}

	if err := j.pending.UpdateStatus(ctx, pe.ID, domain.PendingEvaluationStatusResolved); err != nil {
		return j.recordFailure(ctx, pe, fmt.Errorf("mark pending evaluation resolved: %w", err))
	}
	return nil
}

func prepareRecoveryAlert(a *domain.Alert) error {
	if a == nil {
		return fmt.Errorf("persist alert: nil alert")
	}
	a.ID = generateID()
	now := time.Now().UTC()
	a.CreatedAt = now
	a.UpdatedAt = now
	return nil
}

func (j *RecoveryJob) persistRecoveredAtomic(ctx context.Context, pe *domain.PendingEvaluation, results []domain.Alert) error {
	return j.atomic.RunAtomic(ctx, func(repos domain.AtomicMutationRepositories) error {
		if repos.PendingEvaluations == nil || repos.Alerts == nil || repos.Audit == nil {
			return fmt.Errorf("pending recovery atomic repositories are incomplete")
		}
		processing, err := repos.PendingEvaluations.TransitionPendingEvaluation(ctx, pe.ID, "process", pendingRecoveryActor, "automatic recovery claimed", pe.Version)
		if err != nil {
			return fmt.Errorf("claim pending evaluation: %w", err)
		}
		alertIDs := make([]string, 0, len(results))
		for _, alert := range results {
			if err := prepareRecoveryAlert(&alert); err != nil {
				return err
			}
			created, existing, err := repos.Alerts.CreateIfNotDuplicate(ctx, &alert)
			if err != nil {
				return fmt.Errorf("persist alert: %w", err)
			}
			if created {
				alertIDs = append(alertIDs, alert.ID)
				continue
			}
			if existing == nil {
				return fmt.Errorf("duplicate alert lookup returned no existing alert")
			}
			if err := repos.Alerts.AnnotateBatchReviewed(ctx, existing.ID, pe.ID); err != nil {
				return fmt.Errorf("annotate duplicate alert: %w", err)
			}
			alertIDs = append(alertIDs, existing.ID)
		}
		if err := repos.PendingEvaluations.SetPendingEvaluationAlertIDs(ctx, pe.ID, alertIDs, processing.Version); err != nil {
			return fmt.Errorf("persist pending alert links: %w", err)
		}
		resolved, err := repos.PendingEvaluations.TransitionPendingEvaluation(ctx, pe.ID, "resolve", pendingRecoveryActor, "automatic recovery completed", processing.Version+1)
		if err != nil {
			return fmt.Errorf("resolve pending evaluation: %w", err)
		}
		createdAt := time.Now().UTC()
		if err := repos.Audit.Create(ctx, &domain.AuditEntry{UserID: pendingRecoveryActor, Action: "pending_evaluation_recovered", ResourceType: "pending_evaluations", ResourceID: pe.ID, Details: map[string]string{"alert_count": fmt.Sprint(len(alertIDs)), "correlation_id": "pending-recovery:" + pe.ID}, CreatedAt: createdAt}); err != nil {
			return fmt.Errorf("append pending recovery audit: %w", err)
		}
		if j.eventOutbox != nil {
			if repos.EventOutbox == nil {
				return fmt.Errorf("pending recovery event outbox is not configured")
			}
			payload, err := json.Marshal(resolved)
			if err != nil {
				return fmt.Errorf("marshal pending recovery event: %w", err)
			}
			if err := repos.EventOutbox.Enqueue(ctx, &domain.DurableEvent{ID: generateID(), Topic: "pending.evaluation.recovered", Payload: payload, ChainID: "pending-recovery:" + pe.ID, CreatedAt: createdAt}); err != nil {
				return fmt.Errorf("enqueue pending recovery event: %w", err)
			}
		}
		return nil
	})
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// recordFailure increments retry_count and, once maxPendingRetries is
// exceeded, transitions the record to FAILED.
func (j *RecoveryJob) recordFailure(ctx context.Context, pe *domain.PendingEvaluation, cause error) error {
	if j.atomic != nil {
		err := j.atomic.RunAtomic(ctx, func(repos domain.AtomicMutationRepositories) error {
			if repos.PendingEvaluations == nil || repos.Audit == nil {
				return fmt.Errorf("pending recovery atomic repositories are incomplete")
			}
			updated, err := repos.PendingEvaluations.TransitionPendingEvaluation(ctx, pe.ID, "retry", pendingRecoveryActor, cause.Error(), pe.Version)
			if err != nil {
				return err
			}
			if updated.RetryCount >= maxPendingRetries {
				updated, err = repos.PendingEvaluations.TransitionPendingEvaluation(ctx, pe.ID, "escalate", pendingRecoveryActor, "automatic retry limit reached", updated.Version)
				if err != nil {
					return err
				}
			}
			createdAt := time.Now().UTC()
			if err := repos.Audit.Create(ctx, &domain.AuditEntry{UserID: pendingRecoveryActor, Action: "pending_evaluation_recovery_failed", ResourceType: "pending_evaluations", ResourceID: pe.ID, Details: map[string]string{"status": string(updated.Status), "retry_count": fmt.Sprint(updated.RetryCount), "correlation_id": "pending-recovery:" + pe.ID}, CreatedAt: createdAt}); err != nil {
				return err
			}
			if j.eventOutbox != nil {
				if repos.EventOutbox == nil {
					return fmt.Errorf("pending recovery event outbox is not configured")
				}
				payload, err := json.Marshal(updated)
				if err != nil {
					return err
				}
				if err := repos.EventOutbox.Enqueue(ctx, &domain.DurableEvent{ID: generateID(), Topic: "pending.evaluation.recovery_failed", Payload: payload, ChainID: "pending-recovery:" + pe.ID, CreatedAt: createdAt}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			metrics.PendingEvaluationFailuresTotal.WithLabelValues("atomic_transition").Inc()
			return fmt.Errorf("%w; atomic recovery failure: %v", cause, err)
		}
		metrics.PendingEvaluationFailuresTotal.WithLabelValues("reevaluate").Inc()
		return cause
	}
	if err := j.pending.IncrementRetry(ctx, pe.ID); err != nil {
		metrics.PendingEvaluationFailuresTotal.WithLabelValues("increment_retry").Inc()
		return fmt.Errorf("%w; increment retry: %v", cause, err)
	}
	if pe.RetryCount+1 >= maxPendingRetries {
		if err := j.pending.UpdateStatus(ctx, pe.ID, domain.PendingEvaluationStatusFailed); err != nil {
			metrics.PendingEvaluationFailuresTotal.WithLabelValues("mark_failed").Inc()
			return fmt.Errorf("%w; mark failed: %v", cause, err)
		}
	}
	metrics.PendingEvaluationFailuresTotal.WithLabelValues("reevaluate").Inc()
	return cause
}
