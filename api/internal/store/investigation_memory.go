package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// MemoryCaseInvestigationRepo is the local/dev implementation of the
// append-only investigation file. Current checklist/work/relationship state is
// kept separately from the immutable event stream so corrections never erase
// the evidence of the prior value.
type MemoryCaseInvestigationRepo struct {
	mu                 sync.RWMutex
	appendEventFailure error
	evidenceFailure    error
	events             map[string][]domain.CaseEvent
	evidence           map[string][]domain.CaseEvidence
	checklist          map[string]map[string]domain.CaseChecklistItem
	work               map[string]map[string]domain.CaseWorkItem
	relationship       map[string]*domain.CaseRelationship
	relationshipEvents []domain.CaseRelationshipEvent
}

func (r *MemoryCaseInvestigationRepo) SetAppendEventFailure(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.appendEventFailure = err
}

func (r *MemoryCaseInvestigationRepo) SetEvidenceFailure(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evidenceFailure = err
}

func NewMemoryCaseInvestigationRepo() *MemoryCaseInvestigationRepo {
	return &MemoryCaseInvestigationRepo{
		events:       make(map[string][]domain.CaseEvent),
		evidence:     make(map[string][]domain.CaseEvidence),
		checklist:    make(map[string]map[string]domain.CaseChecklistItem),
		work:         make(map[string]map[string]domain.CaseWorkItem),
		relationship: make(map[string]*domain.CaseRelationship),
	}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneInvestigationValue(value)
	}
	return out
}

func cloneInvestigationValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneInvestigationValue(item)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}

func cloneEvent(event domain.CaseEvent) domain.CaseEvent {
	event.Before = cloneMap(event.Before)
	event.After = cloneMap(event.After)
	event.RelatedAlertIDs = append([]string(nil), event.RelatedAlertIDs...)
	event.RelatedCaseIDs = append([]string(nil), event.RelatedCaseIDs...)
	event.RelatedReportIDs = append([]string(nil), event.RelatedReportIDs...)
	return event
}

func normalizeCaseEventIdentifiers(event *domain.CaseEvent) {
	if event == nil {
		return
	}
	event.ID = domain.CanonicalIdentifier(event.ID)
	event.CaseID = domain.CanonicalIdentifier(event.CaseID)
	for i := range event.RelatedAlertIDs {
		event.RelatedAlertIDs[i] = domain.CanonicalUUID(event.RelatedAlertIDs[i])
	}
	for i := range event.RelatedCaseIDs {
		event.RelatedCaseIDs[i] = domain.CanonicalIdentifier(event.RelatedCaseIDs[i])
	}
	for i := range event.RelatedReportIDs {
		event.RelatedReportIDs[i] = domain.CanonicalIdentifier(event.RelatedReportIDs[i])
	}
}

func normalizeEvidenceIdentifiers(evidence *domain.CaseEvidence) {
	if evidence == nil {
		return
	}
	evidence.ID = domain.CanonicalIdentifier(evidence.ID)
	evidence.CaseID = domain.CanonicalIdentifier(evidence.CaseID)
	evidence.RootID = domain.CanonicalIdentifier(evidence.RootID)
	evidence.SupersedesID = domain.CanonicalIdentifier(evidence.SupersedesID)
}

func normalizeRelationshipIdentifiers(relationship *domain.CaseRelationship) {
	if relationship == nil {
		return
	}
	relationship.ID = domain.CanonicalIdentifier(relationship.ID)
	relationship.CaseID = domain.CanonicalIdentifier(relationship.CaseID)
	relationship.RelatedCaseID = domain.CanonicalIdentifier(relationship.RelatedCaseID)
}

func normalizeRelationshipEventIdentifiers(event *domain.CaseRelationshipEvent) {
	if event == nil {
		return
	}
	event.ID = domain.CanonicalIdentifier(event.ID)
	event.RelationshipID = domain.CanonicalIdentifier(event.RelationshipID)
	event.CaseID = domain.CanonicalIdentifier(event.CaseID)
	event.RelatedCaseID = domain.CanonicalIdentifier(event.RelatedCaseID)
}

