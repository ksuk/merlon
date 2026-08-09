package store

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/domain"
)

// Workload aggregation for the operations dashboard.
//
// The dashboard previously reported only totals, so an operator could not tell
// what was theirs, what nobody owned, or what had been sitting untouched (#79).
// These aggregates answer that from the ownership columns migration 039 already
// added; no new table or column is required (ADR-0024, DR-06).
//
// The counts are computed by the store rather than by listing rows and counting
// in the handler, so a deployment with a hundred thousand open alerts answers
// the dashboard without materialising them.

// ageBucketBounds are the documented age bands, in hours. The final bucket is
// open-ended. They are declared once here and reported on the response, so the
// UI never restates a boundary the server did not use.
var ageBucketBounds = []struct {
	label string
	from  int
	to    int
}{
	{"under_24h", 0, 24},
	{"1_to_3d", 24, 72},
	{"3_to_7d", 72, 168},
	{"over_7d", 168, 0},
}

func emptyAgeBuckets() []domain.AgeBucket {
	buckets := make([]domain.AgeBucket, 0, len(ageBucketBounds))
	for _, bound := range ageBucketBounds {
		buckets = append(buckets, domain.AgeBucket{Label: bound.label, FromHours: bound.from, ToHours: bound.to})
	}
	return buckets
}

// addToAgeBucket places one item by how long it has been open.
func addToAgeBucket(buckets []domain.AgeBucket, opened, now time.Time) {
	if opened.IsZero() {
		return
	}
	hours := now.Sub(opened).Hours()
	if hours < 0 {
		// Clock skew between the writer and this process. Treating it as newly
		// arrived is the conservative reading; inventing a negative age is not.
		hours = 0
	}
	for i, bound := range ageBucketBounds {
		if hours >= float64(bound.from) && (bound.to == 0 || hours < float64(bound.to)) {
			buckets[i].Count++
			return
		}
	}
}

// workloadAccumulator collects one queue's picture as rows are visited.
type workloadAccumulator struct {
	counts        domain.WorkloadCounts
	owner         string
	now           time.Time
	dueSoon       time.Duration
	slaConfigured bool
	overdue       int
	dueSoonCount  int
}

func newWorkloadAccumulator(owner string, now time.Time, dueSoon time.Duration, slaConfigured bool) *workloadAccumulator {
	return &workloadAccumulator{
		counts:        domain.WorkloadCounts{AgeBuckets: emptyAgeBuckets()},
		owner:         owner,
		now:           now,
		dueSoon:       dueSoon,
		slaConfigured: slaConfigured,
	}
}

func (a *workloadAccumulator) add(assignedTo, assignedTeam string, opened time.Time, due *time.Time) {
	a.counts.Open++

	if a.owner != "" && strings.EqualFold(strings.TrimSpace(assignedTo), a.owner) {
		a.counts.Mine++
	}
	if strings.TrimSpace(assignedTo) == "" && strings.TrimSpace(assignedTeam) == "" {
		a.counts.Unassigned++
	}

	if !opened.IsZero() {
		if a.counts.OldestOpenAt == nil || opened.Before(*a.counts.OldestOpenAt) {
			oldest := opened
			a.counts.OldestOpenAt = &oldest
		}
	}
	addToAgeBucket(a.counts.AgeBuckets, opened, a.now)

	if due != nil {
		if a.now.After(*due) {
			a.overdue++
		} else if a.dueSoon > 0 && due.Sub(a.now) <= a.dueSoon {
			a.dueSoonCount++
		}
	}
}

func (a *workloadAccumulator) result() domain.WorkloadCounts {
	counts := a.counts
	if counts.OldestOpenAt != nil {
		age := int64(a.now.Sub(*counts.OldestOpenAt).Seconds())
		if age < 0 {
			age = 0
		}
		counts.OldestAgeSeconds = &age
	}
	// Overdue is reported only when deadlines exist. Zero would read as "none
	// overdue", which is a claim an unconfigured deployment cannot make.
	if a.slaConfigured {
		overdue := a.overdue
		dueSoon := a.dueSoonCount
		counts.Overdue = &overdue
		counts.DueSoon = &dueSoon
	}
	return counts
}

// DashboardWorkload aggregates the open alert queue.
func (r *MemoryAlertRepo) DashboardWorkload(_ context.Context, owner string, now time.Time, dueSoon time.Duration, slaConfigured bool) (domain.WorkloadCounts, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	acc := newWorkloadAccumulator(owner, now, dueSoon, slaConfigured)
	for _, a := range r.data {
		if !domain.IsAlertUnresolved(a.Status) {
			continue
		}
		acc.add(a.AssignedTo, a.AssignedTeam, a.DetectedAt, a.DueAt)
	}
	return acc.result(), nil
}

