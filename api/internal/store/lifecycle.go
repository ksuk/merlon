package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/domain"
)

// MemoryCaseAlertLifecycleRepo coordinates the in-memory case and alert
// repositories. It is deliberately a separate adapter so the ordinary CRUD
// interfaces remain small while tests and local development still exercise
// the same all-or-nothing lifecycle path as PostgreSQL.
type MemoryCaseAlertLifecycleRepo struct {
	cases  *MemoryCaseRepo
	alerts *MemoryAlertRepo
}

func NewMemoryCaseAlertLifecycleRepo(cases *MemoryCaseRepo, alerts *MemoryAlertRepo) *MemoryCaseAlertLifecycleRepo {
	return &MemoryCaseAlertLifecycleRepo{cases: cases, alerts: alerts}
}

func (r *MemoryCaseAlertLifecycleRepo) CreateCaseWithAlerts(ctx context.Context, c *domain.Case) error {
	if r == nil || r.cases == nil || r.alerts == nil {
		return errors.New("case/alert lifecycle repository is not configured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	normalizeMemoryCase(c)
	r.cases.mu.Lock()
	defer r.cases.mu.Unlock()
	r.alerts.mu.Lock()
	defer r.alerts.mu.Unlock()

	if _, exists := r.cases.data[c.ID]; exists {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "already exists"}
	}
	if !domain.IsCaseUnresolved(c.Status) {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "new alert links require an active case"}
	}
	if err := validateMemoryAlertLinks(r.alerts.data, c.CustomerID, c.Status, c.AlertIDs); err != nil {
		return err
	}
	stored := *c
	stored.AlertIDs = append([]string(nil), c.AlertIDs...)
	r.cases.data[c.ID] = &stored
	return nil
}

func (r *MemoryCaseAlertLifecycleRepo) AppendAlerts(ctx context.Context, caseID string, expectedUpdatedAt time.Time, alertIDs []string) (*domain.Case, error) {
	if r == nil || r.cases == nil || r.alerts == nil {
		return nil, errors.New("case/alert lifecycle repository is not configured")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	caseID = domain.CanonicalIdentifier(caseID)
	for i := range alertIDs {
		alertIDs[i] = domain.CanonicalUUID(alertIDs[i])
	}
	r.cases.mu.Lock()
	defer r.cases.mu.Unlock()
	r.alerts.mu.Lock()
	defer r.alerts.mu.Unlock()

	current, ok := r.cases.data[caseID]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "case", ID: caseID}
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return nil, &domain.ErrConflict{Entity: "case", ID: caseID, Reason: "updated_at mismatch"}
	}
	if !domain.IsCaseUnresolved(current.Status) {
		return nil, &domain.ErrConflict{Entity: "case", ID: caseID, Reason: "terminal case cannot receive alerts"}
	}

	existing := make(map[string]struct{}, len(current.AlertIDs))
	for _, id := range current.AlertIDs {
		existing[id] = struct{}{}
	}
	var additions []string
	requestSeen := make(map[string]struct{}, len(alertIDs))
	for _, id := range alertIDs {
		if _, duplicate := requestSeen[id]; duplicate {
			return nil, fmt.Errorf("duplicate linked alert: %s", id)
		}
		requestSeen[id] = struct{}{}
		if _, alreadyLinked := existing[id]; alreadyLinked {
			continue
		}
		additions = append(additions, id)
	}
	if err := validateMemoryAlertLinks(r.alerts.data, current.CustomerID, current.Status, additions); err != nil {
		return nil, err
	}

	updated := *current
	updated.AlertIDs = append(append([]string(nil), current.AlertIDs...), additions...)
	if len(additions) > 0 {
		updated.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
		r.cases.data[caseID] = &updated
	}
	return &updated, nil
}

func validateMemoryAlertLinks(alerts map[string]*domain.Alert, customerID string, caseStatus domain.CaseStatus, alertIDs []string) error {
	seen := make(map[string]struct{}, len(alertIDs))
	for _, id := range alertIDs {
		id = domain.CanonicalUUID(id)
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("duplicate linked alert: %s", id)
		}
		seen[id] = struct{}{}
		a, ok := alerts[id]
		if !ok {
			return &domain.ErrNotFound{Entity: "alert", ID: id}
		}
		if !domain.SameIdentifier(a.CustomerID, customerID) {
			return &domain.ErrConflict{Entity: "alert", ID: id, Reason: "alert belongs to a different customer"}
		}
		if !domain.CanAttachAlertToCase(caseStatus, a.Status) {
			return &domain.ErrConflict{Entity: "alert", ID: id, Reason: "alert is not active or case cannot receive alerts"}
		}
	}
	return nil
}

