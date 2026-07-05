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

// AuditListFilter narrows AuditRepository.List (ALD-001: 期間、操作カテゴ
// リ、操作者、対象リソースでのフィルタリング). Zero-value fields impose no
// restriction on that axis. Cursor/Limit follow the same keyset-pagination
// convention as the other List*Cursor repository methods (domain.Cursor);
// callers fetch Limit+1 rows and trim the lookahead row themselves
// (server.BuildPaginationMeta), so List does not return a next-cursor.
type AuditListFilter struct {
	ResourceType string
	ResourceID   string
	UserID       string
	// ActionCategory groups entries by ResourceTypesForCategory below
	// (audit.md §1 操作カテゴリ), rather than filtering on Action directly.
	ActionCategory string
	Since          *time.Time
	Until          *time.Time
	Cursor         *Cursor
	Limit          int
}

type AuditRepository interface {
	Create(ctx context.Context, entry *AuditEntry) error
	List(ctx context.Context, filter AuditListFilter) ([]AuditEntry, error)
}

// actionCategoryResourceTypes groups the resource_type values recorded by
// auditMiddleware's resolveResource into the 操作カテゴリ audit.md §1
// enumerates, for ALD-001's category filter. resource_type values not
// listed here have no category (ActionCategory filtering excludes them).
var actionCategoryResourceTypes = map[string][]string{
	"認証":       {"auth", "session"},
	"顧客データ":    {"customers"},
	"ルール管理":    {"rules", "rule_definitions"},
	"アラート・ケース": {"alerts", "cases"},
	"STR":      {"reports"},
	"ホワイトリスト":  {"whitelist"},
	"管理操作":     {"admin", "apikeys", "users", "retention_policy", "webhooks", "webhook_dlq"},
}

// ResourceTypesForCategory returns the resource_type values belonging to
// category (audit.md §1), or nil for an empty/unrecognized category (no
// category filter applied).
func ResourceTypesForCategory(category string) []string {
	return actionCategoryResourceTypes[category]
}