// DashboardWorkload aggregates the open case queue.
func (r *MemoryCaseRepo) DashboardWorkload(_ context.Context, owner string, now time.Time, dueSoon time.Duration, slaConfigured bool) (domain.WorkloadCounts, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	acc := newWorkloadAccumulator(owner, now, dueSoon, slaConfigured)
	for _, c := range r.data {
		if domain.IsCaseTerminal(c.Status) {
			continue
		}
		acc.add(c.AssignedTo, c.AssignedTeam, c.CreatedAt, c.DueAt)
	}
	return acc.result(), nil
}

// DashboardWorkload aggregates the open alert queue in PostgreSQL.
//
// The aggregation runs in one statement over idx_alerts_queue_owner
// (migration 039) rather than reading rows into the process.
func (r *PgAlertRepo) DashboardWorkload(ctx context.Context, owner string, now time.Time, dueSoon time.Duration, slaConfigured bool) (domain.WorkloadCounts, error) {
	return pgDashboardWorkload(ctx, r.pool, workloadQuery{
		table:      "alerts",
		openedCol:  "detected_at",
		openFilter: "status NOT IN ('closed_true_positive', 'closed_false_positive')",
	}, owner, now, dueSoon, slaConfigured)
}

// DashboardWorkload aggregates the open case queue in PostgreSQL.
func (r *PgCaseRepo) DashboardWorkload(ctx context.Context, owner string, now time.Time, dueSoon time.Duration, slaConfigured bool) (domain.WorkloadCounts, error) {
	return pgDashboardWorkload(ctx, r.pool, workloadQuery{
		table:      "cases",
		openedCol:  "created_at",
		openFilter: "status NOT IN ('closed', 'str_filed')",
	}, owner, now, dueSoon, slaConfigured)
}

// workloadQuerier is the narrow slice of the pool this aggregate needs, kept
// as an interface so the query can be exercised without a live database.
type workloadQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type workloadQuery struct {
	table      string
	openedCol  string
	openFilter string
}

func pgDashboardWorkload(ctx context.Context, pool workloadQuerier, q workloadQuery, owner string, now time.Time, dueSoon time.Duration, slaConfigured bool) (domain.WorkloadCounts, error) {
	counts := domain.WorkloadCounts{AgeBuckets: emptyAgeBuckets()}

	sql := `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE lower(COALESCE(assigned_to, '')) = lower($1) AND $1 <> ''),
			COUNT(*) FILTER (WHERE NULLIF(BTRIM(COALESCE(assigned_to, '')), '') IS NULL AND NULLIF(BTRIM(COALESCE(assigned_team, '')), '') IS NULL),
			MIN(` + q.openedCol + `),
			COUNT(*) FILTER (WHERE ` + q.openedCol + ` > $2 - INTERVAL '24 hours'),
			COUNT(*) FILTER (WHERE ` + q.openedCol + ` <= $2 - INTERVAL '24 hours' AND ` + q.openedCol + ` > $2 - INTERVAL '72 hours'),
			COUNT(*) FILTER (WHERE ` + q.openedCol + ` <= $2 - INTERVAL '72 hours' AND ` + q.openedCol + ` > $2 - INTERVAL '168 hours'),
			COUNT(*) FILTER (WHERE ` + q.openedCol + ` <= $2 - INTERVAL '168 hours'),
			COUNT(*) FILTER (WHERE due_at IS NOT NULL AND due_at < $2),
			COUNT(*) FILTER (WHERE due_at IS NOT NULL AND due_at >= $2 AND due_at <= $3)
		FROM ` + q.table + `
		WHERE purge_marked_at IS NULL AND ` + q.openFilter

	var total, mine, unassigned int
	var oldest *time.Time
	var b0, b1, b2, b3, overdue, soon int
	if err := pool.QueryRow(ctx, sql, owner, now, now.Add(dueSoon)).Scan(
		&total, &mine, &unassigned, &oldest, &b0, &b1, &b2, &b3, &overdue, &soon,
	); err != nil {
		return domain.WorkloadCounts{}, err
	}

	counts.Open = total
	counts.Mine = mine
	counts.Unassigned = unassigned
	counts.OldestOpenAt = oldest
	counts.AgeBuckets[0].Count = b0
	counts.AgeBuckets[1].Count = b1
	counts.AgeBuckets[2].Count = b2
	counts.AgeBuckets[3].Count = b3
	if oldest != nil {
		age := int64(now.Sub(*oldest).Seconds())
		if age < 0 {
			age = 0
		}
		counts.OldestAgeSeconds = &age
	}
	if slaConfigured {
		counts.Overdue = &overdue
		counts.DueSoon = &soon
	}
	return counts, nil
}
