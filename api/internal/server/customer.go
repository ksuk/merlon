package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ksuk/merlon/api/internal/apierr"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/events"
	"github.com/ksuk/merlon/api/internal/events/handlers"
	"github.com/ksuk/merlon/api/internal/screening"
)

const (
	maxAttributes   = 50
	maxAttrKeyLen   = 256
	maxAttrValueLen = 10000
)

type CreateCustomerRequest struct {
	ExternalID   string              `json:"external_id"`
	CustomerType domain.CustomerType `json:"customer_type"`
	CountryCode  string              `json:"country_code"`
	ProductTypes []string            `json:"product_types"`
	Attributes   map[string]any      `json:"attributes"`
}

type UpdateCustomerRequest struct {
	CountryCode  *string        `json:"country_code,omitempty"`
	ProductTypes *[]string      `json:"product_types,omitempty"`
	Attributes   map[string]any `json:"attributes,omitempty"`
}

func customerCursor(c domain.Customer) Cursor {
	return Cursor{CreatedAt: c.CreatedAt, ID: c.ID}
}

func (s *Server) handleListCustomers(w http.ResponseWriter, r *http.Request) {
	if useCursorPagination(r) {
		s.handleListCustomersCursor(w, r)
		return
	}
	s.handleListCustomersOffset(w, r)
}

// handleListCustomersCursor serves the HTTP API contract §1.1 cursor-based pagination.
func (s *Server) handleListCustomersCursor(w http.ResponseWriter, r *http.Request) {
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	search := strings.TrimSpace(r.URL.Query().Get("search"))
	var customers []domain.Customer
	if search == "" {
		customers, err = s.customers.ListByCursor(r.Context(), pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
	} else {
		searchRepo, ok := s.customers.(domain.CustomerSearchRepository)
		if !ok {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "customer search is not configured")
			return
		}
		customers, err = searchRepo.ListByCursorSearch(r.Context(), pageReq.Limit+1, toDomainCursor(pageReq.Cursor), search)
	}
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	page, meta := BuildPaginationMeta(customers, pageReq.Limit, customerCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

// handleListCustomersOffset preserves the pre-existing offset/limit contract
// (the HTTP API contract §1.2 dual-support / deprecation period) while still returning the
// additive {"data", "pagination"} envelope.
func (s *Server) handleListCustomersOffset(w http.ResponseWriter, r *http.Request) {
	offsetParam := r.URL.Query().Get("offset")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(offsetParam)
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	search := strings.TrimSpace(r.URL.Query().Get("search"))
	var customers []domain.Customer
	var err error
	if search == "" {
		customers, err = s.customers.List(r.Context(), limit+1, offset)
	} else {
		searchRepo, ok := s.customers.(domain.CustomerSearchRepository)
		if !ok {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "customer search is not configured")
			return
		}
		customers, err = searchRepo.ListSearch(r.Context(), search, limit+1, offset)
	}
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	if offsetParam != "" {
		setOffsetDeprecationHeaders(w)
	}

	page, meta := BuildPaginationMeta(customers, limit, customerCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

func (s *Server) handleGetCustomer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.customers.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleCreateCustomer(w http.ResponseWriter, r *http.Request) {
	var req CreateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}

	if req.ExternalID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "external_id required")
		return
	}
	if !isValidCustomerType(req.CustomerType) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer_type must be one of: individual, corporate_domestic, corporate_foreign")
		return
	}
	if err := validateAttributes(req.Attributes); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	now := time.Now()
	c := &domain.Customer{
		ID:           generateID(),
		ExternalID:   req.ExternalID,
		CustomerType: req.CustomerType,
		CountryCode:  req.CountryCode,
		ProductTypes: req.ProductTypes,
		Status:       domain.CustomerStatusActive,
		Attributes:   req.Attributes,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Customers == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if err := repos.Customers.Create(r.Context(), c); err != nil {
			return err
		}
		return appendRequiredMutationAudit(r.Context(), r, repos, "create", "customers", c.ID, map[string]string{
			"external_id": c.ExternalID,
		}, c.CreatedAt)
	}); err != nil {
		writeAtomicMutationError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateCustomer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.customers.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	var req UpdateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}

	if req.CountryCode != nil {
		c.CountryCode = *req.CountryCode
	}
	if req.ProductTypes != nil {
		c.ProductTypes = *req.ProductTypes
	}
	if req.Attributes != nil {
		if err := validateAttributes(req.Attributes); err != nil {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
			return
		}
		c.Attributes = req.Attributes
	}

	before := *c
	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Customers == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if err := repos.Customers.Update(r.Context(), c); err != nil {
			return err
		}
		return appendRequiredMutationAudit(r.Context(), r, repos, "update", "customers", c.ID, map[string]string{
			"before_country_code": before.CountryCode, "after_country_code": c.CountryCode,
			"before_external_id": before.ExternalID, "after_external_id": c.ExternalID,
		}, c.UpdatedAt)
	}); err != nil {
		writeAtomicMutationError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleGetScoreHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 10
	}

	records, err := s.customers.ListScoreHistory(r.Context(), id, limit)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if records == nil {
		records = []domain.ScoreRecord{}
	}

	writeJSON(w, http.StatusOK, records)
}

