package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func alertQueueMatches(a domain.Alert, f domain.AlertQueueFilter) bool {
	if f.CustomerID != "" && !domain.SameIdentifier(a.CustomerID, f.CustomerID) {
		return false
	}
	if f.ScenarioID != "" && a.ScenarioID != f.ScenarioID {
		return false
	}
	if f.TransactionID != "" && !containsIdentifier(a.TransactionIDs, f.TransactionID) {
		return false
	}
	if f.Severity != "" && a.Severity != f.Severity {
		return false
	}
	if len(f.Statuses) > 0 {
		matched := false
		for _, status := range f.Statuses {
			if a.Status == status {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	} else if !domain.IsAlertUnresolved(a.Status) {
		return false
	}
	if f.Unassigned && (a.AssignedTo != "" || a.AssignedTeam != "") {
		return false
	}
	if f.Assignee != "" && a.AssignedTo != f.Assignee {
		return false
	}
	if f.Team != "" && a.AssignedTeam != f.Team {
		return false
	}
	if f.Overdue && (a.DueAt == nil || !a.DueAt.Before(f.AsOf)) {
		return false
	}
	alertAge := a.DetectedAt
	if alertAge.IsZero() {
		alertAge = a.CreatedAt
	}
	if !queueAgeMatches(alertAge, f.AsOf, f.MinAgeDays, f.MaxAgeDays) {
		return false
	}
	if search := strings.ToLower(strings.TrimSpace(f.Search)); search != "" &&
		!strings.Contains(strings.ToLower(a.ID), search) &&
		!strings.Contains(strings.ToLower(a.CustomerID), search) &&
		!strings.Contains(strings.ToLower(a.ScenarioID), search) &&
		!strings.Contains(strings.ToLower(a.Description), search) {
		return false
	}
	return true
}

// containsIdentifier reports whether ids holds want, comparing under the same
// canonical form the repositories store identifiers in.
func containsIdentifier(ids []string, want string) bool {
	for _, id := range ids {
		if domain.SameIdentifier(id, want) {
			return true
		}
	}
	return false
}

func anyIdentifier(ids []string, wanted []string) bool {
	for _, want := range wanted {
		if containsIdentifier(ids, want) {
			return true
		}
	}
	return false
}

func caseQueueMatches(c domain.Case, f domain.CaseQueueFilter) bool {
	if f.CustomerID != "" && !domain.SameIdentifier(c.CustomerID, f.CustomerID) {
		return false
	}
	if len(f.AlertIDs) > 0 && !anyIdentifier(c.AlertIDs, f.AlertIDs) {
		return false
	}
	if len(f.Statuses) > 0 {
		matched := false
		for _, status := range f.Statuses {
			if c.Status == status {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	} else if !domain.IsCaseUnresolved(c.Status) {
		return false
	}
	if f.Priority != "" && c.Priority != f.Priority {
		return false
	}
	if f.Unassigned && (c.AssignedTo != "" || c.AssignedTeam != "") {
		return false
	}
	if f.Assignee != "" && c.AssignedTo != f.Assignee {
		return false
	}
	if f.Team != "" && c.AssignedTeam != f.Team {
		return false
	}
	if f.Disposition != "" && c.InvestigationDisposition != f.Disposition {
		return false
	}
	if f.STRCandidate != nil && c.STRCandidate != *f.STRCandidate {
		return false
	}
	if f.Overdue && (c.DueAt == nil || !c.DueAt.Before(f.AsOf)) {
		return false
	}
	if !queueAgeMatches(c.CreatedAt, f.AsOf, f.MinAgeDays, f.MaxAgeDays) {
		return false
	}
	if search := strings.ToLower(strings.TrimSpace(f.Search)); search != "" &&
		!strings.Contains(strings.ToLower(c.ID), search) &&
		!strings.Contains(strings.ToLower(c.CustomerID), search) &&
		!strings.Contains(strings.ToLower(c.Summary), search) {
		return false
	}
	return true
}

func queueAgeMatches(createdAt, asOf time.Time, minDays, maxDays int) bool {
	if minDays <= 0 && maxDays <= 0 {
		return true
	}
	if createdAt.IsZero() {
		return false
	}
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	if minDays > 0 && createdAt.After(asOf.Add(-time.Duration(minDays)*24*time.Hour)) {
		return false
	}
	if maxDays > 0 && createdAt.Before(asOf.Add(-time.Duration(maxDays)*24*time.Hour)) {
		return false
	}
	return true
}

func (r *MemoryAlertRepo) ListQueue(_ context.Context, f domain.AlertQueueFilter, limit, offset int) ([]domain.Alert, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if f.AsOf.IsZero() {
		f.AsOf = time.Now()
	}
	var out []domain.Alert
	for _, alert := range r.data {
		if alertQueueMatches(*alert, f) {
			out = append(out, *alert)
		}
	}
	sortByRiskDesc(out, func(a domain.Alert) int { return domain.AlertSeverityRank(a.Severity) }, func(a domain.Alert) time.Time { return a.UpdatedAt }, func(a domain.Alert) string { return a.ID })
	return pageByOffset(out, limit, offset), nil
}

func (r *MemoryAlertRepo) ListQueueCursor(_ context.Context, f domain.AlertQueueFilter, limit int, after *domain.Cursor) ([]domain.Alert, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if f.AsOf.IsZero() {
		f.AsOf = time.Now().UTC()
	}
	var out []domain.Alert
	for _, alert := range r.data {
		if alertQueueMatches(*alert, f) {
			out = append(out, *alert)
		}
	}
	return sortAndPageByRiskCursor(out, limit, after,
		func(a domain.Alert) int { return domain.AlertSeverityRank(a.Severity) },
		func(a domain.Alert) time.Time { return a.UpdatedAt },
		func(a domain.Alert) string { return a.ID }), nil
}

func (r *MemoryCaseRepo) ListQueue(_ context.Context, f domain.CaseQueueFilter, limit, offset int) ([]domain.Case, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if f.AsOf.IsZero() {
		f.AsOf = time.Now()
	}
	var out []domain.Case
	for _, kase := range r.data {
		if caseQueueMatches(*kase, f) {
			out = append(out, *kase)
		}
	}
	sortByRiskDesc(out, func(c domain.Case) int { return domain.CasePriorityRank(c.Priority) }, func(c domain.Case) time.Time { return c.UpdatedAt }, func(c domain.Case) string { return c.ID })
	return pageByOffset(out, limit, offset), nil
}

func (r *MemoryCaseRepo) ListQueueCursor(_ context.Context, f domain.CaseQueueFilter, limit int, after *domain.Cursor) ([]domain.Case, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if f.AsOf.IsZero() {
		f.AsOf = time.Now().UTC()
	}
	var out []domain.Case
	for _, kase := range r.data {
		if caseQueueMatches(*kase, f) {
			out = append(out, *kase)
		}
	}
	return sortAndPageByRiskCursor(out, limit, after,
		func(c domain.Case) int { return domain.CasePriorityRank(c.Priority) },
		func(c domain.Case) time.Time { return c.UpdatedAt },
		func(c domain.Case) string { return c.ID }), nil
}

func (r *MemoryAlertRepo) UpdateQueue(_ context.Context, id string, assignedTo, assignedTeam *string, dueAt *time.Time, expectedUpdatedAt *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	id = domain.CanonicalUUID(id)
	a, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "alert", ID: id}
	}
	if expectedUpdatedAt != nil && !a.UpdatedAt.Equal(*expectedUpdatedAt) {
		return &domain.ErrConflict{Entity: "alert", ID: id, Reason: "updated_at mismatch"}
	}
	if assignedTo != nil {
		a.AssignedTo = strings.TrimSpace(*assignedTo)
	}
	if assignedTeam != nil {
		a.AssignedTeam = strings.TrimSpace(*assignedTeam)
	}
	// A nil due date is an explicit clear. The HTTP layer preserves the
	// current value when due_at was omitted, so repository callers can use nil
	// to represent the durable unassigned-SLA state.
	a.DueAt = dueAt
	a.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *PgAlertRepo) UpdateQueue(ctx context.Context, id string, assignedTo, assignedTeam *string, dueAt *time.Time, expectedUpdatedAt *time.Time) error {
	id = domain.CanonicalUUID(id)
	now := time.Now().UTC().Truncate(time.Microsecond)
	query := `UPDATE alerts SET assigned_to=$2, assigned_team=$3, due_at=$4, updated_at=$5 WHERE id=$1`
	args := []any{id, nullableStringValue(assignedTo), nullableStringValue(assignedTeam), dueAt, now}
	if expectedUpdatedAt != nil {
		query += ` AND updated_at=$6`
		args = append(args, *expectedUpdatedAt)
	}
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.Get(ctx, id); err != nil {
			return err
		}
		return &domain.ErrConflict{Entity: "alert", ID: id, Reason: "updated_at mismatch"}
	}
	return nil
}

func nullableStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return nullableString(strings.TrimSpace(*value))
}

