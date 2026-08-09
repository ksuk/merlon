package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/domain"
)

type PgCaseInvestigationRepo struct {
	pool DBTX
}

func NewPgCaseInvestigationRepo(pool DBTX) *PgCaseInvestigationRepo {
	return &PgCaseInvestigationRepo{pool: pool}
}

func nullableJSONMap(value map[string]any) []byte {
	if value == nil {
		return []byte("{}")
	}
	b, _ := json.Marshal(value)
	return b
}

func (r *PgCaseInvestigationRepo) AppendEvent(ctx context.Context, event *domain.CaseEvent) error {
	if event == nil {
		return errors.New("case event is nil")
	}
	if strings.TrimSpace(event.ID) == "" {
		return errors.New("case event id is required")
	}
	normalizeCaseEventIdentifiers(event)
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO case_events
		(id, case_id, event_type, actor, reason, before_state, after_state, related_alert_ids, related_case_ids, related_report_ids, correlation_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		event.ID, event.CaseID, event.EventType, event.Actor, event.Reason,
		nullableJSONMap(event.Before), nullableJSONMap(event.After), nonNilStrings(event.RelatedAlertIDs),
		nonNilStrings(event.RelatedCaseIDs), nonNilStrings(event.RelatedReportIDs), event.CorrelationID, event.CreatedAt)
	if err != nil && isUniqueViolation(err) {
		return &domain.ErrConflict{Entity: "case_event", ID: event.ID, Reason: "event already exists"}
	}
	return err
}

func (r *PgCaseInvestigationRepo) AppendRelationshipEvent(ctx context.Context, event *domain.CaseRelationshipEvent) error {
	if event == nil {
		return errors.New("relationship event is nil")
	}
	if strings.TrimSpace(event.ID) == "" {
		return errors.New("relationship event id is required")
	}
	normalizeRelationshipEventIdentifiers(event)
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO case_relationship_events
		(id, relationship_id, case_id, related_case_id, event_type, actor, reason, before_state, after_state, correlation_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		event.ID, event.RelationshipID, event.CaseID, event.RelatedCaseID, event.EventType, event.Actor, event.Reason,
		nullableJSONMap(event.Before), nullableJSONMap(event.After), event.CorrelationID, event.CreatedAt)
	if err != nil && isUniqueViolation(err) {
		return &domain.ErrConflict{Entity: "case_relationship_event", ID: event.ID, Reason: "event already exists"}
	}
	return err
}

