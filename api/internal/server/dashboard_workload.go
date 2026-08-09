package server

import (
	"context"
	"net/http"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/policy"
)

// dueSoonWindow is how far ahead "due soon" looks. It is reported on the
// response rather than left for the UI to assume, so the figure and its
// definition cannot drift apart.
const dueSoonWindow = 24 * time.Hour

// dashboardWorkloadRepository is the aggregate a queue must expose to appear on
// the dashboard. It is an upcast rather than a required method on the main
// repository interface, matching how the existing dashboard aggregates are
// wired: a small in-memory composition that does not implement it produces an
// explicit failure rather than a silent zero.
type dashboardWorkloadRepository interface {
	DashboardWorkload(ctx context.Context, owner string, now time.Time, dueSoon time.Duration, slaConfigured bool) (domain.WorkloadCounts, error)
}

// dashboardWorkload assembles the ownership, age and SLA picture.
//
// The operator identity comes from the authenticated context, never from the
// request: "mine" is a claim about who is asking, and a client-supplied answer
// to that question is not evidence (OWN-01). A deployment with no identity
// reports an empty scope, and the UI shows the team and unassigned figures
// without pretending to know whose work is whose.
func (s *Server) dashboardWorkload(r *http.Request, now time.Time) (*domain.DashboardWorkload, error) {
	alertsRepo, ok := s.alerts.(dashboardWorkloadRepository)
	if !ok {
		return nil, nil
	}
	casesRepo, ok := s.cases.(dashboardWorkloadRepository)
	if !ok {
		return nil, nil
	}

	sla := s.policies.SLA()
	configured := sla.Configured()
	owner := resolveAuditUserID(r)

	alertCounts, err := alertsRepo.DashboardWorkload(r.Context(), owner, now, dueSoonWindow, configured)
	if err != nil {
		return nil, err
	}
	caseCounts, err := casesRepo.DashboardWorkload(r.Context(), owner, now, dueSoonWindow, configured)
	if err != nil {
		return nil, err
	}

	state := string(policy.SLANotConfigured)
	dueSoonHours := 0
	if configured {
		// The policy exists; whether any individual item has breached is a
		// per-item question the counts answer. What this field settles is
		// whether deadlines exist at all.
		state = string(policy.SLARunning)
		dueSoonHours = int(dueSoonWindow / time.Hour)
	}

	return &domain.DashboardWorkload{
		Scope:  owner,
		Alerts: alertCounts,
		Cases:  caseCounts,
		SLA: domain.DashboardSLA{
			State:              state,
			PolicyVersion:      sla.Version(),
			DueSoonWithinHours: dueSoonHours,
		},
		EvaluatedAt: now,
	}, nil
}

// dashboardExceptions summarises operational work that failed or degraded.
//
// Each entry carries the queue that explains it. A count an operator cannot
// open is a count they cannot act on, which is how the screening degradation
// added in Wave 3 could be true on the dashboard and invisible everywhere else.
func (s *Server) dashboardExceptions(ctx context.Context, stats *domain.DashboardStats, now time.Time) []domain.DashboardException {
	exceptions := make([]domain.DashboardException, 0, 3)

	if len(stats.ScreeningDegradedSources) > 0 {
		exceptions = append(exceptions, domain.DashboardException{
			Kind:  "screening_sources_degraded",
			Count: len(stats.ScreeningDegradedSources),
			Href:  "/screening-queue",
			State: "degraded",
		})
	}

	if s.pendingEvals != nil {
		if statsRepo, ok := s.pendingEvals.(domain.PendingEvaluationStatsRepository); ok {
			if pending, err := statsRepo.PendingEvaluationStats(ctx, now); err == nil {
				if pending.Failed > 0 {
					exceptions = append(exceptions, domain.DashboardException{
						Kind:  "pending_evaluations_failed",
						Count: pending.Failed,
						Href:  "/pending-evaluations?status=failed",
						State: "failed",
					})
				}
				if pending.Exhausted > 0 {
					// Exhausted work will not retry on its own. It is a
					// different call to action from a transient failure.
					exceptions = append(exceptions, domain.DashboardException{
						Kind:  "pending_evaluations_exhausted",
						Count: pending.Exhausted,
						Href:  "/pending-evaluations?status=exhausted",
						State: "failed",
					})
				}
			}
		}
	}

	return exceptions
}