type ScoreCustomerRequest struct {
	RuleSetID string `json:"rule_set_id"`
}

func (s *Server) handleScoreCustomer(w http.ResponseWriter, r *http.Request) {
	if s.scoring == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "scoring engine not configured")
		return
	}

	id := r.PathValue("id")
	c, err := s.customers.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	var req ScoreCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}

	record, err := s.scoring.ScoreCustomer(r.Context(), c, req.RuleSetID)
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, apierr.CodeEngineError, "scoring engine error: "+err.Error())
		return
	}

	record.ID = generateID()

	oldTier := c.RiskTier

	// Update customer risk score
	c.RiskScore = &record.Score
	c.RiskTier = &record.Tier
	now := record.ScoredAt
	c.LastScoredAt = &now

	// EDD escalation window (the case-management workflow §EDD未実施継続時の段階的
	// 措置): entering High tier starts the clock (kept if already running,
	// so a re-score at High doesn't reset stage progress); leaving High tier
	// closes the window, since EDD is no longer required.
	if record.Tier == domain.RiskTierHigh {
		if c.EddRequestedAt == nil {
			eddAt := record.ScoredAt
			c.EddRequestedAt = &eddAt
		}
	} else {
		c.EddRequestedAt = nil
		c.EddStage1LastSentAt = nil
		c.EddStage2NotifiedAt = nil
		c.EddStage3NotifiedAt = nil
	}

	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Customers == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if err := repos.Customers.Update(r.Context(), c); err != nil {
			return err
		}
		if err := repos.Customers.SaveScoreRecord(r.Context(), record); err != nil {
			return err
		}
		if err := appendRequiredMutationAudit(r.Context(), r, repos, "score_customer", "customers", c.ID, map[string]string{
			"rule_set_id": record.RuleSetID,
			"score":       strconv.FormatFloat(record.Score, 'f', -1, 64),
			"tier":        string(record.Tier),
		}, record.ScoredAt); err != nil {
			return err
		}
		if s.eventOutbox != nil {
			return s.enqueueTierChange(r.Context(), repos, c.ID, oldTier, record.Tier, record.ScoredAt)
		}
		return nil
	}); err != nil {
		writeAtomicMutationError(w, err)
		return
	}

	// Immediate sanctions rescreen at the new tier's frequency
	// (the screening workflow "CDD ティア昇格時（Medium → High 等、新ティアの頻度を即時適用）").
	if isTierPromotion(oldTier, record.Tier) && s.screening != nil {
		deps := screening.SchedulerDeps{
			Customers:        s.customers,
			Screening:        s.screening,
			Results:          s.screeningResults,
			TargetCustomerID: c.ID,
		}
		if _, err := screening.RunRescreeningBatch(r.Context(), deps, screening.TriggerTierPromoted); err != nil {
			slog.Error("tier-promotion immediate rescreen failed", "customer_id", c.ID, "error", err)
		}
	}

	// Publish a tier-change event (Task 8, CDD-009) so
	// events/handlers.TierChangeHandler can trigger the transaction-monitoring design's
	// 24h retroactive TM re-evaluation on upgrades. Independent of the
	// screening rescreen above (different downstream consumer).
	if s.eventOutbox == nil {
		if err := s.publishTierChange(r.Context(), c.ID, oldTier, record.Tier, record.ScoredAt); err != nil {
			slog.ErrorContext(r.Context(), "tier-change event publish failed", "customer_id", c.ID, "error", err)
		}
	}

	writeJSON(w, http.StatusOK, record)
}

// isTierPromotion reports whether newTier is a promotion (e.g. Medium ->
// High) relative to oldTier. A first-ever score (oldTier == nil) is not a
// promotion.
func isTierPromotion(oldTier *domain.RiskTier, newTier domain.RiskTier) bool {
	if oldTier == nil {
		return false
	}
	return tierRank(newTier) > tierRank(*oldTier)
}

