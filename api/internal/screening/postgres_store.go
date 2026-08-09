package screening

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresListStore struct{ pool *pgxpool.Pool }

func NewPostgresListStore(pool *pgxpool.Pool) *PostgresListStore {
	return &PostgresListStore{pool: pool}
}
func (s *PostgresListStore) SaveList(ctx context.Context, data *RawListData) error {
	b, err := json.Marshal(data.Entries)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO screening_list_snapshots(list_id,list_type,name,source,entries,imported_at) VALUES($1,$2,$3,$4,$5,now()) ON CONFLICT(list_id) DO UPDATE SET list_type=EXCLUDED.list_type,name=EXCLUDED.name,source=EXCLUDED.source,entries=EXCLUDED.entries,imported_at=EXCLUDED.imported_at`, data.ListID, data.ListType, data.Name, data.Source, b)
	return err
}
func (s *PostgresListStore) GetList(ctx context.Context, id string) (*RawListData, error) {
	var d RawListData
	var b []byte
	err := s.pool.QueryRow(ctx, `SELECT list_id,list_type,name,source,entries FROM screening_list_snapshots WHERE list_id=$1`, id).Scan(&d.ListID, &d.ListType, &d.Name, &d.Source, &b)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrListNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &d.Entries); err != nil {
		return nil, err
	}
	return &d, nil
}

type PostgresFailureTracker struct{ pool *pgxpool.Pool }

func NewPostgresFailureTracker(pool *pgxpool.Pool) *PostgresFailureTracker {
	return &PostgresFailureTracker{pool: pool}
}
func (s *PostgresFailureTracker) RecordSuccess(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO screening_list_failures(list_id,consecutive_failures,last_attempt_at,last_success_at,last_failure_at,last_error) VALUES($1,0,now(),now(),NULL,'') ON CONFLICT(list_id) DO UPDATE SET consecutive_failures=0,last_attempt_at=now(),last_success_at=now(),last_failure_at=NULL,last_error=''`, id)
	return err
}
func (s *PostgresFailureTracker) RecordFailure(ctx context.Context, id string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `INSERT INTO screening_list_failures(list_id,consecutive_failures,last_attempt_at,last_failure_at,last_error) VALUES($1,1,now(),now(),'import failed') ON CONFLICT(list_id) DO UPDATE SET consecutive_failures=screening_list_failures.consecutive_failures+1,last_attempt_at=now(),last_failure_at=now(),last_error='import failed' RETURNING consecutive_failures`, id).Scan(&n)
	return n, err
}
func (s *PostgresFailureTracker) ConsecutiveFailures(ctx context.Context, id string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT consecutive_failures FROM screening_list_failures WHERE list_id=$1`, id).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}
func (s *PostgresFailureTracker) LastSuccessAt(ctx context.Context, id string) (time.Time, error) {
	var at *time.Time
	err := s.pool.QueryRow(ctx, `SELECT last_success_at FROM screening_list_failures WHERE list_id=$1`, id).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) || at == nil {
		return time.Time{}, errNoSuccessYet
	}
	if err != nil {
		return time.Time{}, err
	}
	return *at, nil
}

// FailureStatus returns only the redacted fields needed by the operator
// readiness directory.  A missing tracker row is a normal never-attempted
// state, not a repository failure.
func (s *PostgresFailureTracker) FailureStatus(ctx context.Context, id string) (FailureStatus, error) {
	var status FailureStatus
	var attempt, success, failure *time.Time
	err := s.pool.QueryRow(ctx, `SELECT last_attempt_at,last_success_at,last_failure_at,consecutive_failures,COALESCE(last_error,'') FROM screening_list_failures WHERE list_id=$1`, id).
		Scan(&attempt, &success, &failure, &status.ConsecutiveFailures, &status.Diagnostic)
	if errors.Is(err, pgx.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return FailureStatus{}, err
	}
	status.LastAttemptAt = attempt
	status.LastSuccessAt = success
	status.LastFailureAt = failure
	return status, nil
}