func (r *MemoryCaseAlertLifecycleRepo) UpdateCaseAndAlerts(ctx context.Context, c *domain.Case, expectedUpdatedAt time.Time, transitions []domain.AlertStatusTransition) error {
	if r == nil || r.cases == nil || r.alerts == nil {
		return errors.New("case/alert lifecycle repository is not configured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	normalizeMemoryCase(c)
	for i := range transitions {
		transitions[i].ID = domain.CanonicalUUID(transitions[i].ID)
	}

	// Always lock in case-then-alert order. No other repository method takes
	// both locks, so this ordering prevents a cross-repository deadlock while
	// making validation and mutation one memory transaction.
	r.cases.mu.Lock()
	defer r.cases.mu.Unlock()
	r.alerts.mu.Lock()
	defer r.alerts.mu.Unlock()

	currentCase, ok := r.cases.data[c.ID]
	if !ok {
		return &domain.ErrNotFound{Entity: "case", ID: c.ID}
	}
	if !currentCase.UpdatedAt.Equal(expectedUpdatedAt) {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "updated_at mismatch"}
	}
	if currentCase.Status != c.Status && !domain.ValidCaseStatusTransition(currentCase.Status, c.Status) {
		return &domain.ErrInvalidStateTransition{Entity: "case", ID: c.ID, From: string(currentCase.Status), To: string(c.Status)}
	}
	if !domain.SameIdentifier(currentCase.CustomerID, c.CustomerID) {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "customer_id is immutable"}
	}
	if !sameIdentifierSlices(currentCase.AlertIDs, c.AlertIDs) {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "alert_ids can only be changed through lifecycle link operations"}
	}

	linked := make(map[string]struct{}, len(currentCase.AlertIDs))
	for _, alertID := range currentCase.AlertIDs {
		alertID = domain.CanonicalUUID(alertID)
		if _, duplicate := linked[alertID]; duplicate {
			return fmt.Errorf("duplicate linked alert: %s", alertID)
		}
		linked[alertID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(transitions))
	for _, transition := range transitions {
		if _, duplicate := seen[transition.ID]; duplicate {
			return fmt.Errorf("duplicate linked alert: %s", transition.ID)
		}
		seen[transition.ID] = struct{}{}
		if _, isLinked := linked[transition.ID]; !isLinked {
			return fmt.Errorf("alert is not linked to case: %s", transition.ID)
		}

		a, ok := r.alerts.data[transition.ID]
		if !ok {
			return &domain.ErrNotFound{Entity: "alert", ID: transition.ID}
		}
		if !a.UpdatedAt.Equal(transition.ExpectedUpdatedAt) || a.Status != transition.From {
			return &domain.ErrConflict{Entity: "alert", ID: transition.ID, Reason: "linked alert changed concurrently"}
		}
		if !domain.SameIdentifier(a.CustomerID, currentCase.CustomerID) {
			return &domain.ErrConflict{Entity: "alert", ID: transition.ID, Reason: "linked alert belongs to a different customer"}
		}
		if transition.From != transition.To && !domain.ValidAlertStatusTransition(transition.From, transition.To) &&
			!(transition.ResolvedBy == "case-filing" && domain.ValidAlertStatusTransitionForCaseFiling(transition.From, transition.To)) {
			return &domain.ErrInvalidStateTransition{Entity: "alert", ID: transition.ID, From: string(transition.From), To: string(transition.To)}
		}
		if transition.From != transition.To && domain.IsAlertTerminal(transition.To) && strings.TrimSpace(transition.ResolvedBy) == "" {
			return fmt.Errorf("resolved_by is required for terminal alert status")
		}
		if !domain.CompatibleCaseAlertState(c.Status, transition.To) {
			return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "linked alert state is incompatible with target case status"}
		}
	}
	if len(seen) != len(linked) {
		return fmt.Errorf("every linked alert must be validated")
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	c.UpdatedAt = now
	r.cases.data[c.ID] = c
	for _, transition := range transitions {
		if transition.From == transition.To {
			continue
		}
		a := r.alerts.data[transition.ID]
		a.Status = transition.To
		a.UpdatedAt = now
		if domain.IsAlertTerminal(transition.To) {
			a.ResolvedBy = transition.ResolvedBy
			a.ResolvedAt = &now
			a.Disposition = string(transition.To)
			a.DispositionRationale = strings.TrimSpace(transition.Rationale)
		} else {
			a.ResolvedBy = ""
			a.ResolvedAt = nil
		}
	}
	return nil
}

