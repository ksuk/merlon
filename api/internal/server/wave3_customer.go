package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
)

func (s *Server) handleListCustomerIdentityHistory(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.wave3.(domain.CustomerIdentityHistoryRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "identity history store not configured")
		return
	}
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	items, err := repo.ListCustomerIdentityHistory(r.Context(), r.PathValue("id"), pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	page, meta := BuildPaginationMeta(items, pageReq.Limit, func(item domain.CustomerIdentityHistoryEntry) Cursor {
		return Cursor{CreatedAt: item.CreatedAt, ID: item.ID}
	})
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

type investigationResponse struct {
	Customer         *domain.Customer               `json:"customer"`
	Counts           map[string]int                 `json:"counts"`
	Pagination       map[string]PaginationMeta      `json:"pagination"`
	Transactions     []domain.Transaction           `json:"transactions"`
	Alerts           []domain.Alert                 `json:"alerts"`
	Cases            []domain.Case                  `json:"cases"`
	ScreeningResults []domain.ScreeningResultRecord `json:"screening_results"`
	ScoreHistory     []domain.ScoreRecord           `json:"score_history"`
	Timeline         []investigationTimelineEntry   `json:"timeline"`
	EDD              investigationEDDPanel          `json:"edd"`
	Freshness        time.Time                      `json:"freshness"`
	PartialFailures  []string                       `json:"partial_failures"`
}

// investigationTimelineEntry is a read-model event, not a second audit
// ledger. It points back to the canonical domain row so a 360 drill-down can
// identify the source of every displayed event.
type investigationTimelineEntry struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	EntityID  string    `json:"entity_id"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type investigationEDDPanel struct {
	Required         bool       `json:"required"`
	RequestedAt      *time.Time `json:"requested_at,omitempty"`
	Stage1LastSentAt *time.Time `json:"stage1_last_sent_at,omitempty"`
	Stage2NotifiedAt *time.Time `json:"stage2_notified_at,omitempty"`
	Stage3NotifiedAt *time.Time `json:"stage3_notified_at,omitempty"`
	CurrentStage     string     `json:"current_stage"`
	ElapsedDays      int        `json:"elapsed_days"`
	RemainingDays    int        `json:"remaining_days"`
	NextStage        string     `json:"next_stage"`
	NextStageAt      *time.Time `json:"next_stage_at,omitempty"`
	CompletionStatus string     `json:"completion_status"`
}

func buildInvestigationEDDPanel(customer *domain.Customer, stage2Days, stage3Days int) investigationEDDPanel {
	if customer == nil {
		return investigationEDDPanel{}
	}
	if stage2Days <= 0 {
		stage2Days = 60
	}
	if stage3Days <= 0 {
		stage3Days = 90
	}
	panel := investigationEDDPanel{
		Required:         customer.EddRequestedAt != nil,
		RequestedAt:      customer.EddRequestedAt,
		Stage1LastSentAt: customer.EddStage1LastSentAt,
		Stage2NotifiedAt: customer.EddStage2NotifiedAt,
		Stage3NotifiedAt: customer.EddStage3NotifiedAt,
		CurrentStage:     "none",
		NextStage:        "none",
		CompletionStatus: "not_required",
	}
	if customer.EddRequestedAt == nil {
		return panel
	}
	now := time.Now().UTC()
	requestedAt := customer.EddRequestedAt.UTC()
	if elapsed := now.Sub(requestedAt); elapsed > 0 {
		panel.ElapsedDays = int(elapsed / (24 * time.Hour))
	}
	panel.CompletionStatus = "open"
	panel.NextStage = "stage1"
	panel.NextStageAt = timePtr(requestedAt.Add(30 * 24 * time.Hour))
	if panel.ElapsedDays >= 30 {
		panel.NextStage = "stage2"
		panel.NextStageAt = timePtr(requestedAt.Add(time.Duration(stage2Days) * 24 * time.Hour))
	}
	if panel.ElapsedDays >= stage2Days {
		panel.NextStage = "stage3"
		panel.NextStageAt = timePtr(requestedAt.Add(time.Duration(stage3Days) * 24 * time.Hour))
	}
	if customer.EddStage3NotifiedAt != nil {
		panel.NextStage = "none"
		panel.NextStageAt = nil
		panel.CompletionStatus = "escalated"
	}
	if panel.NextStageAt != nil {
		if remaining := int(panel.NextStageAt.Sub(now) / (24 * time.Hour)); remaining > 0 {
			panel.RemainingDays = remaining
		}
	}
	if customer.EddStage3NotifiedAt != nil {
		panel.CurrentStage = "critical"
	} else if customer.EddStage2NotifiedAt != nil {
		panel.CurrentStage = "stage2"
	} else if customer.EddStage1LastSentAt != nil {
		panel.CurrentStage = "stage1"
	} else if customer.EddRequestedAt != nil {
		panel.CurrentStage = "requested"
	}
	return panel
}

func buildInvestigationTimeline(out investigationResponse, limit int) ([]investigationTimelineEntry, PaginationMeta) {
	items := make([]investigationTimelineEntry, 0, len(out.Transactions)+len(out.Alerts)+len(out.Cases)+len(out.ScreeningResults)+len(out.ScoreHistory))
	for _, item := range out.Transactions {
		items = append(items, investigationTimelineEntry{ID: "transaction:" + item.ID, Kind: "transaction", EntityID: item.ID, Summary: item.ExternalID, CreatedAt: item.ExecutedAt})
	}
	for _, item := range out.Alerts {
		items = append(items, investigationTimelineEntry{ID: "alert:" + item.ID, Kind: "alert", EntityID: item.ID, Summary: string(item.Severity) + " " + item.ScenarioID, CreatedAt: item.DetectedAt})
	}
	for _, item := range out.Cases {
		items = append(items, investigationTimelineEntry{ID: "case:" + item.ID, Kind: "case", EntityID: item.ID, Summary: item.Summary, CreatedAt: item.CreatedAt})
	}
	for _, item := range out.ScreeningResults {
		items = append(items, investigationTimelineEntry{ID: "screening_result:" + item.ID, Kind: "screening_result", EntityID: item.ID, Summary: item.MatchedName, CreatedAt: item.ScreenedAt})
	}
	for _, item := range out.ScoreHistory {
		items = append(items, investigationTimelineEntry{ID: "score:" + item.ID, Kind: "score", EntityID: item.ID, Summary: string(item.Tier), CreatedAt: item.ScoredAt})
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].ID > items[j].ID
	})
	meta := PaginationMeta{}
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if len(items) > limit {
		meta.HasMore = true
		items = items[:limit]
	}
	return items, meta
}

func (s *Server) handleCustomerInvestigation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	customer, err := s.customers.Get(r.Context(), id)
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	out := investigationResponse{Customer: customer, Counts: map[string]int{}, Pagination: map[string]PaginationMeta{}, Transactions: []domain.Transaction{}, Alerts: []domain.Alert{}, Cases: []domain.Case{}, ScreeningResults: []domain.ScreeningResultRecord{}, ScoreHistory: []domain.ScoreRecord{}, Timeline: []investigationTimelineEntry{}, EDD: buildInvestigationEDDPanel(customer, s.eddStage2Days, s.eddStage3Days), Freshness: time.Now().UTC(), PartialFailures: []string{}}
	if txns, err := s.transactions.ListByCustomerCursor(r.Context(), id, pageReq.Limit+1, toDomainCursor(pageReq.Cursor)); err != nil {
		out.PartialFailures = append(out.PartialFailures, "transactions")
	} else {
		out.Transactions, out.Pagination["transactions"] = BuildPaginationMeta(txns, pageReq.Limit, transactionCursor)
		out.Counts["transactions"] = len(out.Transactions)
		if countRepo, ok := s.transactions.(domain.CustomerScopedCountRepository); ok {
			if count, countErr := countRepo.CountByCustomer(r.Context(), id); countErr == nil {
				out.Counts["transactions"] = count
			} else {
				out.PartialFailures = append(out.PartialFailures, "transactions_count")
			}
		}
	}
	if s.alerts != nil {
		if alerts, err := s.alerts.ListByCustomerCursor(r.Context(), id, pageReq.Limit+1, toDomainCursor(pageReq.Cursor)); err != nil {
			out.PartialFailures = append(out.PartialFailures, "alerts")
		} else {
			out.Alerts, out.Pagination["alerts"] = BuildPaginationMeta(alerts, pageReq.Limit, func(alert domain.Alert) Cursor { return Cursor{CreatedAt: alert.CreatedAt, ID: alert.ID} })
			out.Counts["alerts"] = len(out.Alerts)
			if countRepo, ok := s.alerts.(domain.CustomerScopedCountRepository); ok {
				if count, countErr := countRepo.CountByCustomer(r.Context(), id); countErr == nil {
					out.Counts["alerts"] = count
				} else {
					out.PartialFailures = append(out.PartialFailures, "alerts_count")
				}
			}
		}
	}
	if s.cases != nil {
		if cases, err := s.cases.ListByCustomerCursor(r.Context(), id, pageReq.Limit+1, toDomainCursor(pageReq.Cursor)); err != nil {
			out.PartialFailures = append(out.PartialFailures, "cases")
		} else {
			out.Cases, out.Pagination["cases"] = BuildPaginationMeta(cases, pageReq.Limit, func(item domain.Case) Cursor { return Cursor{CreatedAt: item.CreatedAt, ID: item.ID} })
			out.Counts["cases"] = len(out.Cases)
			if countRepo, ok := s.cases.(domain.CustomerScopedCountRepository); ok {
				if count, countErr := countRepo.CountByCustomer(r.Context(), id); countErr == nil {
					out.Counts["cases"] = count
				} else {
					out.PartialFailures = append(out.PartialFailures, "cases_count")
				}
			}
		}
	}
	if wf, ok := s.wave3.(domain.ScreeningWorkflowRepository); ok {
		filter := domain.ScreeningResultFilter{CustomerID: id, Cursor: toDomainCursor(pageReq.Cursor)}
		if results, err := wf.ListScreeningResults(r.Context(), filter, pageReq.Limit+1); err != nil {
			out.PartialFailures = append(out.PartialFailures, "screening_results")
		} else {
			out.ScreeningResults, out.Pagination["screening_results"] = BuildPaginationMeta(results, pageReq.Limit, screeningResultCursor)
			out.Counts["screening_results"] = len(out.ScreeningResults)
			if countRepo, ok := s.wave3.(domain.CustomerScopedCountRepository); ok {
				if count, countErr := countRepo.CountByCustomer(r.Context(), id); countErr == nil {
					out.Counts["screening_results"] = count
				} else {
					out.PartialFailures = append(out.PartialFailures, "screening_results_count")
				}
			}
		}
	} else if s.screeningResults != nil {
		if results, err := s.screeningResults.ListByCustomer(r.Context(), id, pageReq.Limit, 0); err == nil {
			out.ScreeningResults = results
			out.Pagination["screening_results"] = PaginationMeta{}
			out.Counts["screening_results"] = len(results)
		} else {
			out.PartialFailures = append(out.PartialFailures, "screening_results")
		}
	}
	var scores []domain.ScoreRecord
	if scoreRepo, ok := s.customers.(domain.CustomerScoreCursorRepository); ok {
		scores, err = scoreRepo.ListScoreHistoryCursor(r.Context(), id, pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
	} else {
		scores, err = s.customers.ListScoreHistory(r.Context(), id, pageReq.Limit+1)
	}
	if err != nil {
		out.PartialFailures = append(out.PartialFailures, "score_history")
	} else {
		sort.Slice(scores, func(i, j int) bool {
			if !scores[i].ScoredAt.Equal(scores[j].ScoredAt) {
				return scores[i].ScoredAt.After(scores[j].ScoredAt)
			}
			return scores[i].ID > scores[j].ID
		})
		out.ScoreHistory, out.Pagination["score_history"] = BuildPaginationMeta(scores, pageReq.Limit, func(item domain.ScoreRecord) Cursor { return Cursor{CreatedAt: item.ScoredAt, ID: item.ID} })
		out.Counts["score_history"] = len(out.ScoreHistory)
		if countRepo, ok := s.customers.(domain.CustomerScoreCountRepository); ok {
			if count, countErr := countRepo.CountScoreHistory(r.Context(), id); countErr == nil {
				out.Counts["score_history"] = count
			} else {
				out.PartialFailures = append(out.PartialFailures, "score_history_count")
			}
		}
	}
	out.Timeline, out.Pagination["timeline"] = buildInvestigationTimeline(out, pageReq.Limit)
	if len(out.PartialFailures) > 0 {
		w.Header().Set("Warning", "299 - partial investigation read model")
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleScoreExplanation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	records, err := s.customers.ListScoreHistory(r.Context(), id, 200)
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	scoreID := r.URL.Query().Get("score_id")
	if scoreID == "" {
		scoreID = r.PathValue("scoreID")
	}
	var selected *domain.ScoreRecord
	for i := range records {
		if scoreID == "" || records[i].ID == scoreID {
			selected = &records[i]
			break
		}
	}
	if selected == nil {
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, "score record not found")
		return
	}
	sum := 0.0
	for _, factor := range selected.Factors {
		sum += factor.Contribution
		if factor.Contribution == 0 {
			sum += factor.Score
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"score": selected, "total_reconciled": sum, "rule_set_id": selected.RuleSetID, "rule_set_sha256": selected.RuleSetSHA256, "priority": priorityForTier(selected.Tier), "deterministic": true})
}
func priorityForTier(tier domain.RiskTier) string {
	switch tier {
	case domain.RiskTierHigh:
		return "high"
	case domain.RiskTierMedium:
		return "medium"
	default:
		return "low"
	}
}

func stableDigest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