// appendAlertQueueFilters writes the alert queue's WHERE clauses.
//
// ListQueue and ListQueueCursor held byte-identical copies of this block. Two
// copies of a filter set are two chances for a queue and its next page to
// disagree about what the filter means, so they share one builder and a new
// filter cannot reach only half the queue.
func appendAlertQueueFilters(query string, args []any, arg int, f domain.AlertQueueFilter) (string, []any, int) {
	if f.CustomerID != "" {
		query += fmt.Sprintf(" AND customer_id=$%d", arg)
		args = append(args, f.CustomerID)
		arg++
	}
	if f.ScenarioID != "" {
		query += fmt.Sprintf(" AND scenario_id=$%d", arg)
		args = append(args, f.ScenarioID)
		arg++
	}
	if f.TransactionID != "" {
		query += fmt.Sprintf(" AND $%d::uuid = ANY(transaction_ids)", arg)
		args = append(args, domain.CanonicalUUID(f.TransactionID))
		arg++
	}
	if f.Severity != "" {
		query += fmt.Sprintf(" AND severity=$%d", arg)
		args = append(args, string(f.Severity))
		arg++
	}
	if len(f.Statuses) > 0 {
		statuses := make([]string, len(f.Statuses))
		for i, status := range f.Statuses {
			statuses[i] = string(status)
		}
		query += fmt.Sprintf(" AND status::text = ANY($%d::text[])", arg)
		args = append(args, statuses)
		arg++
	} else {
		query += " AND status IN ('open','investigating','escalated')"
	}
	if f.Assignee != "" {
		query += fmt.Sprintf(" AND assigned_to=$%d", arg)
		args = append(args, f.Assignee)
		arg++
	}
	if f.Team != "" {
		query += fmt.Sprintf(" AND assigned_team=$%d", arg)
		args = append(args, f.Team)
		arg++
	}
	if f.Unassigned {
		query += " AND NULLIF(BTRIM(COALESCE(assigned_to,'')), '') IS NULL AND NULLIF(BTRIM(COALESCE(assigned_team,'')), '') IS NULL"
	}
	if f.Search != "" {
		query += fmt.Sprintf(" AND (id::text ILIKE $%d OR customer_id::text ILIKE $%d OR scenario_id ILIKE $%d OR description ILIKE $%d)", arg, arg, arg, arg)
		args = append(args, "%"+f.Search+"%")
		arg++
	}
	if f.Overdue {
		asOf := f.AsOf
		if asOf.IsZero() {
			asOf = time.Now().UTC()
		}
		query += fmt.Sprintf(" AND due_at IS NOT NULL AND due_at < $%d", arg)
		args = append(args, asOf)
		arg++
	}
	if f.MinAgeDays > 0 || f.MaxAgeDays > 0 {
		asOf := f.AsOf
		if asOf.IsZero() {
			asOf = time.Now().UTC()
		}
		if f.MinAgeDays > 0 {
			query += fmt.Sprintf(" AND detected_at <= $%d", arg)
			args = append(args, asOf.Add(-time.Duration(f.MinAgeDays)*24*time.Hour))
			arg++
		}
		if f.MaxAgeDays > 0 {
			query += fmt.Sprintf(" AND detected_at >= $%d", arg)
			args = append(args, asOf.Add(-time.Duration(f.MaxAgeDays)*24*time.Hour))
			arg++
		}
	}
	return query, args, arg
}

