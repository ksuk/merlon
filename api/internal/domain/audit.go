package domain

import (
	"context"
	"time"
)

type AuditEntry struct {
	ID           int64             `json:"id"`
	UserID       string            `json:"user_id"`
	Action       string            `json:"action"`
	ResourceType string            `json:"resource_type"`
	ResourceID   string            `json:"resource_id"`
	Details      map[string]string `json:"details,omitempty"`
	IPAddress    string            `json:"ip_address,omitempty"`
	UserAgent    string            `json:"user_agent,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

type AuditRepository interface {
	Create(ctx context.Context, entry *AuditEntry) error
	List(ctx context.Context, resourceType, resourceID string, limit int) ([]AuditEntry, error)
}
