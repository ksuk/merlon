package server

import (
	"encoding/json"
	"errors"
	"github.com/ksuk/merlon/api/internal/apierr"
	"net/http"

	"github.com/ksuk/merlon/api/internal/domain"
)

type CreateAccountRequest struct {
	ExternalID  string             `json:"external_id"`
	AccountType domain.AccountType `json:"account_type"`
}

func isValidAccountType(t domain.AccountType) bool {
	switch t {
	case domain.AccountTypeIndividual, domain.AccountTypeJoint:
		return true
	default:
		return false
	}
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req CreateAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if req.ExternalID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "external_id required")
		return
	}
	if !isValidAccountType(req.AccountType) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "account_type must be one of: individual, joint")
		return
	}

	a := &domain.Account{
		ID:          generateID(),
		ExternalID:  req.ExternalID,
		AccountType: req.AccountType,
	}
	if err := s.accounts.Create(r.Context(), a); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.accounts.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a)
}

type AddAccountCustomerRequest struct {
	CustomerID string             `json:"customer_id"`
	Role       domain.AccountRole `json:"role"`
}

func isValidAccountRole(role domain.AccountRole) bool {
	switch role {
	case domain.AccountRolePrimary, domain.AccountRoleCoHolder:
		return true
	default:
		return false
	}
}

func (s *Server) handleAddAccountCustomer(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	if _, err := s.accounts.Get(r.Context(), accountID); err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	var req AddAccountCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if req.CustomerID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer_id required")
		return
	}
	if !isValidAccountRole(req.Role) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "role must be one of: primary, co_holder")
		return
	}

	if _, err := s.customers.Get(r.Context(), req.CustomerID); err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer not found: "+req.CustomerID)
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	if err := s.accounts.AddCustomer(r.Context(), accountID, req.CustomerID, req.Role); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

// handleListAccountCustomers returns every customer linked to the account
// individually (the data model §1.1.3: each holder of a joint account is
// screened separately, not just the account's representative score/holder).
func (s *Server) handleListAccountCustomers(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	if _, err := s.accounts.Get(r.Context(), accountID); err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	links, err := s.accounts.ListCustomers(r.Context(), accountID)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if links == nil {
		links = []domain.AccountCustomer{}
	}
	writeJSON(w, http.StatusOK, links)
}
