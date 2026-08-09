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

type PgSTRReportRepo struct {
	pool DBTX
}

func NewPgSTRReportRepo(pool DBTX) *PgSTRReportRepo {
	return &PgSTRReportRepo{pool: pool}
}

func NewPgReportRepo(pool DBTX) *PgSTRReportRepo {
	return NewPgSTRReportRepo(pool)
}

func (r *PgSTRReportRepo) AppendReportEvent(ctx context.Context, event *domain.STRReportEvent) error {
	if event == nil {
		return errors.New("STR report event is nil")
	}
	if strings.TrimSpace(event.ID) == "" {
		return errors.New("STR report event id is required")
	}
	normalizeReportEventIdentifiers(event)
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO str_report_events
		(id, report_id, event_type, actor, reason, before_state, after_state, correlation_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, event.ID, event.ReportID, event.EventType, event.Actor, event.Reason,
		nullableJSONMap(event.Before), nullableJSONMap(event.After), event.CorrelationID, event.CreatedAt)
	if err != nil && isUniqueViolation(err) {
		return &domain.ErrConflict{Entity: "str_report_event", ID: event.ID, Reason: "event already exists"}
	}
	return err
}

func (r *PgSTRReportRepo) ListReportEvents(ctx context.Context, reportID string) ([]domain.STRReportEvent, error) {
	reportID = domain.CanonicalIdentifier(reportID)
	rows, err := r.pool.Query(ctx, `SELECT id, report_id, event_type, actor, reason, before_state, after_state,
		correlation_id, created_at FROM str_report_events WHERE report_id=$1 ORDER BY created_at ASC, id ASC`, reportID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.STRReportEvent
	for rows.Next() {
		var event domain.STRReportEvent
		var before, after []byte
		if err := rows.Scan(&event.ID, &event.ReportID, &event.EventType, &event.Actor, &event.Reason,
			&before, &after, &event.CorrelationID, &event.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(before, &event.Before); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(after, &event.After); err != nil {
			return nil, err
		}
		normalizeReportEventIdentifiers(&event)
		result = append(result, event)
	}
	return result, rows.Err()
}

const strReportColumns = `id, alert_id, customer_id, case_id, corrects_report_id, supersedes_report_id, report_type, status,
	suspicious_point, transaction_ids, transaction_snapshot, total_amount, currency,
	alert_snapshot, customer_snapshot, created_at, updated_at, submitted_at, created_by, submitted_by, submission_evidence`

func (r *PgSTRReportRepo) Get(ctx context.Context, id string) (*domain.STRReport, error) {
	report, err := r.scanReport(r.pool.QueryRow(ctx,
		`SELECT `+strReportColumns+` FROM str_reports WHERE id = $1`, domain.CanonicalIdentifier(id)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "str_report", ID: id}
		}
		return nil, err
	}
	return report, nil
}

func (r *PgSTRReportRepo) List(ctx context.Context, filter domain.ReportListFilter) ([]domain.STRReport, error) {
	query := `SELECT ` + strReportColumns + ` FROM str_reports`
	conditions := make([]string, 0, 4)
	args := make([]any, 0, 8)
	argIndex := 1
	if filter.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, string(filter.Status))
		argIndex++
	}
	if filter.CustomerID != "" {
		conditions = append(conditions, fmt.Sprintf("customer_id = $%d", argIndex))
		args = append(args, domain.CanonicalUUID(filter.CustomerID))
		argIndex++
	}
	if filter.AlertID != "" {
		conditions = append(conditions, fmt.Sprintf("alert_id = $%d", argIndex))
		args = append(args, domain.CanonicalUUID(filter.AlertID))
		argIndex++
	}
	if filter.Cursor != nil {
		conditions = append(conditions, fmt.Sprintf("(created_at, id) < ($%d, $%d)", argIndex, argIndex+1))
		args = append(args, filter.Cursor.CreatedAt, filter.Cursor.ID)
		argIndex += 2
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC, id DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIndex)
		args = append(args, filter.Limit)
		argIndex++
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIndex)
		args = append(args, filter.Offset)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []domain.STRReport
	for rows.Next() {
		report, err := scanSTRReportRow(rows)
		if err != nil {
			return nil, err
		}
		reports = append(reports, *report)
	}
	return reports, rows.Err()
}

