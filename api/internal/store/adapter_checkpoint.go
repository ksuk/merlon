package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/adapter"
)

type PgAdapterCheckpointRepo struct{ pool DBTX }

func NewPgAdapterCheckpointRepo(pool DBTX) *PgAdapterCheckpointRepo {
	return &PgAdapterCheckpointRepo{pool: pool}
}

func (r *PgAdapterCheckpointRepo) Acquire(ctx context.Context, id, owner string, now time.Time, lease time.Duration) (bool, error) {
	until := now.Add(lease)
	tag, err := r.pool.Exec(ctx, `INSERT INTO adapter_checkpoints(adapter_id,lease_owner,lease_until) VALUES($1,$2,$3) ON CONFLICT(adapter_id) DO UPDATE SET lease_owner=EXCLUDED.lease_owner, lease_until=EXCLUDED.lease_until WHERE adapter_checkpoints.lease_until IS NULL OR adapter_checkpoints.lease_until <= $4 OR adapter_checkpoints.lease_owner=$2`, id, owner, until, now)
	return tag.RowsAffected() > 0, err
}

func (r *PgAdapterCheckpointRepo) Get(ctx context.Context, id string) (*adapter.SyncCheckpoint, error) {
	var checkpoint adapter.SyncCheckpoint
	err := r.pool.QueryRow(ctx, `SELECT adapter_id,customer_cursor,transaction_cursor,customer_watermark,transaction_watermark,updated_at,adapter_digest FROM adapter_checkpoints WHERE adapter_id=$1`, id).Scan(&checkpoint.AdapterID, &checkpoint.CustomerCursor, &checkpoint.TransactionCursor, &checkpoint.CustomerWatermark, &checkpoint.TransactionWatermark, &checkpoint.UpdatedAt, &checkpoint.AdapterDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return &adapter.SyncCheckpoint{AdapterID: id}, nil
	}
	return &checkpoint, err
}

func (r *PgAdapterCheckpointRepo) Save(ctx context.Context, checkpoint *adapter.SyncCheckpoint) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO adapter_checkpoints(adapter_id,customer_cursor,transaction_cursor,customer_watermark,transaction_watermark,adapter_digest,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(adapter_id) DO UPDATE SET customer_cursor=EXCLUDED.customer_cursor,transaction_cursor=EXCLUDED.transaction_cursor,customer_watermark=EXCLUDED.customer_watermark,transaction_watermark=EXCLUDED.transaction_watermark,adapter_digest=EXCLUDED.adapter_digest,updated_at=EXCLUDED.updated_at`, checkpoint.AdapterID, checkpoint.CustomerCursor, checkpoint.TransactionCursor, checkpoint.CustomerWatermark, checkpoint.TransactionWatermark, checkpoint.AdapterDigest, checkpoint.UpdatedAt)
	return err
}

func (r *PgAdapterCheckpointRepo) Release(ctx context.Context, id, owner string) error {
	_, err := r.pool.Exec(ctx, `UPDATE adapter_checkpoints SET lease_owner='', lease_until=NULL WHERE adapter_id=$1 AND lease_owner=$2`, id, owner)
	return err
}

func (r *PgAdapterCheckpointRepo) StartSyncRun(ctx context.Context, run *adapter.SyncRun) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO adapter_sync_runs(id,adapter_id,status,adapter_digest,started_at) VALUES($1,$2,'running',$3,$4)`, run.ID, run.AdapterID, run.AdapterDigest, run.StartedAt)
	return err
}

func (r *PgAdapterCheckpointRepo) FinishSyncRun(ctx context.Context, run *adapter.SyncRun, runErr error) error {
	status := "completed"
	failure := ""
	if runErr != nil {
		status, failure = "failed", runErr.Error()
	} else if run.WaitingDependency > 0 || run.Failed > 0 {
		status = "partial"
	}
	run.Status, run.Error = status, failure
	checkpoint, _ := json.Marshal(run.Checkpoint)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE adapter_sync_runs SET status=$2,error=$3,completed_at=$4,customer_accepted=$5,customer_skipped=$6,transaction_accepted=$7,transaction_skipped=$8,waiting_dependency=$9,failed=$10,checkpoint=$11 WHERE id=$1`, run.ID, status, failure, run.CompletedAt, run.CustomerAccepted, run.CustomerSkipped, run.TransactionAccepted, run.TransactionSkipped, run.WaitingDependency, run.Failed, checkpoint); err != nil {
		return err
	}
	for _, outcome := range run.Outcomes {
		if _, err := tx.Exec(ctx, `INSERT INTO adapter_sync_outcomes(run_id,entity_type,external_id,status,reason) VALUES($1,$2,$3,$4,$5)`, run.ID, outcome.EntityType, outcome.ExternalID, outcome.Status, outcome.Reason); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