func (r *PgAlertRepo) ListQueue(ctx context.Context, f domain.AlertQueueFilter, limit, offset int) ([]domain.Alert, error) {
	f.CustomerID = domain.CanonicalUUID(f.CustomerID)
	query, args, arg := appendAlertQueueFilters(`SELECT `+alertColumns+` FROM alerts WHERE purge_marked_at IS NULL`, nil, 1, f)
	query += ` ORDER BY CASE severity::text WHEN 'critical' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 ELSE 1 END DESC, updated_at DESC, id DESC`
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", arg, arg+1)
	args = append(args, limit, offset)
	return r.listAlerts(ctx, query, args...)
}

// appendCaseQueueFilters writes the case queue's WHERE clauses. ListQueue and
// ListQueueCursor share it for the same reason the alert queue does: two
// copies of a filter set drift, and a queue that disagrees with its own next
// page is worse than one that is simply wrong.
func appendCaseQueueFilters(query string, args []any, arg int, f domain.CaseQueueFilter) (string, []any, int) {
	if f.CustomerID != "" {
		query += fmt.Sprintf(" AND customer_id=$%d", arg)
		args = append(args, f.CustomerID)
		arg++
	}
	if len(f.AlertIDs) > 0 {
		// alert_ids is TEXT[] (it predates the UUID link table), so the
		// overlap operand must be text as well.
		wanted := make([]string, 0, len(f.AlertIDs))
		for _, id := range f.AlertIDs {
			wanted = append(wanted, domain.CanonicalIdentifier(id))
		}
		query += fmt.Sprintf(" AND alert_ids && $%d::text[]", arg)
		args = append(args, wanted)
		arg++
	}
	if len(f.Statuses) > 0 {
		statuses := make([]string, len(f.Statuses))
		for i, status := range f.Statuses {
			statuses[i] = string(status)
		}
		query += fmt.Sprintf(" AND status = ANY($%d::text[])", arg)
		args = append(args, statuses)
		arg++
	} else {
		query += " AND status IN ('open','new','investigating','escalated','reopened')"
	}
	if f.Assignee != "" {
		query += fmt.Sprintf(" AND assigned_to=$%d", arg)
		args = append(args, f.Assignee)
		arg++
	}
	if f.Team != "" {
		query += fmt.Sprintf(" AND assigned_team=$%d", arg)
		args = append(args, f.Team)
		arg++
	}
	if f.Unassigned {
		query += " AND NULLIF(BTRIM(COALESCE(assigned_to,'')), '') IS NULL AND NULLIF(BTRIM(COALESCE(assigned_team,'')), '') IS NULL"
	}
	if f.Priority != "" {
		query += fmt.Sprintf(" AND priority=$%d", arg)
		args = append(args, string(f.Priority))
		arg++
	}
	if f.Disposition != "" {
		query += fmt.Sprintf(" AND investigation_disposition=$%d", arg)
		args = append(args, f.Disposition)
		arg++
	}
	if f.STRCandidate != nil {
		query += fmt.Sprintf(" AND str_candidate=$%d", arg)
		args = append(args, *f.STRCandidate)
		arg++
	}
	if f.Search != "" {
		query += fmt.Sprintf(" AND (id ILIKE $%d OR customer_id::text ILIKE $%d OR summary ILIKE $%d)", arg, arg, arg)
		args = append(args, "%"+f.Search+"%")
		arg++
	}
	if f.Overdue {
		asOf := f.AsOf
		if asOf.IsZero() {
			asOf = time.Now().UTC()
		}
		query += fmt.Sprintf(" AND due_at IS NOT NULL AND due_at < $%d", arg)
		args = append(args, asOf)
		arg++
	}
	if f.MinAgeDays > 0 || f.MaxAgeDays > 0 {
		asOf := f.AsOf
		if asOf.IsZero() {
			asOf = time.Now().UTC()
		}
		if f.MinAgeDays > 0 {
			query += fmt.Sprintf(" AND created_at <= $%d", arg)
			args = append(args, asOf.Add(-time.Duration(f.MinAgeDays)*24*time.Hour))
			arg++
		}
		if f.MaxAgeDays > 0 {
			query += fmt.Sprintf(" AND created_at >= $%d", arg)
			args = append(args, asOf.Add(-time.Duration(f.MaxAgeDays)*24*time.Hour))
			arg++
		}
	}
	return query, args, arg
}