func tierRank(t domain.RiskTier) int {
	switch t {
	case domain.RiskTierLow:
		return 0
	case domain.RiskTierMedium:
		return 1
	case domain.RiskTierHigh:
		return 2
	default:
		return -1
	}
}

// publishTierChange emits a "cdd.tier_changed" event (Task 8, CDD-009) when
// scoring changed the customer's risk tier, so
// events/handlers.TierChangeHandler can trigger the transaction-monitoring design's
// retroactive re-evaluation on upgrades. It is a no-op if no event bus is
// configured or the tier did not change.
func (s *Server) publishTierChange(ctx context.Context, customerID string, oldTier *domain.RiskTier, newTier domain.RiskTier, scoredAt time.Time) error {
	if s.events == nil {
		return nil
	}
	event, err := s.newTierChangeEvent(customerID, oldTier, newTier, scoredAt)
	if err != nil || event.ID == "" {
		return err
	}
	return s.events.Publish(ctx, event)
}

func (s *Server) newTierChangeEvent(customerID string, oldTier *domain.RiskTier, newTier domain.RiskTier, scoredAt time.Time) (events.Event, error) {
	if s.events == nil || (oldTier != nil && *oldTier == newTier) {
		return events.Event{}, nil
	}

	tc := handlers.TierChangeEvent{
		CustomerID: customerID,
		OldTier:    oldTier,
		NewTier:    newTier,
		ChainID:    generateID(),
		ScoredAt:   scoredAt,
	}
	payload, err := json.Marshal(tc)
	if err != nil {
		return events.Event{}, err
	}
	return events.Event{
		ID:        generateID(),
		Topic:     "cdd.tier_changed",
		Payload:   payload,
		ChainID:   tc.ChainID,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (s *Server) enqueueTierChange(ctx context.Context, repos domain.AtomicMutationRepositories, customerID string, oldTier *domain.RiskTier, newTier domain.RiskTier, scoredAt time.Time) error {
	if s.eventOutbox == nil || s.events == nil || (oldTier != nil && *oldTier == newTier) {
		return nil
	}
	if repos.EventOutbox == nil {
		return errAtomicMutationUnavailable
	}
	event, err := s.newTierChangeEvent(customerID, oldTier, newTier, scoredAt)
	if err != nil {
		return err
	}
	return repos.EventOutbox.Enqueue(ctx, &domain.DurableEvent{
		ID:            event.ID,
		Topic:         event.Topic,
		Payload:       event.Payload,
		ChainID:       event.ChainID,
		ChainHopCount: event.ChainHopCount,
		CreatedAt:     event.CreatedAt,
	})
}

type ScreenCustomerRequest struct {
	ListIDs []string `json:"list_ids"`
}

func (s *Server) handleScreenCustomer(w http.ResponseWriter, r *http.Request) {
	if s.screening == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "screening engine not configured")
		return
	}

	id := r.PathValue("id")
	c, err := s.customers.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	var req ScreenCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}

	result, err := s.screening.ScreenCustomer(r.Context(), c, req.ListIDs)
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, apierr.CodeEngineError, "screening engine error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func isValidCustomerType(ct domain.CustomerType) bool {
	switch ct {
	case domain.CustomerTypeIndividual, domain.CustomerTypeCorporateDomestic, domain.CustomerTypeCorporateForeign,
		domain.CustomerTypeTrust, domain.CustomerTypePartnership, domain.CustomerTypeNPO,
		domain.CustomerTypeGovernment, domain.CustomerTypeForeignLegalArrangement:
		return true
	default:
		return false
	}
}

func validateAttributes(attrs map[string]any) error {
	if len(attrs) > maxAttributes {
		return fmt.Errorf("too many attributes: %d (max %d)", len(attrs), maxAttributes)
	}
	for k, v := range attrs {
		if len(k) > maxAttrKeyLen {
			return fmt.Errorf("attribute key too long: %d chars (max %d)", len(k), maxAttrKeyLen)
		}
		// Scalar string values are checked directly; structured values
		// (e.g. attributes.trust_parties, the data model §1.1.1) are checked
		// by their serialized JSON size instead.
		size := 0
		if s, ok := v.(string); ok {
			size = len(s)
		} else if encoded, err := json.Marshal(v); err == nil {
			size = len(encoded)
		}
		if size > maxAttrValueLen {
			return fmt.Errorf("attribute value too long for key %q: %d chars (max %d)", k, size, maxAttrValueLen)
		}
	}
	return nil
}
