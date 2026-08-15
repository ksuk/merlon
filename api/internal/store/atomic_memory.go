package store

import (
	"context"
	"fmt"
	"sync"

	"github.com/ksuk/merlon/api/internal/domain"
)

// MemoryAtomicMutationRepo serializes a mutation and restores every affected
// in-memory repository when a required event or audit append fails. This is a
// real transaction boundary for the database-free mode, rather than a
// best-effort approximation that can leave a half-written case file.
type MemoryAtomicMutationRepo struct {
	mu            sync.Mutex
	repos         domain.AtomicMutationRepositories
	cases         *MemoryCaseRepo
	customers     *MemoryCustomerRepo
	transactions  *MemoryTransactionRepo
	alerts        *MemoryAlertRepo
	reports       *MemorySTRReportRepo
	audit         *MemoryAuditRepo
	investigation *MemoryCaseInvestigationRepo
	decisions     *MemoryAlertDecisionRepo
	outbox        *MemoryEventOutboxRepo
	wave3         *MemoryWave3Repo
	pending       *MemoryPendingEvaluationRepo
	batch         *MemoryBatchRunRepo
	backtest      *MemoryBacktestJobRepo
	reviews       *MemoryCustomerReviewRepo
}

func NewMemoryAtomicMutationRepo(repos domain.AtomicMutationRepositories) (*MemoryAtomicMutationRepo, error) {
	atomic := &MemoryAtomicMutationRepo{repos: repos}
	var ok bool
	if atomic.customers, ok = repos.Customers.(*MemoryCustomerRepo); !ok || atomic.customers == nil {
		return nil, fmt.Errorf("memory atomic repository requires MemoryCustomerRepo")
	}
	if repos.Transactions != nil {
		if atomic.transactions, ok = repos.Transactions.(*MemoryTransactionRepo); !ok || atomic.transactions == nil {
			return nil, fmt.Errorf("memory atomic repository requires MemoryTransactionRepo")
		}
	}
	if repos.Cases != nil {
		if atomic.cases, ok = repos.Cases.(*MemoryCaseRepo); !ok || atomic.cases == nil {
			return nil, fmt.Errorf("memory atomic repository requires MemoryCaseRepo")
		}
	}
	if repos.Alerts != nil {
		if atomic.alerts, ok = repos.Alerts.(*MemoryAlertRepo); !ok || atomic.alerts == nil {
			return nil, fmt.Errorf("memory atomic repository requires MemoryAlertRepo")
		}
	}
	if atomic.audit, ok = repos.Audit.(*MemoryAuditRepo); !ok || atomic.audit == nil {
		return nil, fmt.Errorf("memory atomic repository requires MemoryAuditRepo")
	}
	if repos.Reports != nil {
		if atomic.reports, ok = repos.Reports.(*MemorySTRReportRepo); !ok || atomic.reports == nil {
			return nil, fmt.Errorf("memory atomic repository requires MemorySTRReportRepo")
		}
	}
	if repos.Investigation != nil {
		if atomic.investigation, ok = repos.Investigation.(*MemoryCaseInvestigationRepo); !ok || atomic.investigation == nil {
			return nil, fmt.Errorf("memory atomic repository requires MemoryCaseInvestigationRepo")
		}
	}
	if repos.AlertDecisions != nil {
		if atomic.decisions, ok = repos.AlertDecisions.(*MemoryAlertDecisionRepo); !ok || atomic.decisions == nil {
			return nil, fmt.Errorf("memory atomic repository requires MemoryAlertDecisionRepo")
		}
	}
	if repos.EventOutbox != nil {
		if atomic.outbox, ok = repos.EventOutbox.(*MemoryEventOutboxRepo); !ok || atomic.outbox == nil {
			return nil, fmt.Errorf("memory atomic repository requires MemoryEventOutboxRepo")
		}
	}
	if repos.IdentityHistory != nil {
		if atomic.wave3, ok = repos.IdentityHistory.(*MemoryWave3Repo); !ok || atomic.wave3 == nil {
			return nil, fmt.Errorf("memory atomic repository requires MemoryWave3Repo for identity history")
		}
	}
	if repos.Wave3 != nil && atomic.wave3 == nil {
		if atomic.wave3, ok = repos.Wave3.(*MemoryWave3Repo); !ok || atomic.wave3 == nil {
			return nil, fmt.Errorf("memory atomic repository requires MemoryWave3Repo for Wave 3 workflows")
		}
	}
	if repos.PendingEvaluations != nil {
		if atomic.pending, ok = repos.PendingEvaluations.(*MemoryPendingEvaluationRepo); !ok || atomic.pending == nil {
			return nil, fmt.Errorf("memory atomic repository requires MemoryPendingEvaluationRepo")
		}
	}
	if repos.BatchRuns != nil {
		if atomic.batch, ok = repos.BatchRuns.(*MemoryBatchRunRepo); !ok || atomic.batch == nil {
			return nil, fmt.Errorf("memory atomic repository requires MemoryBatchRunRepo")
		}
	}
	if repos.BacktestJobs != nil {
		if atomic.backtest, ok = repos.BacktestJobs.(*MemoryBacktestJobRepo); !ok || atomic.backtest == nil {
			return nil, fmt.Errorf("memory atomic repository requires MemoryBacktestJobRepo")
		}
	}
	if repos.CustomerReviews != nil {
		if atomic.reviews, ok = repos.CustomerReviews.(*MemoryCustomerReviewRepo); !ok || atomic.reviews == nil {
			return nil, fmt.Errorf("memory atomic repository requires MemoryCustomerReviewRepo")
		}
	}
	return atomic, nil
}

