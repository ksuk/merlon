package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func queueFilterFingerprint(r *http.Request) string {
	query := r.URL.Query()
	query.Del("cursor")
	query.Del("limit")
	query.Del("offset")
	digest := sha256.Sum256([]byte(query.Encode()))
	return hex.EncodeToString(digest[:])
}

func bindQueueCursorFilter(cursor *Cursor, fingerprint string) error {
	if cursor == nil {
		return nil
	}
	if cursor.Filter != "" && cursor.Filter != fingerprint {
		return fmt.Errorf("cursor does not belong to the requested queue filters")
	}
	return nil
}

func addQueueCursorFilter(meta PaginationMeta, fingerprint string) PaginationMeta {
	if !meta.HasMore || meta.NextCursor == "" {
		return meta
	}
	cursor, err := DecodeCursor(meta.NextCursor)
	if err != nil {
		return meta
	}
	cursor.Filter = fingerprint
	meta.NextCursor = EncodeCursor(cursor)
	return meta
}

func csvValues(raw string) []string {
	var values []string
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func parseAlertQueueFilter(r *http.Request) (domain.AlertQueueFilter, error) {
	f := domain.AlertQueueFilter{
		CustomerID: domain.CanonicalUUID(r.URL.Query().Get("customer_id")), ScenarioID: r.URL.Query().Get("scenario_id"),
		Assignee: r.URL.Query().Get("assignee"), Team: r.URL.Query().Get("team"),
		Unassigned: r.URL.Query().Get("unassigned") == "true", Severity: domain.AlertSeverity(r.URL.Query().Get("severity")),
		Search: r.URL.Query().Get("search"), Overdue: r.URL.Query().Get("overdue") == "true", AsOf: time.Now().UTC(),
	}
	if r.URL.Query().Get("mine") == "true" && f.Assignee == "" {
		f.Assignee = resolveAuditUserID(r)
	}
	var err error
	if f.MinAgeDays, err = parseQueueAgeDays(r.URL.Query().Get("min_age_days"), "min_age_days"); err != nil {
		return domain.AlertQueueFilter{}, err
	}
	if f.MaxAgeDays, err = parseQueueAgeDays(r.URL.Query().Get("max_age_days"), "max_age_days"); err != nil {
		return domain.AlertQueueFilter{}, err
	}
	if f.MinAgeDays > 0 && f.MaxAgeDays > 0 && f.MinAgeDays > f.MaxAgeDays {
		return domain.AlertQueueFilter{}, fmt.Errorf("min_age_days must not exceed max_age_days")
	}
	for _, status := range csvValues(r.URL.Query().Get("status")) {
		parsed := domain.AlertStatus(status)
		if !validAlertQueueStatus(parsed) {
			return domain.AlertQueueFilter{}, fmt.Errorf("unsupported alert status: %s", status)
		}
		f.Statuses = append(f.Statuses, parsed)
	}
	if f.Severity != "" && !validAlertQueueSeverity(f.Severity) {
		return domain.AlertQueueFilter{}, fmt.Errorf("unsupported alert severity: %s", f.Severity)
	}
	active := r.URL.Query().Get("active") == "true"
	terminal := r.URL.Query().Get("terminal") == "true"
	if active && terminal {
		return domain.AlertQueueFilter{}, fmt.Errorf("active and terminal filters cannot be combined")
	}
	if active {
		f.Statuses = []domain.AlertStatus{domain.AlertStatusOpen, domain.AlertStatusInvestigating, domain.AlertStatusEscalated}
	}
	if terminal {
		f.Statuses = []domain.AlertStatus{domain.AlertStatusClosedTruePositive, domain.AlertStatusClosedFalsePositive}
	}
	return f, nil
}

func parseCaseQueueFilter(r *http.Request) (domain.CaseQueueFilter, error) {
	f := domain.CaseQueueFilter{
		CustomerID: domain.CanonicalUUID(r.URL.Query().Get("customer_id")), Assignee: r.URL.Query().Get("assignee"), Team: r.URL.Query().Get("team"),
		Unassigned: r.URL.Query().Get("unassigned") == "true", Priority: domain.CasePriority(r.URL.Query().Get("priority")),
		Disposition: r.URL.Query().Get("disposition"), Search: r.URL.Query().Get("search"), Overdue: r.URL.Query().Get("overdue") == "true", AsOf: time.Now().UTC(),
	}
	if r.URL.Query().Get("mine") == "true" && f.Assignee == "" {
		f.Assignee = resolveAuditUserID(r)
	}
	var err error
	if f.MinAgeDays, err = parseQueueAgeDays(r.URL.Query().Get("min_age_days"), "min_age_days"); err != nil {
		return domain.CaseQueueFilter{}, err
	}
	if f.MaxAgeDays, err = parseQueueAgeDays(r.URL.Query().Get("max_age_days"), "max_age_days"); err != nil {
		return domain.CaseQueueFilter{}, err
	}
	if f.MinAgeDays > 0 && f.MaxAgeDays > 0 && f.MinAgeDays > f.MaxAgeDays {
		return domain.CaseQueueFilter{}, fmt.Errorf("min_age_days must not exceed max_age_days")
	}
	if f.Priority != "" && !validCaseQueuePriority(f.Priority) {
		return domain.CaseQueueFilter{}, fmt.Errorf("unsupported case priority: %s", f.Priority)
	}
	if raw := r.URL.Query().Get("str_candidate"); raw != "" {
		candidate, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return domain.CaseQueueFilter{}, fmt.Errorf("str_candidate must be true or false")
		}
		f.STRCandidate = &candidate
	}
	for _, status := range csvValues(r.URL.Query().Get("status")) {
		parsed := domain.CaseStatus(status)
		if !validCaseQueueStatus(parsed) {
			return domain.CaseQueueFilter{}, fmt.Errorf("unsupported case status: %s", status)
		}
		f.Statuses = append(f.Statuses, parsed)
	}
	active := r.URL.Query().Get("active") == "true"
	terminal := r.URL.Query().Get("terminal") == "true"
	if active && terminal {
		return domain.CaseQueueFilter{}, fmt.Errorf("active and terminal filters cannot be combined")
	}
	if active {
		f.Statuses = []domain.CaseStatus{domain.CaseStatusOpen, domain.CaseStatusNew, domain.CaseStatusInvestigating, domain.CaseStatusEscalated, domain.CaseStatusReopened}
	}
	if terminal {
		f.Statuses = []domain.CaseStatus{domain.CaseStatusClosed, domain.CaseStatusStrFiled}
	}
	return f, nil
}

