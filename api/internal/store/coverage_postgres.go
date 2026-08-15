package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/domain"
)

type PgCoverageAnalysisRepo struct{ pool DBTX }

func NewPgCoverageAnalysisRepo(pool DBTX) *PgCoverageAnalysisRepo {
	return &PgCoverageAnalysisRepo{pool: pool}
}

const coverageColumns = `id,kind,status,scenario_ids,customer_ids,period_from,period_to,rule_set_id,snapshot_at,matcher_version,assumptions,summary,by_scenario,error,created_at,started_at,completed_at,updated_at`

func scanCoverageAnalysis(row interface{ Scan(dest ...any) error }) (*domain.CoverageAnalysis, error) {
	var item domain.CoverageAnalysis
	var status, kind string
	var scenarios, customers, assumptions, summary, byScenario []byte
	if err := row.Scan(&item.ID, &kind, &status, &scenarios, &customers, &item.From, &item.To, &item.RuleSetID, &item.SnapshotAt, &item.MatcherVersion, &assumptions, &summary, &byScenario, &item.Error, &item.CreatedAt, &item.StartedAt, &item.CompletedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.Kind, item.Status = kind, domain.CoverageAnalysisStatus(status)
	_ = json.Unmarshal(scenarios, &item.ScenarioIDs)
	_ = json.Unmarshal(customers, &item.CustomerIDs)
	_ = json.Unmarshal(assumptions, &item.Assumptions)
	_ = json.Unmarshal(summary, &item.Summary)
	_ = json.Unmarshal(byScenario, &item.ByScenario)
	return &item, nil
}

func (r *PgCoverageAnalysisRepo) CreateCoverageAnalysis(ctx context.Context, item *domain.CoverageAnalysis) error {
	if item.ID == "" {
		item.ID = wave3ID()
	}
	if item.Kind == "" {
		item.Kind = domain.CoverageAnalysisKind
	}
	if item.Status == "" {
		item.Status = domain.CoverageAnalysisQueued
	}
	err := r.pool.QueryRow(ctx, `INSERT INTO coverage_analyses(id,kind,status,scenario_ids,customer_ids,period_from,period_to,rule_set_id,snapshot_at,matcher_version,assumptions,summary,by_scenario,error) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING created_at,updated_at`, item.ID, item.Kind, item.Status, marshalJSON(item.ScenarioIDs), marshalJSON(item.CustomerIDs), item.From, item.To, item.RuleSetID, item.SnapshotAt, item.MatcherVersion, marshalJSON(item.Assumptions), marshalJSON(item.Summary), marshalJSON(item.ByScenario), item.Error).Scan(&item.CreatedAt, &item.UpdatedAt)
	return err
}

func (r *PgCoverageAnalysisRepo) GetCoverageAnalysis(ctx context.Context, id string) (*domain.CoverageAnalysis, error) {
	item, err := scanCoverageAnalysis(r.pool.QueryRow(ctx, `SELECT `+coverageColumns+` FROM coverage_analyses WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "coverage_analysis", ID: id}
	}
	return item, err
}

func (r *PgCoverageAnalysisRepo) ListCoverageAnalyses(ctx context.Context, filter domain.CoverageAnalysisFilter) ([]domain.CoverageAnalysis, error) {
	query := `SELECT ` + coverageColumns + ` FROM coverage_analyses WHERE 1=1`
	args := []any{}
	if filter.Status != "" {
		query += fmt.Sprintf(" AND status=$%d", len(args)+1)
		args = append(args, filter.Status)
	}
	query += " ORDER BY created_at DESC,id DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
		args = append(args, filter.Offset)
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.CoverageAnalysis{}
	for rows.Next() {
		item, err := scanCoverageAnalysis(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *PgCoverageAnalysisRepo) StartCoverageAnalysis(ctx context.Context, id string) (*domain.CoverageAnalysis, error) {
	var item domain.CoverageAnalysis
	var kind, status string
	var scenarios, customers, assumptions, summary, byScenario []byte
	err := r.pool.QueryRow(ctx, `UPDATE coverage_analyses SET status='running',started_at=COALESCE(started_at,now()),updated_at=now() WHERE id=$1 AND status='queued' RETURNING `+coverageColumns, id).Scan(&item.ID, &kind, &status, &scenarios, &customers, &item.From, &item.To, &item.RuleSetID, &item.SnapshotAt, &item.MatcherVersion, &assumptions, &summary, &byScenario, &item.Error, &item.CreatedAt, &item.StartedAt, &item.CompletedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r.GetCoverageAnalysis(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	item.Kind, item.Status = kind, domain.CoverageAnalysisStatus(status)
	_ = json.Unmarshal(scenarios, &item.ScenarioIDs)
	_ = json.Unmarshal(customers, &item.CustomerIDs)
	_ = json.Unmarshal(assumptions, &item.Assumptions)
	_ = json.Unmarshal(summary, &item.Summary)
	_ = json.Unmarshal(byScenario, &item.ByScenario)
	return &item, nil
}

func (r *PgCoverageAnalysisRepo) CompleteCoverageAnalysis(ctx context.Context, id string, summary domain.CoverageSummary, byScenario map[string]domain.CoverageSummary) error {
	tag, err := r.pool.Exec(ctx, `UPDATE coverage_analyses SET status='completed',summary=$2,by_scenario=$3,completed_at=now(),updated_at=now() WHERE id=$1`, id, marshalJSON(summary), marshalJSON(byScenario))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return r.ensureCoverageExists(ctx, id)
	}
	return nil
}

func (r *PgCoverageAnalysisRepo) FailCoverageAnalysis(ctx context.Context, id, reason string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE coverage_analyses SET status='failed',error=$2,updated_at=now() WHERE id=$1`, id, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return r.ensureCoverageExists(ctx, id)
	}
	return nil
}

func (r *PgCoverageAnalysisRepo) SaveCoverageMatterResults(ctx context.Context, id string, results []domain.CoverageMatterResult) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM coverage_analysis_matters WHERE analysis_id=$1`, id); err != nil {
		return err
	}
	for _, item := range results {
		if item.ID == "" {
			item.ID = wave3ID()
		}
		if _, err := tx.Exec(ctx, `INSERT INTO coverage_analysis_matters(id,analysis_id,matter_id,customer_id,scenario_ids,source,label,covered,unevaluable,matched_alert_id,matcher_version,assumptions,snapshot_at,provenance,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,COALESCE($15,now())) ON CONFLICT(analysis_id,matter_id) DO UPDATE SET scenario_ids=EXCLUDED.scenario_ids,source=EXCLUDED.source,label=EXCLUDED.label,covered=EXCLUDED.covered,unevaluable=EXCLUDED.unevaluable,matched_alert_id=EXCLUDED.matched_alert_id,matcher_version=EXCLUDED.matcher_version,assumptions=EXCLUDED.assumptions,snapshot_at=EXCLUDED.snapshot_at,provenance=EXCLUDED.provenance`, item.ID, id, item.MatterID, domain.CanonicalUUID(item.CustomerID), marshalJSON(item.ScenarioIDs), item.Source, item.Label, item.Covered, item.Unevaluable, item.MatchedAlertID, item.MatcherVersion, marshalJSON(item.Assumptions), item.SnapshotAt, marshalJSON(item.Provenance), nullableTime(item.CreatedAt)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *PgCoverageAnalysisRepo) ListCoverageMatterResults(ctx context.Context, filter domain.CoverageMatterFilter) ([]domain.CoverageMatterResult, error) {
	query := `SELECT id,analysis_id,matter_id,customer_id::text,scenario_ids,source,label,covered,unevaluable,matched_alert_id,matcher_version,assumptions,snapshot_at,provenance,created_at FROM coverage_analysis_matters WHERE analysis_id=$1`
	args := []any{filter.AnalysisID}
	if filter.ScenarioID != "" {
		query += fmt.Sprintf(" AND scenario_ids @> $%d::jsonb", len(args)+1)
		args = append(args, marshalJSON([]string{filter.ScenarioID}))
	}
	if filter.Label != "" {
		query += fmt.Sprintf(" AND label=$%d", len(args)+1)
		args = append(args, filter.Label)
	}
	if filter.Cursor != nil {
		query += fmt.Sprintf(" AND (created_at,id) > ($%d,$%d)", len(args)+1, len(args)+2)
		args = append(args, filter.Cursor.CreatedAt, filter.Cursor.ID)
	}
	query += " ORDER BY created_at ASC,id ASC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, filter.Limit)
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.CoverageMatterResult{}
	for rows.Next() {
		var item domain.CoverageMatterResult
		var scenarios, assumptions, provenance []byte
		if err := rows.Scan(&item.ID, &item.AnalysisID, &item.MatterID, &item.CustomerID, &scenarios, &item.Source, &item.Label, &item.Covered, &item.Unevaluable, &item.MatchedAlertID, &item.MatcherVersion, &assumptions, &item.SnapshotAt, &provenance, &item.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(scenarios, &item.ScenarioIDs)
		_ = json.Unmarshal(assumptions, &item.Assumptions)
		_ = json.Unmarshal(provenance, &item.Provenance)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgCoverageAnalysisRepo) ensureCoverageExists(ctx context.Context, id string) error {
	if _, err := r.GetCoverageAnalysis(ctx, id); err != nil {
		return err
	}
	return nil
}

var _ domain.CoverageAnalysisRepository = (*PgCoverageAnalysisRepo)(nil)
