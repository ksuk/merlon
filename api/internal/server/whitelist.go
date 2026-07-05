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

// defaultWhitelistMaxValidDays is the fallback maximum validity period
// (WL-002, whitelist.md §要件表: "最大有効期間はシステム設定で制御可能（デフォルト：1年）")
// used when Deps.WhitelistMaxValidDays is unset. TODO(WS-2): once the rule
// management API supports system-level settings, source this from there
// instead of the MERLON_WHITELIST_MAX_VALID_DAYS env var (config.go).
const defaultWhitelistMaxValidDays = 365

// whitelistMaxValidDays resolves the configured maximum validity period,
// falling back to defaultWhitelistMaxValidDays when unset.
func (s *Server) whitelistMaxValidDays() int {
	if s.whitelistMaxValidDaysCfg > 0 {
		return s.whitelistMaxValidDaysCfg
	}
	return defaultWhitelistMaxValidDays
}

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
	maxValidDays := s.whitelistMaxValidDays()
	maxValidUntil := now.Add(time.Duration(maxValidDays) * 24 * time.Hour)
	if validUntil.After(maxValidUntil) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("valid_until must be within %d days", maxValidDays))
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

type createWhitelistReviewRequest struct {
	Decision       domain.WhitelistReviewDecision `json:"decision"`
	ReviewNotes    string                         `json:"review_notes,omitempty"`
	NextReviewDate string                         `json:"next_review_date,omitempty"`
	// NewValidUntil is required when decision=renewed (whitelist.md §7.2
	// "有効期限を延長"); the spec does not pin a fixed extension length, so
	// the reviewer supplies the new expiry explicitly, validated against the
	// same max-period-from-now rule as initial registration (WL-002).
	NewValidUntil string `json:"new_valid_until,omitempty"`
}

// handleCreateWhitelistReview is the periodic review step (WL-006,
// whitelist.md §7.2): decision=renewed extends valid_until and records
// next_review_date; decision=revoked lapses ("失効させる") the entry to
// status=expired. Both the review row and the entry update are written
// atomically via CreateReviewAndApply.
func (s *Server) handleCreateWhitelistReview(w http.ResponseWriter, r *http.Request) {
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

	if entry.Status != domain.WhitelistEntryStatusActive {
		writeError(w, http.StatusConflict, "whitelist entry is not active")
		return
	}

	var req createWhitelistReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	now := time.Now()
	review := &domain.WhitelistReview{
		ID:               generateID(),
		WhitelistEntryID: entry.ID,
		ReviewedBy:       resolveAuditUserID(r),
		Decision:         req.Decision,
		ReviewNotes:      req.ReviewNotes,
		CreatedAt:        now,
	}

	switch req.Decision {
	case domain.WhitelistReviewDecisionRenewed:
		if req.NewValidUntil == "" {
			writeError(w, http.StatusBadRequest, "new_valid_until is required when decision is renewed")
			return
		}
		newValidUntil, err := time.Parse(time.RFC3339, req.NewValidUntil)
		if err != nil {
			writeError(w, http.StatusBadRequest, "new_valid_until must be an RFC3339 timestamp")
			return
		}
		if !newValidUntil.After(now) {
			writeError(w, http.StatusBadRequest, "new_valid_until must be in the future")
			return
		}
		maxValidDays := s.whitelistMaxValidDays()
		if newValidUntil.After(now.Add(time.Duration(maxValidDays) * 24 * time.Hour)) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("new_valid_until must be within %d days", maxValidDays))
			return
		}
		if req.NextReviewDate != "" {
			nextReviewDate, err := time.Parse(time.DateOnly, req.NextReviewDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "next_review_date must be a YYYY-MM-DD date")
				return
			}
			review.NextReviewDate = &nextReviewDate
		}
		entry.ValidUntil = newValidUntil
	case domain.WhitelistReviewDecisionRevoked:
		entry.Status = domain.WhitelistEntryStatusExpired
	default:
		writeError(w, http.StatusBadRequest, `decision must be "renewed" or "revoked"`)
		return
	}

	expectedVersion := entry.Version
	if err := s.whitelist.CreateReviewAndApply(r.Context(), review, entry, expectedVersion); err != nil {
		writeWhitelistUpdateError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, review)
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
