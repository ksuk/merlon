package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

func TestCreateBacktestJobRequiresUTCWindowAndSelector(t *testing.T) {
	jobs := store.NewMemoryBacktestJobRepo()
	rules := store.NewMemoryRuleRepo()
	if err := rules.Create(context.Background(), &domain.RuleDefinition{Name: "v2", Type: domain.RuleTypeTMScenario, Definition: json.RawMessage(`{"scenario_id":"tm_structuring_basic"}`), IsActive: true}); err != nil {
		t.Fatal(err)
	}
	s := New(":0", Deps{BacktestJobs: jobs, Rules: rules, ConfigDigests: map[string]string{"tm": "abc"}})
	bad := `{"from":"2026-01-01T00:00:00+09:00","to":"2026-01-02T00:00:00Z","customer_ids":["c1"],"candidate_rule_set_id":"v2"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backtests", strings.NewReader(bad))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	good := `{"from":"2026-01-01T00:00:00Z","to":"2026-01-02T00:00:00Z","customer_ids":["c1"],"candidate_rule_set_id":"v2"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/backtests", strings.NewReader(good))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var job domain.BacktestJob
	if err := json.NewDecoder(rec.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.BacktestJobQueued || job.ConfigDigests["tm"] != "abc" {
		t.Fatalf("job=%+v", job)
	}
	stored, err := jobs.Get(context.Background(), job.ID)
	if err != nil || stored.CandidateRuleVersion != 1 || len(stored.CandidateRuleDefinition) == 0 {
		t.Fatalf("stored rule snapshot=%+v err=%v", stored, err)
	}
}