func validAlertQueueStatus(status domain.AlertStatus) bool {
	switch status {
	case domain.AlertStatusOpen, domain.AlertStatusInvestigating, domain.AlertStatusEscalated,
		domain.AlertStatusClosedTruePositive, domain.AlertStatusClosedFalsePositive, domain.AlertStatusSuppressed:
		return true
	default:
		return false
	}
}

func validAlertQueueSeverity(severity domain.AlertSeverity) bool {
	switch severity {
	case domain.AlertSeverityLow, domain.AlertSeverityMedium, domain.AlertSeverityHigh, domain.AlertSeverityCritical:
		return true
	default:
		return false
	}
}

func validCaseQueueStatus(status domain.CaseStatus) bool {
	switch status {
	case domain.CaseStatusOpen, domain.CaseStatusNew, domain.CaseStatusInvestigating,
		domain.CaseStatusEscalated, domain.CaseStatusReopened, domain.CaseStatusClosed, domain.CaseStatusStrFiled:
		return true
	default:
		return false
	}
}

func validCaseQueuePriority(priority domain.CasePriority) bool {
	switch priority {
	case domain.CasePriorityLow, domain.CasePriorityMedium, domain.CasePriorityHigh, domain.CasePriorityCritical:
		return true
	default:
		return false
	}
}

func parseQueueAgeDays(raw, name string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days < 1 || days > 36500 {
		return 0, fmt.Errorf("%s must be an integer between 1 and 36500", name)
	}
	return days, nil
}

func hasAlertQueueFilter(r *http.Request) bool {
	q := r.URL.Query()
	return q.Get("customer_id") != "" || q.Get("status") != "" || q.Get("active") != "" || q.Get("terminal") != "" || q.Get("assignee") != "" || q.Get("mine") != "" || q.Get("team") != "" || q.Get("unassigned") != "" || q.Get("severity") != "" || q.Get("scenario_id") != "" || q.Get("search") != "" || q.Get("overdue") != "" || q.Get("min_age_days") != "" || q.Get("max_age_days") != ""
}

func hasCaseQueueFilter(r *http.Request) bool {
	q := r.URL.Query()
	return q.Get("customer_id") != "" || q.Get("status") != "" || q.Get("active") != "" || q.Get("terminal") != "" || q.Get("assignee") != "" || q.Get("mine") != "" || q.Get("team") != "" || q.Get("unassigned") != "" || q.Get("priority") != "" || q.Get("disposition") != "" || q.Get("str_candidate") != "" || q.Get("search") != "" || q.Get("overdue") != "" || q.Get("min_age_days") != "" || q.Get("max_age_days") != ""
}

func parseOffsetLimit(r *http.Request, fallback int) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = fallback
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
