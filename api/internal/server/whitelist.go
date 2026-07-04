package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

// defaultWhitelistMaxValidDays is the default maximum validity period (WL-002,
// whitelist.md §要件表: "最大有効期間はシステム設定で制御可能（デフォルト：1年）").
// TODO(WS-6 Task 6): make this configurable via system settings; for now it is
// the fixed default.
const defaultWhitelistMaxValidDays = 365

type createWhitelistEntryRequest struct {
	CustomerID      string   `json:"customer_id"`
	Reason          string   `json:"reason"`
	ValidUntil      string   `json:"valid_until"`
	ExcludedRuleIDs []string `json:"excluded_rule_ids,omitempty"`
}

// handleCreateWhitelistEntry is the whitelist request step (WL-001/WL-002,
// whitelist.md §1): it validates reason/valid_until and creates the entry in
// status=pending_approval. requested_by is resolved from the authenticated
// principal so segregation of duties (WL-003) can be enforced at approval
// time.
func (s *Server) handleCreateWhitelistEntry(w http.ResponseWriter, r *http.Request) {
	if s.whitelist == nil {
		writeError(w, http.StatusServiceUnavailable, "whitelist management not configured")
		return
	}

	var req createWhitelistEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.CustomerID == "" {
		writeError(w, http.StatusBadRequest, "customer_id is required")
		return
	}
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	if req.ValidUntil == "" {
		writeError(w, http.StatusBadRequest, "valid_until is required")
		return
	}

	validUntil, err := time.Parse(time.RFC3339, req.ValidUntil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "valid_until must be an RFC3339 timestamp")
		return
	}

	now := time.Now()
	if !validUntil.After(now) {
		writeError(w, http.StatusBadRequest, "valid_until must be in the future")
		return
	}
	maxValidUntil := now.Add(defaultWhitelistMaxValidDays * 24 * time.Hour)
	if validUntil.After(maxValidUntil) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("valid_until must be within %d days", defaultWhitelistMaxValidDays))
		return
	}

	if _, err := s.customers.Get(r.Context(), req.CustomerID); err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, "customer not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	entry := &domain.WhitelistEntry{
		ID:              generateID(),
		CustomerID:      req.CustomerID,
		Status:          domain.WhitelistEntryStatusPendingApproval,
		Reason:          req.Reason,
		ExcludedRuleIDs: req.ExcludedRuleIDs,
		ValidFrom:       now,
		ValidUntil:      validUntil,
		RequestedBy:     resolveAuditUserID(r),
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.whitelist.Create(r.Context(), entry); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, entry)
}

// handleApproveWhitelistEntry is the approval step (WL-003, whitelist.md §1):
// it enforces segregation of duties (requester != approver) and optimistic
// locking, then transitions the entry to status=active.
func (s *Server) handleApproveWhitelistEntry(w http.ResponseWriter, r *http.Request) {
	if s.whitelist == nil {
		writeError(w, http.StatusServiceUnavailable, "whitelist management not configured")
		return
	}

	id := r.PathValue("id")
	entry, err := s.whitelist.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if entry.Status != domain.WhitelistEntryStatusPendingApproval {
		writeError(w, http.StatusConflict, "whitelist entry is not pending approval")
		return
	}

	approver := resolveAuditUserID(r)
	if approver == entry.RequestedBy {
		writeError(w, http.StatusForbidden, "requester cannot approve their own whitelist entry")
		return
	}

	expectedVersion := entry.Version
	now := time.Now()
	entry.Status = domain.WhitelistEntryStatusActive
	entry.ApprovedBy = &approver
	entry.ApprovedAt = &now

	if err := s.whitelist.UpdateWithVersion(r.Context(), entry, expectedVersion); err != nil {
		writeWhitelistUpdateError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

// handleRevokeWhitelistEntry is the immediate-revoke step (WL-007,
// whitelist.md §5): unlike approval it requires no counter-signature since it
// only tightens detection, so any principal holding whitelist:request may
// invoke it, including to withdraw their own still-pending request.
func (s *Server) handleRevokeWhitelistEntry(w http.ResponseWriter, r *http.Request) {
	if s.whitelist == nil {
		writeError(w, http.StatusServiceUnavailable, "whitelist management not configured")
		return
	}

	id := r.PathValue("id")
	entry, err := s.whitelist.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if entry.Status != domain.WhitelistEntryStatusPendingApproval && entry.Status != domain.WhitelistEntryStatusActive {
		writeError(w, http.StatusConflict, "whitelist entry cannot be revoked from status "+string(entry.Status))
		return
	}

	revoker := resolveAuditUserID(r)
	expectedVersion := entry.Version
	now := time.Now()
	entry.Status = domain.WhitelistEntryStatusRevoked
	entry.RevokedBy = &revoker
	entry.RevokedAt = &now

	if err := s.whitelist.UpdateWithVersion(r.Context(), entry, expectedVersion); err != nil {
		writeWhitelistUpdateError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

// writeWhitelistUpdateError translates UpdateWithVersion's error classes
// (optimistic lock version mismatch or the active-entry partial unique index,
// both whitelist.md §3.1) to their HTTP equivalents.
func writeWhitelistUpdateError(w http.ResponseWriter, err error) {
	var conflict *domain.ErrConflict
	if errors.As(err, &conflict) {
		writeError(w, http.StatusConflict, conflict.Error())
		return
	}
	var nf *domain.ErrNotFound
	if errors.As(err, &nf) {
		writeError(w, http.StatusNotFound, nf.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func (s *Server) handleGetWhitelistEntry(w http.ResponseWriter, r *http.Request) {
	if s.whitelist == nil {
		writeError(w, http.StatusServiceUnavailable, "whitelist management not configured")
		return
	}

	id := r.PathValue("id")
	entry, err := s.whitelist.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

func whitelistEntryCursor(e domain.WhitelistEntry) Cursor {
	return Cursor{CreatedAt: e.CreatedAt, ID: e.ID}
}

func (s *Server) handleListWhitelistEntries(w http.ResponseWriter, r *http.Request) {
	if s.whitelist == nil {
		writeError(w, http.StatusServiceUnavailable, "whitelist management not configured")
		return
	}

	status := domain.WhitelistEntryStatus(r.URL.Query().Get("status"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	entries, err := s.whitelist.List(r.Context(), status, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	page, meta := BuildPaginationMeta(entries, limit, whitelistEntryCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}