type memoryAtomicSnapshot struct {
	customers          map[string]*domain.Customer
	customerExternal   map[string]string
	customerScores     map[string][]domain.ScoreRecord
	transactions       map[string]*domain.Transaction
	transactionByCust  map[string][]string
	transactionIdem    map[string]string
	cases              map[string]*domain.Case
	alerts             map[string]*domain.Alert
	alertByCustomer    map[string][]string
	reports            map[string]*domain.STRReport
	reportEvents       []domain.STRReportEvent
	auditEntries       []domain.AuditEntry
	auditNextID        int64
	events             map[string][]domain.CaseEvent
	evidence           map[string][]domain.CaseEvidence
	checklist          map[string]map[string]domain.CaseChecklistItem
	work               map[string]map[string]domain.CaseWorkItem
	relationship       map[string]*domain.CaseRelationship
	relationshipEvents []domain.CaseRelationshipEvent
	decisions          map[string][]domain.AlertDecisionEvent
	outboxEvents       []domain.DurableEvent
	outboxNextSequence int64
	identity           map[string][]domain.CustomerIdentityHistoryEntry
	wave3Runs          map[string]*domain.ScreeningRun
	wave3Results       map[string]*domain.ScreeningResultRecord
	wave3History       map[string][]domain.ScreeningResultHistoryEntry
	wave3Sources       map[string]domain.ScreeningSourceSnapshot
	wave3Metadata      map[string]*domain.BacktestMetadata
	wave3Manifests     map[string]*domain.TargetManifest
	pendingData        map[string]*domain.PendingEvaluation
	pendingHistory     map[string][]domain.PendingEvaluationHistoryEntry
	batchData          map[string]*domain.BatchRun
	backtestData       map[string]*domain.BacktestJob
	backtestSnapshots  map[string][]string
	backtestOutcomes   map[string][]domain.BacktestOutcomeDetail
	reviewData         map[string]*domain.CustomerReview
	reviewByKey        map[string]string
}