func normalizeDecisionIdentifiers(event *domain.AlertDecisionEvent) {
	if event == nil {
		return
	}
	event.ID = domain.CanonicalIdentifier(event.ID)
	event.AlertID = domain.CanonicalUUID(event.AlertID)
	event.SupersedesID = domain.CanonicalIdentifier(event.SupersedesID)
}

func (r *MemoryCaseInvestigationRepo) AppendEvent(_ context.Context, event *domain.CaseEvent) error {
	if event == nil {
		return fmt.Errorf("case event is nil")
	}
	if strings.TrimSpace(event.ID) == "" {
		return fmt.Errorf("case event id is required")
	}
	normalizeCaseEventIdentifiers(event)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.appendEventFailure != nil {
		return r.appendEventFailure
	}
	for _, existing := range r.events[event.CaseID] {
		if existing.ID == event.ID {
			return &domain.ErrConflict{Entity: "case_event", ID: event.ID, Reason: "event already exists"}
		}
	}
	stored := cloneEvent(*event)
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	r.events[stored.CaseID] = append(r.events[stored.CaseID], stored)
	return nil
}

func (r *MemoryCaseInvestigationRepo) ListEvents(_ context.Context, caseID string) ([]domain.CaseEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	caseID = domain.CanonicalIdentifier(caseID)
	out := make([]domain.CaseEvent, 0, len(r.events[caseID]))
	for _, event := range r.events[caseID] {
		out = append(out, cloneEvent(event))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *MemoryCaseInvestigationRepo) ListEventsPage(_ context.Context, caseID string, filter domain.CaseEventPageFilter, limit, offset int, after *domain.Cursor) ([]domain.CaseEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	caseID = domain.CanonicalIdentifier(caseID)
	allowed := make(map[string]struct{}, len(filter.EventTypes))
	for _, eventType := range filter.EventTypes {
		allowed[strings.TrimSpace(eventType)] = struct{}{}
	}
	all := make([]domain.CaseEvent, 0, len(r.events[caseID]))
	for _, event := range r.events[caseID] {
		if len(allowed) > 0 {
			if _, ok := allowed[event.EventType]; !ok {
				continue
			}
		}
		if after != nil && !(event.CreatedAt.After(after.CreatedAt) || (event.CreatedAt.Equal(after.CreatedAt) && event.ID > after.ID)) {
			continue
		}
		all = append(all, cloneEvent(event))
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID < all[j].ID
		}
		return all[i].CreatedAt.Before(all[j].CreatedAt)
	})
	if offset > 0 {
		if offset >= len(all) {
			return []domain.CaseEvent{}, nil
		}
		all = all[offset:]
	}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func cloneRelationshipEvent(event domain.CaseRelationshipEvent) domain.CaseRelationshipEvent {
	event.Before = cloneMap(event.Before)
	event.After = cloneMap(event.After)
	return event
}

