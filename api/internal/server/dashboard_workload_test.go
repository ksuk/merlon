package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/policy"
	"github.com/ksuk/merlon/api/internal/store"
)

func workloadServer(t *testing.T, policies *policy.Set) (*Server, *store.MemoryAlertRepo, *store.MemoryCaseRepo) {
	t.Helper()
	alerts := store.NewMemoryAlertRepo()
	cases := store.NewMemoryCaseRepo()
	s := New(":0", Deps{
		Customers:          store.NewMemoryCustomerRepo(),
		Transactions:       store.NewMemoryTransactionRepo(),
		Alerts:             alerts,
		Cases:              cases,
		CaseAlertLifecycle: store.NewMemoryCaseAlertLifecycleRepo(cases, alerts),
		Policies:           policies,
	})
	return s, alerts, cases
}

func fetchDashboard(t *testing.T, s *Server) domain.DashboardStats {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var stats domain.DashboardStats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return stats
}

func seedOpenAlert(t *testing.T, alerts *store.MemoryAlertRepo, assignedTo, assignedTeam string, detectedAt time.Time, due *time.Time) {
	t.Helper()
	if err := alerts.Create(context.Background(), &domain.Alert{
		ID: generateID(), CustomerID: generateID(), ScenarioID: "tm_structuring_basic",
		Severity: domain.AlertSeverityHigh, Status: domain.AlertStatusOpen,
		AssignedTo: assignedTo, AssignedTeam: assignedTeam, DueAt: due,
		DetectedAt: detectedAt, CreatedAt: detectedAt, UpdatedAt: detectedAt,
	}); err != nil {
		t.Fatalf("create alert: %v", err)
	}
}

// TestDashboardWorkload_ReportsOwnershipAndAge is the start-of-day question the
// page could not answer: what is unassigned, and what has been sitting.
func TestDashboardWorkload_ReportsOwnershipAndAge(t *testing.T) {
	s, alerts, _ := workloadServer(t, nil)
	now := time.Now().UTC()

	seedOpenAlert(t, alerts, "", "", now.Add(-2*time.Hour), nil)
	seedOpenAlert(t, alerts, "", "aml-team", now.Add(-30*time.Hour), nil)
	seedOpenAlert(t, alerts, "analyst@example.com", "", now.Add(-10*24*time.Hour), nil)

	stats := fetchDashboard(t, s)
	if stats.Workload == nil {
		t.Fatal("dashboard reported no workload at all")
	}

	if stats.Workload.Alerts.Open != 3 {
		t.Errorf("open = %d, want 3", stats.Workload.Alerts.Open)
	}
	// Assigned to a team is owned; only the first alert is unassigned.
	if stats.Workload.Alerts.Unassigned != 1 {
		t.Errorf("unassigned = %d, want 1", stats.Workload.Alerts.Unassigned)
	}
	if stats.Workload.Alerts.OldestOpenAt == nil || stats.Workload.Alerts.OldestAgeSeconds == nil {
		t.Fatal("the oldest open item was not reported")
	}
	if *stats.Workload.Alerts.OldestAgeSeconds < int64((9 * 24 * time.Hour).Seconds()) {
		t.Errorf("oldest age = %ds, want roughly ten days", *stats.Workload.Alerts.OldestAgeSeconds)
	}

	byLabel := map[string]int{}
	for _, bucket := range stats.Workload.Alerts.AgeBuckets {
		byLabel[bucket.Label] = bucket.Count
	}
	if byLabel["under_24h"] != 1 || byLabel["1_to_3d"] != 1 || byLabel["over_7d"] != 1 {
		t.Errorf("age buckets = %v, want one item in each of under_24h, 1_to_3d and over_7d", byLabel)
	}
	// The boundaries the server used travel with the counts, so the UI never
	// restates a band the server did not apply.
	for _, bucket := range stats.Workload.Alerts.AgeBuckets {
		if bucket.Label == "" {
			t.Error("an age bucket has no label")
		}
	}
}

