package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

type CreateCustomerRequest struct {
	ExternalID   string            `json:"external_id"`
	CustomerType domain.CustomerType `json:"customer_type"`
	CountryCode  string            `json:"country_code"`
	ProductTypes []string          `json:"product_types"`
	Attributes   map[string]string `json:"attributes"`
}

type UpdateCustomerRequest struct {
	CountryCode  *string           `json:"country_code,omitempty"`
	ProductTypes *[]string         `json:"product_types,omitempty"`
	Attributes   map[string]string `json:"attributes,omitempty"`
}

func (s *Server) handleListCustomers(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	customers, err := s.customers.List(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if customers == nil {
		customers = []domain.Customer{}
	}

	writeJSON(w, http.StatusOK, customers)
}

func (s *Server) handleGetCustomer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.customers.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleCreateCustomer(w http.ResponseWriter, r *http.Request) {
	var req CreateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.ExternalID == "" {
		writeError(w, http.StatusBadRequest, "external_id required")
		return
	}
	if req.CustomerType == "" {
		writeError(w, http.StatusBadRequest, "customer_type required")
		return
	}

	now := time.Now()
	c := &domain.Customer{
		ID:           generateID(),
		ExternalID:   req.ExternalID,
		CustomerType: req.CustomerType,
		CountryCode:  req.CountryCode,
		ProductTypes: req.ProductTypes,
		Attributes:   req.Attributes,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.customers.Create(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req UpdateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.CountryCode != nil {
		c.CountryCode = *req.CountryCode
	}
	if req.ProductTypes != nil {
		c.ProductTypes = *req.ProductTypes
	}
	if req.Attributes != nil {
		c.Attributes = req.Attributes
	}

	if err := s.customers.Update(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusServiceUnavailable, "scoring engine not configured")
		return
	}

	id := r.PathValue("id")
	c, err := s.customers.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req ScoreCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	record, err := s.scoring.ScoreCustomer(r.Context(), c, req.RuleSetID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "scoring engine error: "+err.Error())
		return
	}

	record.ID = generateID()

	// Update customer risk score
	c.RiskScore = &record.Score
	c.RiskTier = &record.Tier
	now := record.ScoredAt
	c.LastScoredAt = &now

	if err := s.customers.Update(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.customers.SaveScoreRecord(r.Context(), record); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, record)
}

type ScreenCustomerRequest struct {
	ListIDs []string `json:"list_ids"`
}

func (s *Server) handleScreenCustomer(w http.ResponseWriter, r *http.Request) {
	if s.screening == nil {
		writeError(w, http.StatusServiceUnavailable, "screening engine not configured")
		return
	}

	id := r.PathValue("id")
	c, err := s.customers.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req ScreenCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	result, err := s.screening.ScreenCustomer(r.Context(), c, req.ListIDs)
	if err != nil {
		writeError(w, http.StatusBadGateway, "screening engine error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}