// AppendRelationshipEvent records the immutable history of a relationship
// projection change. It intentionally shares the event failure injection with
// the case timeline: a relationship mutation cannot commit without both
// audit streams.
func (r *MemoryCaseInvestigationRepo) AppendRelationshipEvent(_ context.Context, event *domain.CaseRelationshipEvent) error {
	if event == nil {
		return fmt.Errorf("relationship event is nil")
	}
	if strings.TrimSpace(event.ID) == "" {
		return fmt.Errorf("relationship event id is required")
	}
	normalizeRelationshipEventIdentifiers(event)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.appendEventFailure != nil {
		return r.appendEventFailure
	}
	for _, existing := range r.relationshipEvents {
		if existing.ID == event.ID {
			return &domain.ErrConflict{Entity: "case_relationship_event", ID: event.ID, Reason: "event already exists"}
		}
	}
	copy := cloneRelationshipEvent(*event)
	if copy.CreatedAt.IsZero() {
		copy.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	r.relationshipEvents = append(r.relationshipEvents, copy)
	return nil
}

func (r *MemoryCaseInvestigationRepo) ListRelationshipEvents(_ context.Context, relationshipID string) ([]domain.CaseRelationshipEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.CaseRelationshipEvent, 0)
	relationshipID = domain.CanonicalIdentifier(relationshipID)
	for _, event := range r.relationshipEvents {
		if relationshipID == "" || domain.SameIdentifier(event.RelationshipID, relationshipID) {
			result = append(result, cloneRelationshipEvent(event))
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

func (r *MemoryCaseInvestigationRepo) AddEvidence(_ context.Context, evidence *domain.CaseEvidence) error {
	if evidence == nil {
		return fmt.Errorf("case evidence is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.evidenceFailure != nil {
		return r.evidenceFailure
	}
	normalizeEvidenceIdentifiers(evidence)
	copy := *evidence
	if copy.RootID == "" {
		copy.RootID = copy.ID
	}
	if copy.Version <= 0 {
		copy.Version = 1
	}
	for _, existing := range r.evidence[copy.CaseID] {
		if existing.ID == copy.ID || (existing.RootID == copy.RootID && existing.Version == copy.Version) {
			return &domain.ErrConflict{Entity: "case_evidence", ID: copy.ID, Reason: "evidence version already exists"}
		}
	}
	evidence.Version = copy.Version
	evidence.RootID = copy.RootID
	r.evidence[copy.CaseID] = append(r.evidence[copy.CaseID], copy)
	return nil
}

func (r *MemoryCaseInvestigationRepo) CorrectEvidence(_ context.Context, evidence *domain.CaseEvidence, expectedCurrentID string) error {
	if evidence == nil {
		return fmt.Errorf("case evidence is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	normalizeEvidenceIdentifiers(evidence)
	evidence.CaseID = domain.CanonicalIdentifier(evidence.CaseID)
	expectedCurrentID = domain.CanonicalIdentifier(expectedCurrentID)
	var current *domain.CaseEvidence
	for i := range r.evidence[evidence.CaseID] {
		candidate := &r.evidence[evidence.CaseID][i]
		if candidate.ID == expectedCurrentID {
			copy := *candidate
			current = &copy
			break
		}
	}
	if current == nil {
		return &domain.ErrConflict{Entity: "case_evidence", ID: expectedCurrentID, Reason: "current evidence changed concurrently"}
	}
	if evidence.RootID == "" {
		evidence.RootID = current.RootID
	}
	if evidence.RootID != current.RootID || evidence.SupersedesID != current.ID || evidence.Version != current.Version+1 {
		return &domain.ErrConflict{Entity: "case_evidence", ID: expectedCurrentID, Reason: "expected version does not match current evidence"}
	}
	for _, existing := range r.evidence[evidence.CaseID] {
		if existing.RootID == evidence.RootID && existing.Version == evidence.Version {
			return &domain.ErrConflict{Entity: "case_evidence", ID: evidence.ID, Reason: "evidence version already exists"}
		}
	}
	copy := *evidence
	r.evidence[evidence.CaseID] = append(r.evidence[evidence.CaseID], copy)
	return nil
}

func (r *MemoryCaseInvestigationRepo) ListEvidence(_ context.Context, caseID string) ([]domain.CaseEvidence, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	caseID = domain.CanonicalIdentifier(caseID)
	out := append([]domain.CaseEvidence(nil), r.evidence[caseID]...)
	return out, nil
}

func (r *MemoryCaseInvestigationRepo) UpsertChecklist(_ context.Context, item *domain.CaseChecklistItem) error {
	item.CaseID = domain.CanonicalIdentifier(item.CaseID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.checklist[item.CaseID] == nil {
		r.checklist[item.CaseID] = make(map[string]domain.CaseChecklistItem)
	}
	copy := *item
	previous, exists := r.checklist[item.CaseID][item.Key]
	if exists {
		copy.Version = previous.Version + 1
		copy.CreatedAt = previous.CreatedAt
	} else if copy.Version <= 0 {
		copy.Version = 1
	}
	r.checklist[item.CaseID][item.Key] = copy
	return nil
}

func (r *MemoryCaseInvestigationRepo) ListChecklist(_ context.Context, caseID string) ([]domain.CaseChecklistItem, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	caseID = domain.CanonicalIdentifier(caseID)
	items := make([]domain.CaseChecklistItem, 0, len(r.checklist[caseID]))
	for _, item := range r.checklist[caseID] {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, nil
}

func (r *MemoryCaseInvestigationRepo) CreateWorkItem(_ context.Context, item *domain.CaseWorkItem) error {
	item.CaseID = domain.CanonicalIdentifier(item.CaseID)
	item.ID = domain.CanonicalIdentifier(item.ID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.work[item.CaseID] == nil {
		r.work[item.CaseID] = make(map[string]domain.CaseWorkItem)
	}
	if _, exists := r.work[item.CaseID][item.ID]; exists {
		return &domain.ErrConflict{Entity: "case_work_item", ID: item.ID, Reason: "already exists"}
	}
	r.work[item.CaseID][item.ID] = *item
	return nil
}

func (r *MemoryCaseInvestigationRepo) UpdateWorkItem(_ context.Context, item *domain.CaseWorkItem) error {
	item.CaseID = domain.CanonicalIdentifier(item.CaseID)
	item.ID = domain.CanonicalIdentifier(item.ID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.work[item.CaseID] == nil {
		return &domain.ErrNotFound{Entity: "case_work_item", ID: item.ID}
	}
	if _, exists := r.work[item.CaseID][item.ID]; !exists {
		return &domain.ErrNotFound{Entity: "case_work_item", ID: item.ID}
	}
	r.work[item.CaseID][item.ID] = *item
	return nil
}

func (r *MemoryCaseInvestigationRepo) ListWorkItems(_ context.Context, caseID string) ([]domain.CaseWorkItem, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	caseID = domain.CanonicalIdentifier(caseID)
	items := make([]domain.CaseWorkItem, 0, len(r.work[caseID]))
	for _, item := range r.work[caseID] {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (r *MemoryCaseInvestigationRepo) AddRelationship(_ context.Context, relationship *domain.CaseRelationship) error {
	if relationship == nil {
		return fmt.Errorf("case relationship is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	normalizeRelationshipIdentifiers(relationship)
	if domain.SameIdentifier(relationship.CaseID, relationship.RelatedCaseID) {
		return &domain.ErrConflict{Entity: "case_relationship", ID: relationship.CaseID, Reason: "self relationship is not allowed"}
	}
	if strings.TrimSpace(relationship.Rationale) == "" {
		return &domain.ErrConflict{Entity: "case_relationship", ID: relationship.RelatedCaseID, Reason: "rationale is required"}
	}
	for _, existing := range r.relationship {
		if existing.Active && domain.SameIdentifier(existing.CaseID, relationship.CaseID) && domain.SameIdentifier(existing.RelatedCaseID, relationship.RelatedCaseID) {
			return &domain.ErrConflict{Entity: "case_relationship", ID: relationship.RelatedCaseID, Reason: "duplicate active relationship"}
		}
	}
	copy := *relationship
	if copy.Source == "" {
		copy.Source = "manual"
	}
	copy.Active = true
	r.relationship[copy.ID] = &copy
	return nil
}

func (r *MemoryCaseInvestigationRepo) ListRelationships(_ context.Context, caseID string, includeInactive bool) ([]domain.CaseRelationship, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	caseID = domain.CanonicalIdentifier(caseID)
	out := make([]domain.CaseRelationship, 0)
	for _, relationship := range r.relationship {
		if !domain.SameIdentifier(relationship.CaseID, caseID) || (!includeInactive && !relationship.Active) {
			continue
		}
		out = append(out, *relationship)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *MemoryCaseInvestigationRepo) RemoveRelationship(_ context.Context, id, actor, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	id = domain.CanonicalIdentifier(id)
	relationship, ok := r.relationship[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "case_relationship", ID: id}
	}
	if strings.TrimSpace(reason) == "" {
		return &domain.ErrConflict{Entity: "case_relationship", ID: id, Reason: "removal reason is required"}
	}
	if !relationship.Active {
		return &domain.ErrConflict{Entity: "case_relationship", ID: id, Reason: "relationship is already inactive"}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	relationship.Active = false
	relationship.RemovedBy = actor
	relationship.RemovedAt = &now
	relationship.RemovalReason = strings.TrimSpace(reason)
	return nil
}

func (r *MemoryCaseInvestigationRepo) ReplaceRelationship(_ context.Context, currentID string, replacement *domain.CaseRelationship, actor, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	currentID = domain.CanonicalIdentifier(currentID)
	normalizeRelationshipIdentifiers(replacement)
	if replacement == nil {
		return &domain.ErrConflict{Entity: "case_relationship", ID: currentID, Reason: "replacement relationship is required"}
	}
	if strings.TrimSpace(reason) == "" {
		return &domain.ErrConflict{Entity: "case_relationship", ID: currentID, Reason: "correction reason is required"}
	}
	if strings.TrimSpace(replacement.Rationale) == "" {
		return &domain.ErrConflict{Entity: "case_relationship", ID: currentID, Reason: "replacement rationale is required"}
	}
	current, ok := r.relationship[currentID]
	if !ok {
		return &domain.ErrNotFound{Entity: "case_relationship", ID: currentID}
	}
	if !current.Active {
		return &domain.ErrConflict{Entity: "case_relationship", ID: currentID, Reason: "relationship is already inactive"}
	}
	if !domain.SameIdentifier(current.CaseID, replacement.CaseID) || !domain.SameIdentifier(current.RelatedCaseID, replacement.RelatedCaseID) {
		return &domain.ErrConflict{Entity: "case_relationship", ID: currentID, Reason: "correction cannot change the linked cases"}
	}
	if replacement.ID == "" || domain.SameIdentifier(replacement.ID, currentID) {
		return &domain.ErrConflict{Entity: "case_relationship", ID: currentID, Reason: "correction requires a new relationship id"}
	}
	if _, exists := r.relationship[replacement.ID]; exists {
		return &domain.ErrConflict{Entity: "case_relationship", ID: replacement.ID, Reason: "replacement relationship already exists"}
	}
	if domain.SameIdentifier(replacement.CaseID, replacement.RelatedCaseID) {
		return &domain.ErrConflict{Entity: "case_relationship", ID: replacement.CaseID, Reason: "self relationship is not allowed"}
	}
	for id, existing := range r.relationship {
		if !domain.SameIdentifier(id, currentID) && existing.Active && domain.SameIdentifier(existing.CaseID, replacement.CaseID) && domain.SameIdentifier(existing.RelatedCaseID, replacement.RelatedCaseID) {
			return &domain.ErrConflict{Entity: "case_relationship", ID: replacement.RelatedCaseID, Reason: "duplicate active relationship"}
		}
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	current.Active = false
	current.RemovedBy = actor
	current.RemovedAt = &now
	current.RemovalReason = strings.TrimSpace(reason)
	copy := *replacement
	copy.Active = true
	if copy.Source == "" {
		copy.Source = "manual"
	}
	if copy.CreatedAt.IsZero() {
		copy.CreatedAt = now
	}
	r.relationship[copy.ID] = &copy
	return nil
}

type MemoryAlertDecisionRepo struct {
	mu            sync.RWMutex
	createFailure error
	decisions     map[string][]domain.AlertDecisionEvent
}

func NewMemoryAlertDecisionRepo() *MemoryAlertDecisionRepo {
	return &MemoryAlertDecisionRepo{decisions: make(map[string][]domain.AlertDecisionEvent)}
}

func (r *MemoryAlertDecisionRepo) SetCreateFailure(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createFailure = err
}

func (r *MemoryAlertDecisionRepo) CreateDecision(_ context.Context, event *domain.AlertDecisionEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createFailure != nil {
		return r.createFailure
	}
	normalizeDecisionIdentifiers(event)
	copy := *event
	if copy.CreatedAt.IsZero() {
		copy.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	r.decisions[copy.AlertID] = append(r.decisions[copy.AlertID], copy)
	return nil
}

func (r *MemoryAlertDecisionRepo) ListDecisions(_ context.Context, alertID string) ([]domain.AlertDecisionEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	alertID = domain.CanonicalUUID(alertID)
	return append([]domain.AlertDecisionEvent(nil), r.decisions[alertID]...), nil
}
