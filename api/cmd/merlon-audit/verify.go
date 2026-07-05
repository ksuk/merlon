package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultDropThreshold is the default fraction (50%) by which a day's
// audit_logs count must fall below the preceding 7-day moving average to be
// flagged (audit.md §7).
const defaultDropThreshold = 0.5

// IDGap is a break in audit_logs.id's expected consecutive sequence, the
// simplest signal of deleted rows on a table that should be append-only
// (audit.md §7).
type IDGap struct {
	PreviousID int64 `json:"previous_id"`
	NextID     int64 `json:"next_id"`
}

// TimeRegression is a row whose created_at precedes an earlier-id row's
// created_at, which should never happen if audit_logs is only ever
// inserted into in real time.
type TimeRegression struct {
	ID                int64     `json:"id"`
	CreatedAt         time.Time `json:"created_at"`
	PreviousID        int64     `json:"previous_id"`
	PreviousCreatedAt time.Time `json:"previous_created_at"`
}

// CountDrop is a day whose audit_logs row count falls more than
// VerifyOptions.DropThreshold below the preceding 7-day moving average.
type CountDrop struct {
	Day           time.Time `json:"day"`
	Count         int       `json:"count"`
	MovingAverage float64   `json:"moving_average_7d"`
}

// VerifyOptions configures Verify (audit.md §7 CLI options table).
type VerifyOptions struct {
	Since         *time.Time
	Until         *time.Time
	DropThreshold float64
	Format        string
	// ReadOnly signals the connection cannot INSERT (audit.md §7: "read-only
	// 接続で実行可能なこと"), so callers should skip recording the verify
	// execution itself to the audit log. Verify does not use this field
	// directly (it only issues SELECT queries); run() in main.go reads it.
	ReadOnly bool
}

// VerifyResult holds every anomaly Verify found.
type VerifyResult struct {
	IDGaps          []IDGap          `json:"id_gaps"`
	TimeRegressions []TimeRegression `json:"time_regressions"`
	CountDrops      []CountDrop      `json:"count_drops"`
}

// HasAnomalies reports whether any anomaly was found (drives exit code 1
// vs 0, audit.md §7).
func (r VerifyResult) HasAnomalies() bool {
	return len(r.IDGaps) > 0 || len(r.TimeRegressions) > 0 || len(r.CountDrops) > 0
}

// Verify runs the three checks audit.md §7 defines against audit_logs,
// using only read queries so it works over a read-only connection.
func Verify(ctx context.Context, pool *pgxpool.Pool, opts VerifyOptions) (VerifyResult, error) {
	if opts.DropThreshold <= 0 {
		opts.DropThreshold = defaultDropThreshold
	}

	var result VerifyResult

	rows, err := pool.Query(ctx,
		`SELECT id, created_at FROM audit_logs
		WHERE ($1::timestamptz IS NULL OR created_at >= $1)
		  AND ($2::timestamptz IS NULL OR created_at <= $2)
		ORDER BY id`,
		opts.Since, opts.Until,
	)
	if err != nil {
		return VerifyResult{}, err
	}
	defer rows.Close()

	var prevID int64
	var prevCreatedAt time.Time
	first := true
	for rows.Next() {
		var id int64
		var createdAt time.Time
		if err := rows.Scan(&id, &createdAt); err != nil {
			return VerifyResult{}, err
		}
		if !first {
			if id > prevID+1 {
				result.IDGaps = append(result.IDGaps, IDGap{PreviousID: prevID, NextID: id})
			}
			if createdAt.Before(prevCreatedAt) {
				result.TimeRegressions = append(result.TimeRegressions, TimeRegression{
					ID: id, CreatedAt: createdAt,
					PreviousID: prevID, PreviousCreatedAt: prevCreatedAt,
				})
			}
		}
		prevID, prevCreatedAt, first = id, createdAt, false
	}
	if err := rows.Err(); err != nil {
		return VerifyResult{}, err
	}

	drops, err := detectCountDrops(ctx, pool, opts)
	if err != nil {
		return VerifyResult{}, err
	}
	result.CountDrops = drops

	return result, nil
}

func detectCountDrops(ctx context.Context, pool *pgxpool.Pool, opts VerifyOptions) ([]CountDrop, error) {
	rows, err := pool.Query(ctx,
		`SELECT date_trunc('day', created_at) AS day, COUNT(*)
		FROM audit_logs
		WHERE ($1::timestamptz IS NULL OR created_at >= $1)
		  AND ($2::timestamptz IS NULL OR created_at <= $2)
		GROUP BY day ORDER BY day`,
		opts.Since, opts.Until,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type dayCount struct {
		day   time.Time
		count int
	}
	var days []dayCount
	for rows.Next() {
		var d dayCount
		if err := rows.Scan(&d.day, &d.count); err != nil {
			return nil, err
		}
		days = append(days, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var drops []CountDrop
	for i := 7; i < len(days); i++ {
		var sum int
		for j := i - 7; j < i; j++ {
			sum += days[j].count
		}
		avg := float64(sum) / 7
		if avg == 0 {
			continue
		}
		if float64(days[i].count) < avg*(1-opts.DropThreshold) {
			drops = append(drops, CountDrop{Day: days[i].day, Count: days[i].count, MovingAverage: avg})
		}
	}
	return drops, nil
}