func (r *PgSTRReportRepo) Create(ctx context.Context, report *domain.STRReport) error {
	if report == nil {
		return fmt.Errorf("str_report is nil")
	}
	if err := prepareSTRReportForCreate(report); err != nil {
		return err
	}
	snapshot, err := json.Marshal(nonNilSTRTransactionSnapshots(report.TransactionSnapshot))
	if err != nil {
		return fmt.Errorf("marshal transaction snapshot: %w", err)
	}
	alertSnapshot, err := json.Marshal(report.AlertSnapshot)
	if err != nil {
		return fmt.Errorf("marshal alert snapshot: %w", err)
	}
	customerSnapshot, err := json.Marshal(report.CustomerSnapshot)
	if err != nil {
		return fmt.Errorf("marshal customer snapshot: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO str_reports
		 (id, alert_id, customer_id, case_id, corrects_report_id, supersedes_report_id, report_type, status, suspicious_point,
		  transaction_ids, transaction_snapshot, total_amount, currency, alert_snapshot, customer_snapshot, created_at,
		  updated_at, submitted_at, created_by, submitted_by, submission_evidence)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)`,
		report.ID, report.AlertID, report.CustomerID, nullableString(report.CaseID), nullableString(report.CorrectsReportID), nullableString(report.SupersedesReportID),
		string(report.ReportType), string(report.Status), report.SuspiciousPoint,
		nonNilStrings(report.TransactionIDs), snapshot, report.TotalAmount, report.Currency,
		alertSnapshot, customerSnapshot, report.CreatedAt, report.UpdatedAt, report.SubmittedAt, report.CreatedBy,
		nullableString(report.SubmittedBy), nullableString(report.SubmissionEvidence),
	)
	if err != nil && isUniqueViolation(err) {
		return &domain.ErrConflict{Entity: "str_report", ID: report.ID, Reason: "report already exists"}
	}
	return err
}

func (r *PgSTRReportRepo) Update(ctx context.Context, report *domain.STRReport) error {
	if report == nil {
		return fmt.Errorf("str_report is nil")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	tag, err := r.pool.Exec(ctx,
		`UPDATE str_reports
		 SET suspicious_point = $2, updated_at = $3
		 WHERE id = $1 AND status = 'draft'`, report.ID, report.SuspiciousPoint, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		current, getErr := r.Get(ctx, report.ID)
		if getErr != nil {
			return getErr
		}
		if current.Status != domain.ReportStatusDraft {
			return &domain.ErrConflict{Entity: "str_report", ID: report.ID, Reason: "submitted report is immutable"}
		}
		return &domain.ErrConflict{Entity: "str_report", ID: report.ID, Reason: "report changed concurrently"}
	}
	updated, err := r.Get(ctx, report.ID)
	if err != nil {
		return err
	}
	*report = *updated
	return nil
}

func (r *PgSTRReportRepo) UpdateIfUnmodified(ctx context.Context, report *domain.STRReport, expectedUpdatedAt time.Time) error {
	if report == nil {
		return fmt.Errorf("str_report is nil")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	tag, err := r.pool.Exec(ctx,
		`UPDATE str_reports SET suspicious_point=$2, updated_at=$3
		 WHERE id=$1 AND status='draft' AND updated_at=$4`, report.ID, report.SuspiciousPoint, now, expectedUpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		current, getErr := r.Get(ctx, report.ID)
		if getErr != nil {
			return getErr
		}
		if current.Status != domain.ReportStatusDraft {
			return &domain.ErrConflict{Entity: "str_report", ID: report.ID, Reason: "submitted report is immutable"}
		}
		return &domain.ErrConflict{Entity: "str_report", ID: report.ID, Reason: "updated_at mismatch"}
	}
	updated, err := r.Get(ctx, report.ID)
	if err != nil {
		return err
	}
	*report = *updated
	return nil
}

func (r *PgSTRReportRepo) Submit(ctx context.Context, id, submittedBy, submissionEvidence string) (*domain.STRReport, error) {
	submissionEvidence = strings.TrimSpace(submissionEvidence)
	if submissionEvidence == "" {
		return nil, fmt.Errorf("submission evidence is required")
	}
	submittedBy = strings.TrimSpace(submittedBy)
	report, err := r.scanReport(r.pool.QueryRow(ctx,
		`UPDATE str_reports
		 SET status = 'submitted', submitted_at = COALESCE(submitted_at, now()),
		     submitted_by = $2, submission_evidence = $3, updated_at = now()
		 WHERE id = $1 AND status = 'draft'
		 RETURNING `+strReportColumns, id, submittedBy, submissionEvidence))
	if err == nil {
		return report, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	current, getErr := r.Get(ctx, id)
	if getErr != nil {
		return nil, getErr
	}
	if current.Status == domain.ReportStatusSubmitted && current.SubmissionEvidence == submissionEvidence {
		return current, nil
	}
	if current.Status == domain.ReportStatusSubmitted {
		return nil, &domain.ErrConflict{Entity: "str_report", ID: id, Reason: "submitted report has different submission evidence"}
	}
	return nil, &domain.ErrInvalidStateTransition{Entity: "str_report", ID: id, From: string(current.Status), To: string(domain.ReportStatusSubmitted)}
}

func (r *PgSTRReportRepo) scanReport(row interface{ Scan(dest ...any) error }) (*domain.STRReport, error) {
	return scanSTRReportRow(row)
}

func scanSTRReportRow(row interface{ Scan(dest ...any) error }) (*domain.STRReport, error) {
	var report domain.STRReport
	var caseID, correctsReportID, supersedesReportID, submittedBy, submissionEvidence *string
	var snapshotJSON, alertSnapshotJSON, customerSnapshotJSON []byte
	if err := row.Scan(
		&report.ID, &report.AlertID, &report.CustomerID, &caseID, &correctsReportID, &supersedesReportID,
		&report.ReportType, &report.Status, &report.SuspiciousPoint,
		&report.TransactionIDs, &snapshotJSON, &report.TotalAmount, &report.Currency, &alertSnapshotJSON, &customerSnapshotJSON,
		&report.CreatedAt, &report.UpdatedAt, &report.SubmittedAt, &report.CreatedBy,
		&submittedBy, &submissionEvidence,
	); err != nil {
		return nil, err
	}
	if caseID != nil {
		report.CaseID = *caseID
	}
	if correctsReportID != nil {
		report.CorrectsReportID = *correctsReportID
	}
	if supersedesReportID != nil {
		report.SupersedesReportID = *supersedesReportID
	}
	report.ID = domain.CanonicalIdentifier(report.ID)
	report.AlertID = domain.CanonicalUUID(report.AlertID)
	report.CustomerID = domain.CanonicalUUID(report.CustomerID)
	report.CaseID = domain.CanonicalIdentifier(report.CaseID)
	report.CorrectsReportID = domain.CanonicalIdentifier(report.CorrectsReportID)
	report.SupersedesReportID = domain.CanonicalIdentifier(report.SupersedesReportID)
	for i := range report.TransactionIDs {
		report.TransactionIDs[i] = compactUUID(report.TransactionIDs[i])
	}
	if submittedBy != nil {
		report.SubmittedBy = *submittedBy
	}
	if submissionEvidence != nil {
		report.SubmissionEvidence = *submissionEvidence
	}
	if len(snapshotJSON) > 0 {
		if err := json.Unmarshal(snapshotJSON, &report.TransactionSnapshot); err != nil {
			return nil, fmt.Errorf("decode transaction snapshot: %w", err)
		}
	}
	if len(alertSnapshotJSON) > 0 && string(alertSnapshotJSON) != "{}" {
		if err := json.Unmarshal(alertSnapshotJSON, &report.AlertSnapshot); err != nil {
			return nil, fmt.Errorf("decode alert snapshot: %w", err)
		}
	}
	if len(customerSnapshotJSON) > 0 && string(customerSnapshotJSON) != "{}" {
		if err := json.Unmarshal(customerSnapshotJSON, &report.CustomerSnapshot); err != nil {
			return nil, fmt.Errorf("decode customer snapshot: %w", err)
		}
	}
	report.AlertSnapshot.ID = compactUUID(report.AlertSnapshot.ID)
	report.AlertSnapshot.CustomerID = compactUUID(report.AlertSnapshot.CustomerID)
	for i := range report.AlertSnapshot.TransactionIDs {
		report.AlertSnapshot.TransactionIDs[i] = compactUUID(report.AlertSnapshot.TransactionIDs[i])
	}
	report.CustomerSnapshot.ID = compactUUID(report.CustomerSnapshot.ID)
	if report.TransactionIDs == nil {
		report.TransactionIDs = []string{}
	}
	if report.TransactionSnapshot == nil {
		report.TransactionSnapshot = []domain.STRTransactionSnapshot{}
	}
	for i := range report.TransactionSnapshot {
		report.TransactionSnapshot[i].ID = compactUUID(report.TransactionSnapshot[i].ID)
	}
	return &report, nil
}

func nonNilSTRTransactionSnapshots(snapshots []domain.STRTransactionSnapshot) []domain.STRTransactionSnapshot {
	if snapshots == nil {
		return []domain.STRTransactionSnapshot{}
	}
	return snapshots
}
