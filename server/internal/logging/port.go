package logging

import (
	"context"
	"time"

	"beecon/internal/organizations"
)

// Cursor is the decoded form of the opaque pagination cursor Query accepts
// and returns (PD10): the created_at/id pair of the last entry on the
// previous page, so the next page resumes strictly after it in the
// newest-first ordering.
type Cursor struct {
	CreatedAt time.Time
	ID        LogID
}

// Filter is the org-scoped driven port's query shape: every optional filter
// AC10 lists (connectionId, userId, toolSlug, a from/to time range), the
// decoded pagination cursor, and the page size to fetch.
type Filter struct {
	ConnectionID string
	UserID       string
	ToolSlug     string
	From         *time.Time
	To           *time.Time
	Cursor       *Cursor
	Limit        int
}

// Repository is the logging module's org-scoped driven port. Query returns
// entries matching filter, newest first, scoped to org — a caller never sees
// another organization's entries (AC10).
type Repository interface {
	Save(ctx context.Context, entry EventLog) error
	Query(ctx context.Context, org organizations.OrgID, filter Filter) ([]EventLog, error)
}