func (r *PgCaseRepo) ListQueue(ctx context.Context, f domain.CaseQueueFilter, limit, offset int) ([]domain.Case, error) {
	f.CustomerID = domain.CanonicalUUID(f.CustomerID)
	query, args, arg := appendCaseQueueFilters(`SELECT `+caseColumns+` FROM cases WHERE purge_marked_at IS NULL`, nil, 1, f)
	query += ` ORDER BY ` + caseRiskRankSQL + ` DESC, updated_at DESC, id DESC`
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", arg, arg+1)
	args = append(args, limit, offset)
	return r.listCases(ctx, query, args...)
}

// ListQueueCursor is the keyset form of the filtered alert queue. It mirrors
// ListQueue's filters and ordering, but uses the full risk/update/id sort key
// so a page boundary cannot duplicate or skip records when several alerts
// share the same severity.
func (r *PgAlertRepo) ListQueueCursor(ctx context.Context, f domain.AlertQueueFilter, limit int, after *domain.Cursor) ([]domain.Alert, error) {
	f.CustomerID = domain.CanonicalUUID(f.CustomerID)
	query, args, arg := appendAlertQueueFilters(`SELECT `+alertColumns+` FROM alerts WHERE purge_marked_at IS NULL`, nil, 1, f)
	if after != nil {
		query += fmt.Sprintf(" AND ("+alertRiskRankSQL+`, updated_at, id) < ($%d, $%d, $%d)`, arg, arg+1, arg+2)
		args = append(args, after.Rank, after.CreatedAt, after.ID)
		arg += 3
	}
	query += ` ORDER BY ` + alertRiskRankSQL + ` DESC, updated_at DESC, id DESC`
	query += fmt.Sprintf(" LIMIT $%d", arg)
	args = append(args, limit)
	return r.listAlerts(ctx, query, args...)
}

// ListQueueCursor is the keyset form of the filtered case queue. The cursor
// uses priority rank plus updated_at/id, matching the queue's deterministic
// ordering across memory and PostgreSQL stores.
func (r *PgCaseRepo) ListQueueCursor(ctx context.Context, f domain.CaseQueueFilter, limit int, after *domain.Cursor) ([]domain.Case, error) {
	f.CustomerID = domain.CanonicalUUID(f.CustomerID)
	query, args, arg := appendCaseQueueFilters(`SELECT `+caseColumns+` FROM cases WHERE purge_marked_at IS NULL`, nil, 1, f)
	if after != nil {
		query += fmt.Sprintf(" AND ("+caseRiskRankSQL+`, updated_at, id) < ($%d, $%d, $%d)`, arg, arg+1, arg+2)
		args = append(args, after.Rank, after.CreatedAt, after.ID)
		arg += 3
	}
	query += ` ORDER BY ` + caseRiskRankSQL + ` DESC, updated_at DESC, id DESC`
	query += fmt.Sprintf(" LIMIT $%d", arg)
	args = append(args, limit)
	return r.listCases(ctx, query, args...)
}