func (r *MemoryAtomicMutationRepo) RunAtomic(ctx context.Context, fn func(domain.AtomicMutationRepositories) error) error {
	if r == nil {
		return fmt.Errorf("memory atomic repository is not configured")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := r.snapshot()
	if err := fn(r.repos); err != nil {
		r.restore(snapshot)
		return err
	}
	return nil
}

func (r *MemoryAtomicMutationRepo) snapshot() memoryAtomicSnapshot {
	r.customers.mu.RLock()
	customers := cloneMemoryCustomers(r.customers.data)
	customerExternal := cloneStringMap(r.customers.external)
	customerScores := cloneScoreRecords(r.customers.scores)
	r.customers.mu.RUnlock()

	var transactions map[string]*domain.Transaction
	var transactionByCust map[string][]string
	var transactionIdem map[string]string
	if r.transactions != nil {
		r.transactions.mu.RLock()
		transactions = cloneMemoryTransactions(r.transactions.data)
		transactionByCust = cloneStringSlices(r.transactions.byCustomer)
		transactionIdem = cloneStringMap(r.transactions.idempotencyKey)
		r.transactions.mu.RUnlock()
	}

	var cases map[string]*domain.Case
	if r.cases != nil {
		r.cases.mu.RLock()
		cases = cloneMemoryCases(r.cases.data)
		r.cases.mu.RUnlock()
	}

	var alerts map[string]*domain.Alert
	var alertByCustomer map[string][]string
	if r.alerts != nil {
		r.alerts.mu.RLock()
		alerts = cloneMemoryAlerts(r.alerts.data)
		alertByCustomer = cloneStringSlices(r.alerts.byCustomer)
		r.alerts.mu.RUnlock()
	}

	var reports map[string]*domain.STRReport
	var reportEvents []domain.STRReportEvent
	if r.reports != nil {
		r.reports.mu.RLock()
		reports = make(map[string]*domain.STRReport, len(r.reports.data))
		for id, report := range r.reports.data {
			reports[id] = cloneSTRReport(report)
		}
		reportEvents = cloneSTRReportEvents(r.reports.events)
		r.reports.mu.RUnlock()
	}

	r.audit.mu.RLock()
	auditEntries := cloneAuditEntries(r.audit.entries)
	auditNextID := r.audit.nextID
	r.audit.mu.RUnlock()

	var events map[string][]domain.CaseEvent
	var evidence map[string][]domain.CaseEvidence
	var checklist map[string]map[string]domain.CaseChecklistItem
	var work map[string]map[string]domain.CaseWorkItem
	var relationship map[string]*domain.CaseRelationship
	var relationshipEvents []domain.CaseRelationshipEvent
	if r.investigation != nil {
		r.investigation.mu.RLock()
		events = cloneEventSlices(r.investigation.events)
		evidence = cloneEvidenceSlices(r.investigation.evidence)
		checklist = cloneChecklist(r.investigation.checklist)
		work = cloneWork(r.investigation.work)
		relationship = cloneRelationships(r.investigation.relationship)
		relationshipEvents = cloneRelationshipEvents(r.investigation.relationshipEvents)
		r.investigation.mu.RUnlock()
	}

	var decisions map[string][]domain.AlertDecisionEvent
	if r.decisions != nil {
		r.decisions.mu.RLock()
		decisions = cloneDecisionSlices(r.decisions.decisions)
		r.decisions.mu.RUnlock()
	}

	var outboxEvents []domain.DurableEvent
	var outboxNextSequence int64
	if r.outbox != nil {
		r.outbox.mu.RLock()
		outboxEvents = make([]domain.DurableEvent, len(r.outbox.events))
		for i, event := range r.outbox.events {
			outboxEvents[i] = cloneDurableEvent(event)
		}
		outboxNextSequence = r.outbox.nextSequence
		r.outbox.mu.RUnlock()
	}

	var identity map[string][]domain.CustomerIdentityHistoryEntry
	var wave3Runs map[string]*domain.ScreeningRun
	var wave3Results map[string]*domain.ScreeningResultRecord
	var wave3History map[string][]domain.ScreeningResultHistoryEntry
	var wave3Sources map[string]domain.ScreeningSourceSnapshot
	var wave3Metadata map[string]*domain.BacktestMetadata
	var wave3Manifests map[string]*domain.TargetManifest
	var pendingData map[string]*domain.PendingEvaluation
	var pendingHistory map[string][]domain.PendingEvaluationHistoryEntry
	var batchData map[string]*domain.BatchRun
	var backtestData map[string]*domain.BacktestJob
	var backtestSnapshots map[string][]string
	var backtestOutcomes map[string][]domain.BacktestOutcomeDetail
	if r.wave3 != nil {
		r.wave3.mu.RLock()
		identity = cloneIdentityHistory(r.wave3.identity)
		wave3Runs = cloneScreeningRuns(r.wave3.runs)
		wave3Results = cloneScreeningResults(r.wave3.results)
		wave3History = cloneScreeningHistory(r.wave3.history)
		wave3Sources = cloneScreeningSources(r.wave3.snapshots)
		wave3Metadata = cloneBacktestMetadata(r.wave3.metadata)
		wave3Manifests = cloneTargetManifests(r.wave3.manifests)
		r.wave3.mu.RUnlock()
	}
	if r.pending != nil {
		r.pending.mu.RLock()
		pendingData = clonePendingEvaluations(r.pending.data)
		pendingHistory = clonePendingHistory(r.pending.history)
		r.pending.mu.RUnlock()
	}
	if r.batch != nil {
		r.batch.mu.RLock()
		batchData = cloneBatchRuns(r.batch.data)
		r.batch.mu.RUnlock()
	}
	if r.backtest != nil {
		r.backtest.mu.Lock()
		backtestData = cloneBacktestJobs(r.backtest.data)
		backtestSnapshots = cloneStringSlices(r.backtest.snapshots)
		backtestOutcomes = cloneBacktestOutcomeDetails(r.backtest.outcomeDetails)
		r.backtest.mu.Unlock()
	}
	var reviewData map[string]*domain.CustomerReview
	var reviewByKey map[string]string
	if r.reviews != nil {
		r.reviews.mu.RLock()
		reviewData = make(map[string]*domain.CustomerReview, len(r.reviews.data))
		for id, value := range r.reviews.data {
			reviewData[id] = cloneCustomerReview(value)
		}
		reviewByKey = cloneStringMap(r.reviews.byKey)
		r.reviews.mu.RUnlock()
	}

	return memoryAtomicSnapshot{customers: customers, customerExternal: customerExternal, customerScores: customerScores,
		transactions: transactions, transactionByCust: transactionByCust, transactionIdem: transactionIdem,
		cases: cases, alerts: alerts, alertByCustomer: alertByCustomer, reports: reports, reportEvents: reportEvents,
		auditEntries: auditEntries, auditNextID: auditNextID, events: events, evidence: evidence,
		checklist: checklist, work: work, relationship: relationship, relationshipEvents: relationshipEvents, decisions: decisions,
		outboxEvents: outboxEvents, outboxNextSequence: outboxNextSequence, identity: identity,
		wave3Runs: wave3Runs, wave3Results: wave3Results, wave3History: wave3History,
		wave3Sources: wave3Sources, wave3Metadata: wave3Metadata, wave3Manifests: wave3Manifests,
		pendingData: pendingData, pendingHistory: pendingHistory, batchData: batchData,
		backtestData: backtestData, backtestSnapshots: backtestSnapshots, backtestOutcomes: backtestOutcomes, reviewData: reviewData, reviewByKey: reviewByKey}
}

func (r *MemoryAtomicMutationRepo) restore(snapshot memoryAtomicSnapshot) {
	r.customers.mu.Lock()
	r.customers.data = snapshot.customers
	r.customers.external = snapshot.customerExternal
	r.customers.scores = snapshot.customerScores
	r.customers.mu.Unlock()

	if r.transactions != nil {
		r.transactions.mu.Lock()
		r.transactions.data = snapshot.transactions
		r.transactions.byCustomer = snapshot.transactionByCust
		r.transactions.idempotencyKey = snapshot.transactionIdem
		r.transactions.mu.Unlock()
	}

	if r.cases != nil {
		r.cases.mu.Lock()
		r.cases.data = snapshot.cases
		r.cases.mu.Unlock()
	}
	if r.alerts != nil {
		r.alerts.mu.Lock()
		r.alerts.data = snapshot.alerts
		r.alerts.byCustomer = snapshot.alertByCustomer
		r.alerts.mu.Unlock()
	}
	if r.reports != nil {
		r.reports.mu.Lock()
		r.reports.data = snapshot.reports
		r.reports.events = snapshot.reportEvents
		r.reports.mu.Unlock()
	}
	r.audit.mu.Lock()
	r.audit.entries = snapshot.auditEntries
	r.audit.nextID = snapshot.auditNextID
	r.audit.mu.Unlock()
	if r.investigation != nil {
		r.investigation.mu.Lock()
		r.investigation.events = snapshot.events
		r.investigation.evidence = snapshot.evidence
		r.investigation.checklist = snapshot.checklist
		r.investigation.work = snapshot.work
		r.investigation.relationship = snapshot.relationship
		r.investigation.relationshipEvents = snapshot.relationshipEvents
		r.investigation.mu.Unlock()
	}
	if r.decisions != nil {
		r.decisions.mu.Lock()
		r.decisions.decisions = snapshot.decisions
		r.decisions.mu.Unlock()
	}
	if r.outbox != nil {
		r.outbox.mu.Lock()
		r.outbox.events = snapshot.outboxEvents
		r.outbox.nextSequence = snapshot.outboxNextSequence
		r.outbox.mu.Unlock()
	}
	if r.wave3 != nil {
		r.wave3.mu.Lock()
		r.wave3.identity = snapshot.identity
		r.wave3.runs = snapshot.wave3Runs
		r.wave3.results = snapshot.wave3Results
		r.wave3.history = snapshot.wave3History
		r.wave3.snapshots = snapshot.wave3Sources
		r.wave3.metadata = snapshot.wave3Metadata
		r.wave3.manifests = snapshot.wave3Manifests
		r.wave3.mu.Unlock()
	}
	if r.pending != nil {
		r.pending.mu.Lock()
		r.pending.data = snapshot.pendingData
		r.pending.history = snapshot.pendingHistory
		r.pending.mu.Unlock()
	}
	if r.batch != nil {
		r.batch.mu.Lock()
		r.batch.data = snapshot.batchData
		r.batch.mu.Unlock()
	}
	if r.backtest != nil {
		r.backtest.mu.Lock()
		r.backtest.data = snapshot.backtestData
		r.backtest.snapshots = snapshot.backtestSnapshots
		r.backtest.outcomeDetails = snapshot.backtestOutcomes
		r.backtest.mu.Unlock()
	}
	if r.reviews != nil {
		r.reviews.mu.Lock()
		r.reviews.data = snapshot.reviewData
		r.reviews.byKey = snapshot.reviewByKey
		r.reviews.mu.Unlock()
	}
}

func cloneMemoryCustomers(input map[string]*domain.Customer) map[string]*domain.Customer {
	output := make(map[string]*domain.Customer, len(input))
	for id, value := range input {
		if value == nil {
			continue
		}
		copy := *value
		copy.ProductTypes = append([]string(nil), value.ProductTypes...)
		if value.RiskScore != nil {
			score := *value.RiskScore
			copy.RiskScore = &score
		}
		if value.RiskTier != nil {
			tier := *value.RiskTier
			copy.RiskTier = &tier
		}
		if value.LastScoredAt != nil {
			at := *value.LastScoredAt
			copy.LastScoredAt = &at
		}
		if value.Attributes != nil {
			copy.Attributes = make(map[string]any, len(value.Attributes))
			for key, attribute := range value.Attributes {
				copy.Attributes[key] = attribute
			}
		}
		output[id] = &copy
	}
	return output
}

func cloneStringMap(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneScoreRecords(input map[string][]domain.ScoreRecord) map[string][]domain.ScoreRecord {
	output := make(map[string][]domain.ScoreRecord, len(input))
	for key, values := range input {
		output[key] = append([]domain.ScoreRecord(nil), values...)
	}
	return output
}

func cloneMemoryCases(input map[string]*domain.Case) map[string]*domain.Case {
	output := make(map[string]*domain.Case, len(input))
	for id, value := range input {
		if value == nil {
			continue
		}
		copy := *value
		copy.AlertIDs = append([]string(nil), value.AlertIDs...)
		copy.RelatedCaseIDs = append([]string(nil), value.RelatedCaseIDs...)
		copy.Notes = append([]domain.CaseNote(nil), value.Notes...)
		output[id] = &copy
	}
	return output
}

func cloneMemoryAlerts(input map[string]*domain.Alert) map[string]*domain.Alert {
	output := make(map[string]*domain.Alert, len(input))
	for id, value := range input {
		if value == nil {
			continue
		}
		copy := *value
		copy.TransactionIDs = append([]string(nil), value.TransactionIDs...)
		if value.ResolvedAt != nil {
			at := *value.ResolvedAt
			copy.ResolvedAt = &at
		}
		if value.DueAt != nil {
			at := *value.DueAt
			copy.DueAt = &at
		}
		output[id] = &copy
	}
	return output
}

func cloneStringSlices(input map[string][]string) map[string][]string {
	output := make(map[string][]string, len(input))
	for key, values := range input {
		output[key] = append([]string(nil), values...)
	}
	return output
}

func cloneMemoryTransactions(input map[string]*domain.Transaction) map[string]*domain.Transaction {
	output := make(map[string]*domain.Transaction, len(input))
	for id, value := range input {
		if value == nil {
			continue
		}
		copy := *value
		if value.AccountID != nil {
			accountID := *value.AccountID
			copy.AccountID = &accountID
		}
		if value.Counterparty != nil {
			counterparty := *value.Counterparty
			copy.Counterparty = &counterparty
		}
		copy.Metadata = copyAnyMap(value.Metadata)
		copy.TravelRuleEvidence = copyAnyMap(value.TravelRuleEvidence)
		if value.IdempotencyKey != nil {
			key := *value.IdempotencyKey
			copy.IdempotencyKey = &key
		}
		if value.TravelRuleApplicable != nil {
			applicable := *value.TravelRuleApplicable
			copy.TravelRuleApplicable = &applicable
		}
		output[id] = &copy
	}
	return output
}

func cloneIdentityHistory(input map[string][]domain.CustomerIdentityHistoryEntry) map[string][]domain.CustomerIdentityHistoryEntry {
	output := make(map[string][]domain.CustomerIdentityHistoryEntry, len(input))
	for key, values := range input {
		output[key] = make([]domain.CustomerIdentityHistoryEntry, len(values))
		for i, value := range values {
			output[key][i] = value
			if value.ChangedFields != nil {
				output[key][i].ChangedFields = copyAnyMap(value.ChangedFields)
			}
		}
	}
	return output
}

func cloneScreeningRuns(input map[string]*domain.ScreeningRun) map[string]*domain.ScreeningRun {
	output := make(map[string]*domain.ScreeningRun, len(input))
	for key, value := range input {
		output[key] = cloneRun(value)
	}
	return output
}

func cloneScreeningResults(input map[string]*domain.ScreeningResultRecord) map[string]*domain.ScreeningResultRecord {
	output := make(map[string]*domain.ScreeningResultRecord, len(input))
	for key, value := range input {
		output[key] = cloneScreeningRecord(value)
	}
	return output
}

func cloneScreeningHistory(input map[string][]domain.ScreeningResultHistoryEntry) map[string][]domain.ScreeningResultHistoryEntry {
	output := make(map[string][]domain.ScreeningResultHistoryEntry, len(input))
	for key, values := range input {
		output[key] = append([]domain.ScreeningResultHistoryEntry(nil), values...)
	}
	return output
}

func cloneScreeningSources(input map[string]domain.ScreeningSourceSnapshot) map[string]domain.ScreeningSourceSnapshot {
	output := make(map[string]domain.ScreeningSourceSnapshot, len(input))
	for key, value := range input {
		copy := value
		if value.LastAttemptAt != nil {
			at := *value.LastAttemptAt
			copy.LastAttemptAt = &at
		}
		if value.LastFailureAt != nil {
			at := *value.LastFailureAt
			copy.LastFailureAt = &at
		}
		if value.LastSuccessAt != nil {
			at := *value.LastSuccessAt
			copy.LastSuccessAt = &at
		}
		output[key] = copy
	}
	return output
}

func cloneBacktestMetadata(input map[string]*domain.BacktestMetadata) map[string]*domain.BacktestMetadata {
	output := make(map[string]*domain.BacktestMetadata, len(input))
	for key, value := range input {
		if value == nil {
			continue
		}
		copy := *value
		copy.CohortPreview = copyAnyMap(value.CohortPreview)
		copy.BaselineSnapshot = copyAnyMap(value.BaselineSnapshot)
		copy.CandidateSnapshot = copyAnyMap(value.CandidateSnapshot)
		output[key] = &copy
	}
	return output
}

func cloneTargetManifests(input map[string]*domain.TargetManifest) map[string]*domain.TargetManifest {
	output := make(map[string]*domain.TargetManifest, len(input))
	for key, value := range input {
		output[key] = cloneManifest(value)
	}
	return output
}

func clonePendingEvaluations(input map[string]*domain.PendingEvaluation) map[string]*domain.PendingEvaluation {
	output := make(map[string]*domain.PendingEvaluation, len(input))
	for key, value := range input {
		output[key] = clonePending(value)
	}
	return output
}

func clonePendingHistory(input map[string][]domain.PendingEvaluationHistoryEntry) map[string][]domain.PendingEvaluationHistoryEntry {
	output := make(map[string][]domain.PendingEvaluationHistoryEntry, len(input))
	for key, values := range input {
		output[key] = append([]domain.PendingEvaluationHistoryEntry(nil), values...)
	}
	return output
}

func cloneBatchRuns(input map[string]*domain.BatchRun) map[string]*domain.BatchRun {
	output := make(map[string]*domain.BatchRun, len(input))
	for key, value := range input {
		if value == nil {
			continue
		}
		copy := *value
		copy.ProcessedCustomerIDs = append([]string(nil), value.ProcessedCustomerIDs...)
		copy.Parameters = copyAnyMap(value.Parameters)
		copy.ConfigDigests = copyStringMap(value.ConfigDigests)
		copy.ResultCounts = copyIntMap(value.ResultCounts)
		copy.CustomerOutcomes = cloneBatchCustomerOutcomes(value.CustomerOutcomes)
		output[key] = &copy
	}
	return output
}

func cloneBacktestJobs(input map[string]*domain.BacktestJob) map[string]*domain.BacktestJob {
	output := make(map[string]*domain.BacktestJob, len(input))
	for key, value := range input {
		output[key] = cloneBacktestJob(value)
	}
	return output
}

func cloneBacktestOutcomeDetails(input map[string][]domain.BacktestOutcomeDetail) map[string][]domain.BacktestOutcomeDetail {
	output := make(map[string][]domain.BacktestOutcomeDetail, len(input))
	for key, values := range input {
		copyValues := make([]domain.BacktestOutcomeDetail, len(values))
		for i, value := range values {
			copyValues[i] = cloneBacktestOutcomeDetail(value)
		}
		output[key] = copyValues
	}
	return output
}

func cloneAuditEntries(input []domain.AuditEntry) []domain.AuditEntry {
	output := make([]domain.AuditEntry, len(input))
	for i, entry := range input {
		output[i] = entry
		if entry.Details != nil {
			output[i].Details = make(map[string]string, len(entry.Details))
			for key, value := range entry.Details {
				output[i].Details[key] = value
			}
		}
	}
	return output
}

func cloneEventSlices(input map[string][]domain.CaseEvent) map[string][]domain.CaseEvent {
	output := make(map[string][]domain.CaseEvent, len(input))
	for key, values := range input {
		for _, value := range values {
			output[key] = append(output[key], cloneEvent(value))
		}
	}
	return output
}

func cloneEvidenceSlices(input map[string][]domain.CaseEvidence) map[string][]domain.CaseEvidence {
	output := make(map[string][]domain.CaseEvidence, len(input))
	for key, values := range input {
		output[key] = append([]domain.CaseEvidence(nil), values...)
	}
	return output
}

func cloneChecklist(input map[string]map[string]domain.CaseChecklistItem) map[string]map[string]domain.CaseChecklistItem {
	output := make(map[string]map[string]domain.CaseChecklistItem, len(input))
	for caseID, values := range input {
		output[caseID] = make(map[string]domain.CaseChecklistItem, len(values))
		for key, value := range values {
			output[caseID][key] = value
		}
	}
	return output
}

func cloneWork(input map[string]map[string]domain.CaseWorkItem) map[string]map[string]domain.CaseWorkItem {
	output := make(map[string]map[string]domain.CaseWorkItem, len(input))
	for caseID, values := range input {
		output[caseID] = make(map[string]domain.CaseWorkItem, len(values))
		for key, value := range values {
			output[caseID][key] = value
		}
	}
	return output
}

func cloneRelationships(input map[string]*domain.CaseRelationship) map[string]*domain.CaseRelationship {
	output := make(map[string]*domain.CaseRelationship, len(input))
	for id, value := range input {
		if value == nil {
			continue
		}
		copy := *value
		if value.RemovedAt != nil {
			at := *value.RemovedAt
			copy.RemovedAt = &at
		}
		output[id] = &copy
	}
	return output
}

func cloneRelationshipEvents(input []domain.CaseRelationshipEvent) []domain.CaseRelationshipEvent {
	output := make([]domain.CaseRelationshipEvent, len(input))
	for i, event := range input {
		output[i] = cloneRelationshipEvent(event)
	}
	return output
}

func cloneDecisionSlices(input map[string][]domain.AlertDecisionEvent) map[string][]domain.AlertDecisionEvent {
	output := make(map[string][]domain.AlertDecisionEvent, len(input))
	for key, values := range input {
		output[key] = append([]domain.AlertDecisionEvent(nil), values...)
	}
	return output
}

func cloneSTRReportEvents(input []domain.STRReportEvent) []domain.STRReportEvent {
	output := make([]domain.STRReportEvent, len(input))
	for i, event := range input {
		output[i] = cloneSTRReportEvent(event)
	}
	return output
}
