package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

// #72 requires the preview to display "target criteria, total count,
// excluded/ineligible count, operation, mode, rule set/version, and expected
// side effects". Everything but the excluded count and the side effects was
// already there, so an operator saw how many customers would be touched but
// not how many had been dropped from their selection, nor what the run would
// actually do to them.

func newTargetScopeServer(t *testing.T) (*Server, map[string]string) {
	t.Helper()
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	now := time.Now().UTC()
	ids := map[string]string{
		"active":  "00000000000000000000000000000901",
		"closed":  "00000000000000000000000000000902",
		"dormant": "00000000000000000000000000000903",
	}
	for name, id := range ids {
		status := domain.CustomerStatusActive
		switch name {
		case "closed":
			status = domain.CustomerStatusClosed
		case "dormant":
			status = domain.CustomerStatusDormant
		}
		if err := customers.Create(ctx, &domain.Customer{
			ID: id, ExternalID: "scope-" + name, CustomerType: domain.CustomerTypeIndividual,
			CountryCode: "JP", Status: status, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	s := New(":0", Deps{
		Customers: customers, Transactions: store.NewMemoryTransactionRepo(),
		Alerts: store.NewMemoryAlertRepo(), Cases: store.NewMemoryCaseRepo(),
		Audit: store.NewMemoryAuditRepo(), Wave3: store.NewMemoryWave3Repo(),
		BatchRuns:  store.NewMemoryBatchRunRepo(),
		Scoring:    &engine.MockScoringEngine{Score: 1, Tier: domain.RiskTierLow},
		Monitoring: &engine.MockMonitoringEngine{},
	})
	return s, ids
}

func previewTarget(t *testing.T, s *Server, body string) (*httptest.ResponseRecorder, domain.TargetManifest) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/targets/preview", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var m domain.TargetManifest
	if rec.Code == http.StatusCreated {
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("decode manifest: %v (body %s)", err, rec.Body.String())
		}
	}
	return rec, m
}

func TestTargetPreviewCountsIneligibleCustomers(t *testing.T) {
	s, ids := newTargetScopeServer(t)

	// A monitoring run never evaluates a closed customer, and never evaluates
	// a dormant one on a batch pass. Counting them in target_count would tell
	// the operator work will happen that cannot happen.
	rec, manifest := previewTarget(t, s, `{"operation":"batch_monitor","target_mode":"all"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	if manifest.TargetCount != 1 {
		t.Errorf("target_count = %d, want 1 (only the active customer is evaluated on a batch monitor pass)", manifest.TargetCount)
	}
	if manifest.ExcludedCount != 2 {
		t.Errorf("excluded_count = %d, want 2 (closed and dormant)", manifest.ExcludedCount)
	}
	if manifest.ExcludedReasons["closed"] != 1 || manifest.ExcludedReasons["dormant"] != 1 {
		t.Errorf("excluded_reasons = %v, want one closed and one dormant", manifest.ExcludedReasons)
	}
	for _, id := range manifest.CustomerIDs {
		if id == ids["closed"] || id == ids["dormant"] {
			t.Errorf("ineligible customer %s is still in the pinned population", id)
		}
	}
}

func TestTargetPreviewScoringExcludesOnlyClosed(t *testing.T) {
	s, _ := newTargetScopeServer(t)

	// Scoring a dormant customer is meaningful -- the tier still drives
	// rescreening frequency -- so only closed customers drop out.
	rec, manifest := previewTarget(t, s, `{"operation":"batch_score","target_mode":"all"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	if manifest.TargetCount != 2 {
		t.Errorf("target_count = %d, want 2 (active and dormant)", manifest.TargetCount)
	}
	if manifest.ExcludedCount != 1 || manifest.ExcludedReasons["closed"] != 1 {
		t.Errorf("excluded_count=%d reasons=%v, want exactly one closed customer excluded", manifest.ExcludedCount, manifest.ExcludedReasons)
	}
}

func TestTargetPreviewStatesExpectedSideEffects(t *testing.T) {
	s, _ := newTargetScopeServer(t)

	rec, manifest := previewTarget(t, s, `{"operation":"batch_monitor","target_mode":"all"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	if len(manifest.ExpectedSideEffects) == 0 {
		t.Fatal("expected_side_effects is empty; an operator confirming a bulk run must be told what it will do")
	}
	joined := strings.Join(manifest.ExpectedSideEffects, " ")
	if !strings.Contains(joined, "alert") {
		t.Errorf("expected_side_effects = %v, want the alert-generating effect named for a monitoring run", manifest.ExpectedSideEffects)
	}
}

func TestTargetPreviewRejectsAnEntirelyIneligibleSelection(t *testing.T) {
	s, ids := newTargetScopeServer(t)

	// Every named customer is ineligible, so the run would do nothing. Pinning
	// a manifest of zero targets invites a confirmation for no work.
	rec, _ := previewTarget(t, s, `{"operation":"batch_monitor","target_mode":"selected","customer_ids":["`+ids["closed"]+`"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when no selected customer is eligible, body: %s", rec.Code, rec.Body.String())
	}
}