// PgCaseAlertLifecycleRepo applies the same coordinated update in a single
// PostgreSQL transaction. It accepts DBTX so migration/seed tests can supply
// either a pool or a transaction-compatible test double.
type PgCaseAlertLifecycleRepo struct {
	pool DBTX
}

func NewPgCaseAlertLifecycleRepo(pool DBTX) *PgCaseAlertLifecycleRepo {
	return &PgCaseAlertLifecycleRepo{pool: pool}
}

func (r *PgCaseAlertLifecycleRepo) CreateCaseWithAlerts(ctx context.Context, c *domain.Case) error {
	if !domain.IsCaseUnresolved(c.Status) {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "new alert links require an active case"}
	}
	if err := rejectDuplicateAlertIDs(c.AlertIDs); err != nil {
		return err
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
	if err := validatePgAlertLinks(ctx, tx, c.CustomerID, c.Status, c.AlertIDs); err != nil {
		return err
	}
	// PostgreSQL timestamps have microsecond precision. Keep the object returned
	// by the server byte-for-byte usable as a later optimistic-lock token.
	c.CreatedAt = c.CreatedAt.UTC().Truncate(time.Microsecond)
	c.UpdatedAt = c.UpdatedAt.UTC().Truncate(time.Microsecond)
	_, err = tx.Exec(ctx,
		`INSERT INTO cases (id, customer_id, alert_ids, status, priority, assigned_to, assigned_team, due_at, summary, reopen_reason, related_case_ids, investigation_disposition, str_candidate, disposition_rationale, str_report_id, str_filed_at, str_filed_by, str_filing_channel, str_destination, str_external_reference, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)`,
		c.ID, c.CustomerID, nonNilStrings(c.AlertIDs), string(c.Status), string(c.Priority), nullableString(c.AssignedTo), nullableString(c.AssignedTeam), c.DueAt, c.Summary,
		c.ReopenReason, nonNilStrings(c.RelatedCaseIDs), c.InvestigationDisposition, c.STRCandidate, c.DispositionRationale, nullableString(c.STRReportID), c.STRFiledAt, nullableString(c.STRFiledBy), nullableString(c.STRFilingChannel), nullableString(c.STRDestination), nullableString(c.STRExternalReference), c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (r *PgCaseAlertLifecycleRepo) AppendAlerts(ctx context.Context, caseID string, expectedUpdatedAt time.Time, alertIDs []string) (*domain.Case, error) {
	if err := rejectDuplicateAlertIDs(alertIDs); err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var current domain.Case
	err = tx.QueryRow(ctx,
		`SELECT `+caseColumns+` FROM cases WHERE id = $1 AND purge_marked_at IS NULL FOR UPDATE`, caseID,
	).Scan(caseScanArgs(&current)...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "case", ID: caseID}
		}
		return nil, err
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return nil, &domain.ErrConflict{Entity: "case", ID: caseID, Reason: "updated_at mismatch"}
	}
	if !domain.IsCaseUnresolved(current.Status) {
		return nil, &domain.ErrConflict{Entity: "case", ID: caseID, Reason: "terminal case cannot receive alerts"}
	}

	existing := make(map[string]struct{}, len(current.AlertIDs))
	for _, id := range current.AlertIDs {
		existing[id] = struct{}{}
	}
	var additions []string
	for _, id := range alertIDs {
		if _, ok := existing[id]; ok {
			continue
		}
		existing[id] = struct{}{}
		additions = append(additions, id)
	}
	if err := validatePgAlertLinks(ctx, tx, current.CustomerID, current.Status, additions); err != nil {
		return nil, err
	}
	if len(additions) > 0 {
		current.AlertIDs = append(current.AlertIDs, additions...)
		current.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
		if _, err := tx.Exec(ctx, `UPDATE cases SET alert_ids=$2, updated_at=$3 WHERE id=$1`, current.ID, nonNilStrings(current.AlertIDs), current.UpdatedAt); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &current, nil
}

func rejectDuplicateAlertIDs(alertIDs []string) error {
	seen := make(map[string]struct{}, len(alertIDs))
	for _, id := range alertIDs {
		key := domain.CanonicalUUID(id)
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate linked alert: %s", id)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func sameIdentifierSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !domain.SameIdentifier(left[index], right[index]) {
			return false
		}
	}
	return true
}

