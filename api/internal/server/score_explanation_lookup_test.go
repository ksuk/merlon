package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

// handleScoreExplanation read only the newest 200 score records and 404ed on
// anything older. For a customer rescored often -- which is exactly the
// customer whose history a reviewer wants to walk -- an explanation that
// exists became unreachable, and the API said "not found" for a record it
// holds.
func TestScoreExplanationFindsRecordOlderThanOnePage(t *testing.T) {
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	now := time.Now().UTC()
	customerID := "00000000000000000000000000000701"
	if err := customers.Create(ctx, &domain.Customer{
		ID: customerID, ExternalID: "explain-1", CustomerType: domain.CustomerTypeIndividual,
		CountryCode: "JP", Status: domain.CustomerStatusActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	// The oldest record is written first so it sits well beyond the newest 200.
	oldestID := generateID()
	total := 260
	for i := range total {
		id := generateID()
		if i == 0 {
			id = oldestID
		}
		if err := customers.SaveScoreRecord(ctx, &domain.ScoreRecord{
			ID: id, CustomerID: customerID, Score: 2, Tier: domain.RiskTierLow,
			Factors:  []domain.Factor{{Name: "f", Score: 2, Contribution: 2}},
			ScoredAt: now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}

	s := New(":0", Deps{
		Customers: customers, Alerts: store.NewMemoryAlertRepo(), Cases: store.NewMemoryCaseRepo(),
		Audit:   store.NewMemoryAuditRepo(),
		Scoring: &engine.MockScoringEngine{Score: 2, Tier: domain.RiskTierLow},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+customerID+"/scores/"+oldestID+"/explanation", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: a score record %d entries deep is still a record the API holds (body: %s)", rec.Code, total, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["score_id"] != oldestID {
		t.Errorf("score_id = %v, want %s", out["score_id"], oldestID)
	}
}

func TestScoreExplanationStillRejectsAnUnknownRecord(t *testing.T) {
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	now := time.Now().UTC()
	customerID := "00000000000000000000000000000702"
	if err := customers.Create(ctx, &domain.Customer{
		ID: customerID, ExternalID: "explain-2", CustomerType: domain.CustomerTypeIndividual,
		CountryCode: "JP", Status: domain.CustomerStatusActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := customers.SaveScoreRecord(ctx, &domain.ScoreRecord{
		ID: generateID(), CustomerID: customerID, Score: 1, Tier: domain.RiskTierLow, ScoredAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	s := New(":0", Deps{
		Customers: customers, Alerts: store.NewMemoryAlertRepo(), Cases: store.NewMemoryCaseRepo(),
		Audit: store.NewMemoryAuditRepo(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+customerID+"/scores/"+generateID()+"/explanation", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a score id that does not exist", rec.Code)
	}
}