func (r *PgCaseInvestigationRepo) ListRelationshipEvents(ctx context.Context, relationshipID string) ([]domain.CaseRelationshipEvent, error) {
	relationshipID = domain.CanonicalIdentifier(relationshipID)
	rows, err := r.pool.Query(ctx, `SELECT id, relationship_id, case_id, related_case_id, event_type, actor, reason,
		before_state, after_state, correlation_id, created_at
		FROM case_relationship_events WHERE relationship_id=$1 ORDER BY created_at ASC, id ASC`, relationshipID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.CaseRelationshipEvent
	for rows.Next() {
		var event domain.CaseRelationshipEvent
		var before, after []byte
		if err := rows.Scan(&event.ID, &event.RelationshipID, &event.CaseID, &event.RelatedCaseID, &event.EventType,
			&event.Actor, &event.Reason, &before, &after, &event.CorrelationID, &event.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(before, &event.Before); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(after, &event.After); err != nil {
			return nil, err
		}
		normalizeRelationshipEventIdentifiers(&event)
		result = append(result, event)
	}
	return result, rows.Err()
}

func (r *PgCaseInvestigationRepo) ListEvents(ctx context.Context, caseID string) ([]domain.CaseEvent, error) {
	caseID = domain.CanonicalIdentifier(caseID)
	rows, err := r.pool.Query(ctx, `SELECT id, case_id, event_type, actor, reason, before_state, after_state,
		related_alert_ids, related_case_ids, related_report_ids, correlation_id, created_at
		FROM case_events WHERE case_id=$1 ORDER BY created_at ASC, id ASC`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CaseEvent
	for rows.Next() {
		var event domain.CaseEvent
		var before, after []byte
		if err := rows.Scan(&event.ID, &event.CaseID, &event.EventType, &event.Actor, &event.Reason, &before, &after,
			&event.RelatedAlertIDs, &event.RelatedCaseIDs, &event.RelatedReportIDs, &event.CorrelationID, &event.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(before, &event.Before); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(after, &event.After); err != nil {
			return nil, err
		}
		normalizeCaseEventIdentifiers(&event)
		out = append(out, event)
	}
	return out, rows.Err()
}

func (r *PgCaseInvestigationRepo) ListEventsPage(ctx context.Context, caseID string, filter domain.CaseEventPageFilter, limit, offset int, after *domain.Cursor) ([]domain.CaseEvent, error) {
	caseID = domain.CanonicalIdentifier(caseID)
	query := `SELECT id, case_id, event_type, actor, reason, before_state, after_state,
		related_alert_ids, related_case_ids, related_report_ids, correlation_id, created_at
		FROM case_events WHERE case_id=$1`
	args := []any{caseID}
	arg := 2
	if len(filter.EventTypes) > 0 {
		query += ` AND event_type = ANY($2::text[])`
		args = append(args, filter.EventTypes)
		arg++
	}
	if after != nil {
		query += ` AND (created_at, id) > ($` + fmt.Sprint(arg) + `, $` + fmt.Sprint(arg+1) + `)`
		args = append(args, after.CreatedAt, after.ID)
		arg += 2
	}
	query += ` ORDER BY created_at ASC, id ASC`
	if limit > 0 {
		query += ` LIMIT $` + fmt.Sprint(arg)
		args = append(args, limit)
		arg++
	}
	if offset > 0 {
		query += ` OFFSET $` + fmt.Sprint(arg)
		args = append(args, offset)
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CaseEvent
	for rows.Next() {
		var event domain.CaseEvent
		var before, afterState []byte
		if err := rows.Scan(&event.ID, &event.CaseID, &event.EventType, &event.Actor, &event.Reason, &before, &afterState,
			&event.RelatedAlertIDs, &event.RelatedCaseIDs, &event.RelatedReportIDs, &event.CorrelationID, &event.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(before, &event.Before); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(afterState, &event.After); err != nil {
			return nil, err
		}
		normalizeCaseEventIdentifiers(&event)
		out = append(out, event)
	}
	return out, rows.Err()
}

func (r *PgCaseInvestigationRepo) AddEvidence(ctx context.Context, evidence *domain.CaseEvidence) error {
	if evidence == nil {
		return errors.New("case evidence is nil")
	}
	if evidence.RootID == "" {
		evidence.RootID = evidence.ID
	}
	normalizeEvidenceIdentifiers(evidence)
	if evidence.Version <= 0 {
		evidence.Version = 1
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO case_evidence
		(id, case_id, root_id, supersedes_id, description, source, evidence_type, collected_at, collected_by, integrity_hash, version, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, evidence.ID, evidence.CaseID, evidence.RootID, nullableString(evidence.SupersedesID), evidence.Description,
		evidence.Source, evidence.EvidenceType, evidence.CollectedAt, evidence.CollectedBy, evidence.IntegrityHash,
		evidence.Version, evidence.CreatedAt)
	if err != nil && isUniqueViolation(err) {
		return &domain.ErrConflict{Entity: "case_evidence", ID: evidence.ID, Reason: "evidence version already exists"}
	}
	return err
}

func (r *PgCaseInvestigationRepo) CorrectEvidence(ctx context.Context, evidence *domain.CaseEvidence, expectedCurrentID string) error {
	if evidence == nil {
		return errors.New("case evidence is nil")
	}
	normalizeEvidenceIdentifiers(evidence)
	expectedCurrentID = domain.CanonicalIdentifier(expectedCurrentID)
	if evidence.RootID == "" || evidence.SupersedesID == "" || evidence.Version <= 1 {
		return &domain.ErrConflict{Entity: "case_evidence", ID: expectedCurrentID, Reason: "correction lineage is incomplete"}
	}
	if evidence.SupersedesID != expectedCurrentID {
		return &domain.ErrConflict{Entity: "case_evidence", ID: expectedCurrentID, Reason: "expected evidence changed concurrently"}
	}
	if evidence.CreatedAt.IsZero() {
		evidence.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	tag, err := r.pool.Exec(ctx, `INSERT INTO case_evidence
		(id, case_id, root_id, supersedes_id, description, source, evidence_type, collected_at, collected_by, integrity_hash, version, created_at)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12
		WHERE EXISTS (
			SELECT 1 FROM case_evidence
			WHERE id=$4 AND case_id=$2 AND root_id=$3 AND version=$11-1
		)`, evidence.ID, evidence.CaseID, evidence.RootID, evidence.SupersedesID, evidence.Description,
		evidence.Source, evidence.EvidenceType, evidence.CollectedAt, evidence.CollectedBy, evidence.IntegrityHash,
		evidence.Version, evidence.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return &domain.ErrConflict{Entity: "case_evidence", ID: evidence.ID, Reason: "evidence version already exists"}
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrConflict{Entity: "case_evidence", ID: expectedCurrentID, Reason: "current evidence changed concurrently"}
	}
	return nil
}

func (r *PgCaseInvestigationRepo) ListEvidence(ctx context.Context, caseID string) ([]domain.CaseEvidence, error) {
	caseID = domain.CanonicalIdentifier(caseID)
	rows, err := r.pool.Query(ctx, `SELECT id, case_id, root_id, supersedes_id, description, source, evidence_type, collected_at, collected_by, integrity_hash, version, created_at
		FROM case_evidence WHERE case_id=$1 ORDER BY created_at ASC, id ASC`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CaseEvidence
	for rows.Next() {
		var item domain.CaseEvidence
		var supersedesID *string
		if err := rows.Scan(&item.ID, &item.CaseID, &item.RootID, &supersedesID, &item.Description, &item.Source, &item.EvidenceType, &item.CollectedAt,
			&item.CollectedBy, &item.IntegrityHash, &item.Version, &item.CreatedAt); err != nil {
			return nil, err
		}
		if supersedesID != nil {
			item.SupersedesID = *supersedesID
		}
		normalizeEvidenceIdentifiers(&item)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *PgCaseInvestigationRepo) UpsertChecklist(ctx context.Context, item *domain.CaseChecklistItem) error {
	item.CaseID = domain.CanonicalIdentifier(item.CaseID)
	if item.Version <= 0 {
		item.Version = 1
	}
	row := r.pool.QueryRow(ctx, `INSERT INTO case_checklist_items
		(case_id, item_key, label, completed, completed_by, completed_at, version, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (case_id,item_key) DO UPDATE SET label=EXCLUDED.label, completed=EXCLUDED.completed,
		completed_by=EXCLUDED.completed_by, completed_at=EXCLUDED.completed_at, version=case_checklist_items.version+1,
		updated_at=EXCLUDED.updated_at
		RETURNING case_id, item_key, label, completed, completed_by, completed_at, version, created_at, updated_at`,
		item.CaseID, item.Key, item.Label, item.Completed, item.CompletedBy, item.CompletedAt, item.Version, item.CreatedAt, item.UpdatedAt)
	return row.Scan(&item.CaseID, &item.Key, &item.Label, &item.Completed, &item.CompletedBy, &item.CompletedAt, &item.Version, &item.CreatedAt, &item.UpdatedAt)
}

func (r *PgCaseInvestigationRepo) ListChecklist(ctx context.Context, caseID string) ([]domain.CaseChecklistItem, error) {
	caseID = domain.CanonicalIdentifier(caseID)
	rows, err := r.pool.Query(ctx, `SELECT case_id, item_key, label, completed, completed_by, completed_at, version, created_at, updated_at
		FROM case_checklist_items WHERE case_id=$1 ORDER BY item_key`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CaseChecklistItem
	for rows.Next() {
		var item domain.CaseChecklistItem
		var completedAt *time.Time
		if err := rows.Scan(&item.CaseID, &item.Key, &item.Label, &item.Completed, &item.CompletedBy, &completedAt,
			&item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.CompletedAt = completedAt
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *PgCaseInvestigationRepo) CreateWorkItem(ctx context.Context, item *domain.CaseWorkItem) error {
	item.ID = domain.CanonicalIdentifier(item.ID)
	item.CaseID = domain.CanonicalIdentifier(item.CaseID)
	_, err := r.pool.Exec(ctx, `INSERT INTO case_work_items
		(id, case_id, title, description, status, assigned_to, due_at, completed_by, completed_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, item.ID, item.CaseID, item.Title, item.Description, item.Status,
		item.AssignedTo, item.DueAt, item.CompletedBy, item.CompletedAt, item.CreatedAt, item.UpdatedAt)
	return err
}

func (r *PgCaseInvestigationRepo) UpdateWorkItem(ctx context.Context, item *domain.CaseWorkItem) error {
	item.ID = domain.CanonicalIdentifier(item.ID)
	item.CaseID = domain.CanonicalIdentifier(item.CaseID)
	tag, err := r.pool.Exec(ctx, `UPDATE case_work_items SET title=$2, description=$3, status=$4, assigned_to=$5, due_at=$6,
		completed_by=$7, completed_at=$8, updated_at=$9 WHERE id=$1 AND case_id=$10`, item.ID, item.Title, item.Description,
		item.Status, item.AssignedTo, item.DueAt, item.CompletedBy, item.CompletedAt, item.UpdatedAt, item.CaseID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "case_work_item", ID: item.ID}
	}
	return nil
}

func (r *PgCaseInvestigationRepo) ListWorkItems(ctx context.Context, caseID string) ([]domain.CaseWorkItem, error) {
	caseID = domain.CanonicalIdentifier(caseID)
	rows, err := r.pool.Query(ctx, `SELECT id, case_id, title, description, status, assigned_to, due_at, completed_by, completed_at, created_at, updated_at
		FROM case_work_items WHERE case_id=$1 ORDER BY created_at ASC, id ASC`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CaseWorkItem
	for rows.Next() {
		var item domain.CaseWorkItem
		var dueAt, completedAt *time.Time
		if err := rows.Scan(&item.ID, &item.CaseID, &item.Title, &item.Description, &item.Status, &item.AssignedTo, &dueAt,
			&item.CompletedBy, &completedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.DueAt = dueAt
		item.CompletedAt = completedAt
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *PgCaseInvestigationRepo) AddRelationship(ctx context.Context, relationship *domain.CaseRelationship) error {
	if relationship == nil {
		return errors.New("case relationship is nil")
	}
	normalizeRelationshipIdentifiers(relationship)
	if domain.SameIdentifier(relationship.CaseID, relationship.RelatedCaseID) {
		return &domain.ErrConflict{Entity: "case_relationship", ID: relationship.CaseID, Reason: "self relationship is not allowed"}
	}
	if strings.TrimSpace(relationship.Rationale) == "" {
		return &domain.ErrConflict{Entity: "case_relationship", ID: relationship.RelatedCaseID, Reason: "rationale is required"}
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO case_relationships
		(id, case_id, related_case_id, relationship_type, rationale, created_by, created_at, active, source)
		VALUES ($1,$2,$3,$4,$5,$6,$7,TRUE,$8)`, relationship.ID, relationship.CaseID, relationship.RelatedCaseID,
		relationship.RelationshipType, relationship.Rationale, relationship.CreatedBy, relationship.CreatedAt, relationship.Source)
	if err != nil && isCaseRelationshipUniqueViolation(err) {
		return &domain.ErrConflict{Entity: "case_relationship", ID: relationship.RelatedCaseID, Reason: "duplicate active relationship"}
	}
	return err
}

func isCaseRelationshipUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "idx_case_relationships_active_unique") || strings.Contains(message, "case_relationships_active")
}

func (r *PgCaseInvestigationRepo) ListRelationships(ctx context.Context, caseID string, includeInactive bool) ([]domain.CaseRelationship, error) {
	caseID = domain.CanonicalIdentifier(caseID)
	query := `SELECT id, case_id, related_case_id, relationship_type, rationale, created_by, created_at, active, removed_by, removed_at, removal_reason, source
		FROM case_relationships WHERE case_id=$1`
	if !includeInactive {
		query += ` AND active`
	}
	query += ` ORDER BY created_at ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CaseRelationship
	for rows.Next() {
		var relationship domain.CaseRelationship
		if err := rows.Scan(&relationship.ID, &relationship.CaseID, &relationship.RelatedCaseID, &relationship.RelationshipType,
			&relationship.Rationale, &relationship.CreatedBy, &relationship.CreatedAt, &relationship.Active, &relationship.RemovedBy,
			&relationship.RemovedAt, &relationship.RemovalReason, &relationship.Source); err != nil {
			return nil, err
		}
		normalizeRelationshipIdentifiers(&relationship)
		out = append(out, relationship)
	}
	return out, rows.Err()
}

func (r *PgCaseInvestigationRepo) RemoveRelationship(ctx context.Context, id, actor, reason string) error {
	id = domain.CanonicalIdentifier(id)
	if strings.TrimSpace(reason) == "" {
		return &domain.ErrConflict{Entity: "case_relationship", ID: id, Reason: "removal reason is required"}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	tag, err := r.pool.Exec(ctx, `UPDATE case_relationships SET active=FALSE, removed_by=$2, removed_at=$3, removal_reason=$4 WHERE id=$1 AND active`, id, actor, now, strings.TrimSpace(reason))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM case_relationships WHERE id=$1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return &domain.ErrNotFound{Entity: "case_relationship", ID: id}
		}
		return &domain.ErrConflict{Entity: "case_relationship", ID: id, Reason: "relationship is already inactive"}
	}
	return nil
}

func (r *PgCaseInvestigationRepo) ReplaceRelationship(ctx context.Context, currentID string, replacement *domain.CaseRelationship, actor, reason string) error {
	if replacement == nil {
		return &domain.ErrConflict{Entity: "case_relationship", ID: currentID, Reason: "replacement relationship is required"}
	}
	currentID = domain.CanonicalIdentifier(currentID)
	normalizeRelationshipIdentifiers(replacement)
	if strings.TrimSpace(reason) == "" {
		return &domain.ErrConflict{Entity: "case_relationship", ID: currentID, Reason: "correction reason is required"}
	}
	if strings.TrimSpace(replacement.Rationale) == "" {
		return &domain.ErrConflict{Entity: "case_relationship", ID: currentID, Reason: "replacement rationale is required"}
	}
	if replacement.ID == "" || domain.SameIdentifier(replacement.ID, currentID) {
		return &domain.ErrConflict{Entity: "case_relationship", ID: currentID, Reason: "correction requires a new relationship id"}
	}
	if domain.SameIdentifier(replacement.CaseID, replacement.RelatedCaseID) {
		return &domain.ErrConflict{Entity: "case_relationship", ID: replacement.CaseID, Reason: "self relationship is not allowed"}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var currentCaseID, currentRelatedCaseID string
	var active bool
	row := tx.QueryRow(ctx, `SELECT case_id, related_case_id, active FROM case_relationships WHERE id=$1 FOR UPDATE`, currentID)
	err = row.Scan(&currentCaseID, &currentRelatedCaseID, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return &domain.ErrNotFound{Entity: "case_relationship", ID: currentID}
	}
	if err != nil {
		return err
	}
	if !active {
		return &domain.ErrConflict{Entity: "case_relationship", ID: currentID, Reason: "relationship is already inactive"}
	}
	if !domain.SameIdentifier(currentCaseID, replacement.CaseID) || !domain.SameIdentifier(currentRelatedCaseID, replacement.RelatedCaseID) {
		return &domain.ErrConflict{Entity: "case_relationship", ID: currentID, Reason: "correction cannot change the linked cases"}
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := tx.Exec(ctx, `UPDATE case_relationships SET active=FALSE, removed_by=$2, removed_at=$3, removal_reason=$4 WHERE id=$1 AND active`, currentID, actor, now, strings.TrimSpace(reason)); err != nil {
		return err
	}
	createdAt := replacement.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	source := replacement.Source
	if source == "" {
		source = "manual"
	}
	_, err = tx.Exec(ctx, `INSERT INTO case_relationships
		(id, case_id, related_case_id, relationship_type, rationale, created_by, created_at, active, source)
		VALUES ($1,$2,$3,$4,$5,$6,$7,TRUE,$8)`, replacement.ID, replacement.CaseID, replacement.RelatedCaseID,
		replacement.RelationshipType, replacement.Rationale, replacement.CreatedBy, createdAt, source)
	if err != nil {
		if isCaseRelationshipUniqueViolation(err) {
			return &domain.ErrConflict{Entity: "case_relationship", ID: replacement.RelatedCaseID, Reason: "duplicate active relationship"}
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

type PgAlertDecisionRepo struct {
	pool DBTX
}

func NewPgAlertDecisionRepo(pool DBTX) *PgAlertDecisionRepo {
	return &PgAlertDecisionRepo{pool: pool}
}

func (r *PgAlertDecisionRepo) CreateDecision(ctx context.Context, event *domain.AlertDecisionEvent) error {
	normalizeDecisionIdentifiers(event)
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO alert_decision_events
		(id, alert_id, from_status, to_status, outcome, rationale, actor, supersedes_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, event.ID, event.AlertID, event.FromStatus, event.ToStatus, event.Outcome,
		event.Rationale, event.Actor, nullableString(event.SupersedesID), event.CreatedAt)
	return err
}

func (r *PgAlertDecisionRepo) ListDecisions(ctx context.Context, alertID string) ([]domain.AlertDecisionEvent, error) {
	alertID = domain.CanonicalUUID(alertID)
	rows, err := r.pool.Query(ctx, `SELECT id, alert_id, from_status, to_status, outcome, rationale, actor, supersedes_id, created_at
		FROM alert_decision_events WHERE alert_id=$1 ORDER BY created_at ASC, id ASC`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AlertDecisionEvent
	for rows.Next() {
		var event domain.AlertDecisionEvent
		var supersedesID *string
		if err := rows.Scan(&event.ID, &event.AlertID, &event.FromStatus, &event.ToStatus, &event.Outcome, &event.Rationale,
			&event.Actor, &supersedesID, &event.CreatedAt); err != nil {
			return nil, err
		}
		if supersedesID != nil {
			event.SupersedesID = *supersedesID
		}
		normalizeDecisionIdentifiers(&event)
		out = append(out, event)
	}
	return out, rows.Err()
}