func validatePgAlertLinks(ctx context.Context, tx pgx.Tx, customerID string, caseStatus domain.CaseStatus, alertIDs []string) error {
	ordered := append([]string(nil), alertIDs...)
	sort.Strings(ordered)
	for _, id := range ordered {
		var alertCustomerID string
		var alertStatus domain.AlertStatus
		err := tx.QueryRow(ctx,
			`SELECT customer_id, status FROM alerts WHERE id = $1 AND purge_marked_at IS NULL FOR UPDATE`, id,
		).Scan(&alertCustomerID, &alertStatus)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &domain.ErrNotFound{Entity: "alert", ID: id}
			}
			return err
		}
		if !domain.SameIdentifier(alertCustomerID, customerID) {
			return &domain.ErrConflict{Entity: "alert", ID: id, Reason: "alert belongs to a different customer"}
		}
		if !domain.CanAttachAlertToCase(caseStatus, alertStatus) {
			return &domain.ErrConflict{Entity: "alert", ID: id, Reason: "alert is not active or case cannot receive alerts"}
		}
	}
	return nil
}

func (r *PgCaseAlertLifecycleRepo) UpdateCaseAndAlerts(ctx context.Context, c *domain.Case, expectedUpdatedAt time.Time, transitions []domain.AlertStatusTransition) error {
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

	var currentCustomerID string
	var currentAlertIDs []string
	var currentStatus domain.CaseStatus
	var currentCaseUpdatedAt time.Time
	err = tx.QueryRow(ctx,
		`SELECT customer_id, alert_ids, status, updated_at FROM cases WHERE id = $1 AND purge_marked_at IS NULL FOR UPDATE`, c.ID,
	).Scan(&currentCustomerID, &currentAlertIDs, &currentStatus, &currentCaseUpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &domain.ErrNotFound{Entity: "case", ID: c.ID}
		}
		return err
	}
	for i := range currentAlertIDs {
		currentAlertIDs[i] = domain.CanonicalUUID(currentAlertIDs[i])
	}
	if !currentCaseUpdatedAt.Equal(expectedUpdatedAt) {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "updated_at mismatch"}
	}
	if currentStatus != c.Status && !domain.ValidCaseStatusTransition(currentStatus, c.Status) {
		return &domain.ErrInvalidStateTransition{Entity: "case", ID: c.ID, From: string(currentStatus), To: string(c.Status)}
	}
	if !domain.SameIdentifier(currentCustomerID, c.CustomerID) {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "customer_id is immutable"}
	}
	if !sameIdentifierSlices(currentAlertIDs, c.AlertIDs) {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "alert_ids can only be changed through lifecycle link operations"}
	}

	linked := make(map[string]struct{}, len(currentAlertIDs))
	for _, alertID := range currentAlertIDs {
		if _, duplicate := linked[alertID]; duplicate {
			return fmt.Errorf("duplicate linked alert: %s", alertID)
		}
		linked[alertID] = struct{}{}
	}
	orderedTransitions := append([]domain.AlertStatusTransition(nil), transitions...)
	sort.Slice(orderedTransitions, func(i, j int) bool { return orderedTransitions[i].ID < orderedTransitions[j].ID })
	seen := make(map[string]struct{}, len(orderedTransitions))
	for _, transition := range orderedTransitions {
		if _, duplicate := seen[transition.ID]; duplicate {
			return fmt.Errorf("duplicate linked alert: %s", transition.ID)
		}
		seen[transition.ID] = struct{}{}
		if _, isLinked := linked[transition.ID]; !isLinked {
			return fmt.Errorf("alert is not linked to case: %s", transition.ID)
		}

		var currentStatus domain.AlertStatus
		var currentCustomerID string
		var currentUpdatedAt time.Time
		err = tx.QueryRow(ctx,
			`SELECT customer_id, status, updated_at FROM alerts WHERE id = $1 AND purge_marked_at IS NULL FOR UPDATE`, transition.ID,
		).Scan(&currentCustomerID, &currentStatus, &currentUpdatedAt)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &domain.ErrNotFound{Entity: "alert", ID: transition.ID}
			}
			return err
		}
		if !currentUpdatedAt.Equal(transition.ExpectedUpdatedAt) || currentStatus != transition.From {
			return &domain.ErrConflict{Entity: "alert", ID: transition.ID, Reason: "linked alert changed concurrently"}
		}
		if !domain.SameIdentifier(currentCustomerID, c.CustomerID) {
			return &domain.ErrConflict{Entity: "alert", ID: transition.ID, Reason: "linked alert belongs to a different customer"}
		}
		if transition.From != transition.To && !domain.ValidAlertStatusTransition(transition.From, transition.To) &&
			!(transition.ResolvedBy == "case-filing" && domain.ValidAlertStatusTransitionForCaseFiling(transition.From, transition.To)) {
			return &domain.ErrInvalidStateTransition{Entity: "alert", ID: transition.ID, From: string(transition.From), To: string(transition.To)}
		}
		if transition.From != transition.To && domain.IsAlertTerminal(transition.To) && strings.TrimSpace(transition.ResolvedBy) == "" {
			return fmt.Errorf("resolved_by is required for terminal alert status")
		}
		if !domain.CompatibleCaseAlertState(c.Status, transition.To) {
			return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "linked alert state is incompatible with target case status"}
		}
	}
	if len(seen) != len(linked) {
		return fmt.Errorf("every linked alert must be validated")
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	tag, err := tx.Exec(ctx,
		`UPDATE cases SET status=$2, priority=$3, assigned_to=$4, assigned_team=$5, due_at=$6, summary=$7, reopen_reason=$8, related_case_ids=$9, investigation_disposition=$10, str_candidate=$11, disposition_rationale=$12, str_report_id=$13, str_filed_at=$14, str_filed_by=$15, str_filing_channel=$16, str_destination=$17, str_external_reference=$18, updated_at=$19, closed_at=$20
		 WHERE id=$1 AND updated_at=$21`,
		c.ID, string(c.Status), string(c.Priority), nullableString(c.AssignedTo), nullableString(c.AssignedTeam), c.DueAt, c.Summary, c.ReopenReason,
		nonNilStrings(c.RelatedCaseIDs), c.InvestigationDisposition, c.STRCandidate, c.DispositionRationale, nullableString(c.STRReportID), c.STRFiledAt, nullableString(c.STRFiledBy), nullableString(c.STRFilingChannel), nullableString(c.STRDestination), nullableString(c.STRExternalReference), now, c.ClosedAt, expectedUpdatedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "updated_at mismatch"}
	}

	for _, transition := range orderedTransitions {
		if transition.From == transition.To {
			continue
		}
		var resolvedBy any
		var resolvedAt any
		if domain.IsAlertTerminal(transition.To) {
			resolvedBy = transition.ResolvedBy
			resolvedAt = now
		}
		tag, err := tx.Exec(ctx,
			`UPDATE alerts SET status=$2::alert_status, resolved_by=$3, resolved_at=$4,
				 disposition=CASE WHEN $2::alert_status IN ('closed_true_positive','closed_false_positive') THEN $5 ELSE disposition END,
				 disposition_rationale=CASE WHEN $2::alert_status IN ('closed_true_positive','closed_false_positive') THEN $6 ELSE disposition_rationale END,
			 updated_at=$7
				 WHERE id=$1 AND status=$8::alert_status AND updated_at=$9`,
			transition.ID, string(transition.To), resolvedBy, resolvedAt, string(transition.To), strings.TrimSpace(transition.Rationale), now,
			string(transition.From), transition.ExpectedUpdatedAt,
		)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return &domain.ErrConflict{Entity: "alert", ID: transition.ID, Reason: "linked alert changed concurrently"}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	c.UpdatedAt = now
	return nil
}