// TestDashboardWorkload_UnconfiguredSLAIsNotZeroOverdue is the core of #79:
// a deadline nobody set must not be reported as a deadline being met.
func TestDashboardWorkload_UnconfiguredSLAIsNotZeroOverdue(t *testing.T) {
	s, alerts, _ := workloadServer(t, nil)
	now := time.Now().UTC()
	past := now.Add(-time.Hour)

	// This alert carries a client-supplied due_at that has already passed.
	seedOpenAlert(t, alerts, "", "", now.Add(-5*time.Hour), &past)

	stats := fetchDashboard(t, s)

	if stats.Workload.SLA.State != string(policy.SLANotConfigured) {
		t.Errorf("sla state = %q, want %q", stats.Workload.SLA.State, policy.SLANotConfigured)
	}
	if stats.Workload.Alerts.Overdue != nil {
		t.Errorf("overdue = %d; with no SLA policy there is no deadline to be overdue against, and zero would read as healthy", *stats.Workload.Alerts.Overdue)
	}
	if stats.Workload.Alerts.DueSoon != nil {
		t.Error("due_soon was reported without a configured policy")
	}
}

// TestDashboardWorkload_ConfiguredSLACountsOverdue: once a deployment declares
// deadlines, the same data produces a number.
func TestDashboardWorkload_ConfiguredSLACountsOverdue(t *testing.T) {
	policies, err := policy.Load(policy.Paths{SLA: writeSLAPolicy(t)})
	if err != nil {
		t.Fatalf("load policies: %v", err)
	}
	s, alerts, _ := workloadServer(t, policies)
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	soon := now.Add(2 * time.Hour)

	seedOpenAlert(t, alerts, "", "", now.Add(-5*time.Hour), &past)
	seedOpenAlert(t, alerts, "", "", now.Add(-time.Hour), &soon)
	seedOpenAlert(t, alerts, "", "", now.Add(-time.Hour), nil)

	stats := fetchDashboard(t, s)

	if stats.Workload.SLA.State == string(policy.SLANotConfigured) {
		t.Fatal("sla reported not_configured after a policy was loaded")
	}
	if stats.Workload.Alerts.Overdue == nil || *stats.Workload.Alerts.Overdue != 1 {
		t.Errorf("overdue = %v, want 1", stats.Workload.Alerts.Overdue)
	}
	if stats.Workload.Alerts.DueSoon == nil || *stats.Workload.Alerts.DueSoon != 1 {
		t.Errorf("due_soon = %v, want 1", stats.Workload.Alerts.DueSoon)
	}
	if stats.Workload.SLA.DueSoonWithinHours != 24 {
		t.Errorf("due_soon_within_hours = %d, want 24: the window must travel with the count", stats.Workload.SLA.DueSoonWithinHours)
	}
	if stats.Workload.SLA.PolicyVersion == "" {
		t.Error("the policy version that produced these deadlines was not reported")
	}
}

// TestDashboardWorkload_OpenCountReconcilesWithTheQueue is the acceptance
// criterion that makes drill-down meaningful: the tile and its destination must
// agree.
func TestDashboardWorkload_OpenCountReconcilesWithTheQueue(t *testing.T) {
	s, alerts, _ := workloadServer(t, nil)
	now := time.Now().UTC()

	for i := 0; i < 4; i++ {
		seedOpenAlert(t, alerts, "", "", now.Add(-time.Duration(i)*time.Hour), nil)
	}
	// A closed alert must appear in neither.
	if err := alerts.Create(context.Background(), &domain.Alert{
		ID: generateID(), CustomerID: generateID(), ScenarioID: "tm_structuring_basic",
		Severity: domain.AlertSeverityLow, Status: domain.AlertStatusClosedFalsePositive,
		DetectedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create closed alert: %v", err)
	}

	stats := fetchDashboard(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?active=true&limit=100", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var page struct {
		Data []domain.Alert `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode queue: %v", err)
	}

	if stats.Workload.Alerts.Open != len(page.Data) {
		t.Errorf("dashboard reports %d open alerts but the queue it links to returns %d",
			stats.Workload.Alerts.Open, len(page.Data))
	}
}

// TestDashboardExceptions_NameTheQueueThatExplainsThem: a count an operator
// cannot open is a count they cannot act on.
func TestDashboardExceptions_NameTheQueueThatExplainsThem(t *testing.T) {
	s, _, _ := workloadServer(t, nil)

	stats := fetchDashboard(t, s)

	if stats.Exceptions == nil {
		t.Fatal("exceptions is null; an empty list means nothing is failing, null means nothing was checked")
	}
	for _, exception := range stats.Exceptions {
		if exception.Href == "" {
			t.Errorf("exception %q has no destination", exception.Kind)
		}
		if exception.State == "" {
			t.Errorf("exception %q does not say whether it failed or degraded", exception.Kind)
		}
	}
}

func writeSLAPolicy(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/sla.yaml"
	content := "schema_version: sla_policy_v1\npolicy_version: \"test\"\nrules:\n  - kind: alert\n    within_hours: 72\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return path
}
