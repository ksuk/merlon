package domain

import (
	"context"
	"time"
)

// WhitelistEntryStatus enumerates the whitelist_entries.status lifecycle
// (whitelist.md §1, §7.1): pending_approval -> active -> expired, or
// active -> revoked (revoke also applies directly to pending_approval,
// WL-007).
type WhitelistEntryStatus string

const (
	WhitelistEntryStatusPendingApproval WhitelistEntryStatus = "pending_approval"
	WhitelistEntryStatusActive          WhitelistEntryStatus = "active"
	WhitelistEntryStatusExpired         WhitelistEntryStatus = "expired"
	WhitelistEntryStatusRevoked         WhitelistEntryStatus = "revoked"
)

// WhitelistReviewDecision enumerates whitelist_reviews.decision
// (whitelist.md §7.2).
type WhitelistReviewDecision string

const (
	WhitelistReviewDecisionRenewed WhitelistReviewDecision = "renewed"
	WhitelistReviewDecisionRevoked WhitelistReviewDecision = "revoked"
)

// WhitelistEntry is a whitelist_entries row (whitelist.md §7.1): a customer
// granted conditional exclusion from alert generation, subject to a
// mandatory approval workflow (WL-003) and a mandatory expiry (WL-002).
type WhitelistEntry struct {
	ID              string               `json:"id"`
	CustomerID      string               `json:"customer_id"`
	Status          WhitelistEntryStatus `json:"status"`
	Reason          string               `json:"reason"`
	ExcludedRuleIDs []string             `json:"excluded_rule_ids,omitempty"`
	ValidFrom       time.Time            `json:"valid_from"`
	ValidUntil      time.Time            `json:"valid_until"`
	RequestedBy     string               `json:"requested_by"`
	ApprovedBy      *string              `json:"approved_by,omitempty"`
	ApprovedAt      *time.Time           `json:"approved_at,omitempty"`
	RevokedBy       *string              `json:"revoked_by,omitempty"`
	RevokedAt       *time.Time           `json:"revoked_at,omitempty"`
	Version         int                  `json:"version"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
}

// WhitelistReview is a whitelist_reviews row (whitelist.md §7.2): the record
// of a periodic review of an active whitelist entry (WL-006).
type WhitelistReview struct {
	ID               string                  `json:"id"`
	WhitelistEntryID string                  `json:"whitelist_entry_id"`
	ReviewedBy       string                  `json:"reviewed_by"`
	Decision         WhitelistReviewDecision `json:"decision"`
	ReviewNotes      string                  `json:"review_notes,omitempty"`
	NextReviewDate   *time.Time              `json:"next_review_date,omitempty"`
	CreatedAt        time.Time               `json:"created_at"`
}

// WhitelistRepository manages whitelist entries and their reviews.
type WhitelistRepository interface {
	Get(ctx context.Context, id string) (*WhitelistEntry, error)
	GetActiveByCustomer(ctx context.Context, customerID string) (*WhitelistEntry, error)
	List(ctx context.Context, status WhitelistEntryStatus, limit, offset int) ([]WhitelistEntry, error)
	ListExpiringSoon(ctx context.Context, withinDays int) ([]WhitelistEntry, error)
	Create(ctx context.Context, e *WhitelistEntry) error
	// UpdateWithVersion applies optimistic locking (whitelist.md §3.1): it
	// fails with ErrConflict if e.Version no longer matches expectedVersion
	// (i.e. the row changed since it was read).
	UpdateWithVersion(ctx context.Context, e *WhitelistEntry, expectedVersion int) error
	CreateReview(ctx context.Context, r *WhitelistReview) error
	ListReviews(ctx context.Context, entryID string) ([]WhitelistReview, error)
}
