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
// another organization's entries (AC10). PurgeOlderThan (Slice 7, PD44)
// hard-deletes org's own EventLog rows whose CreatedAt is strictly before
// cutoff — unconditional by age (a log entry carries no "in-flight" state
// the way an outbox event does), returning how many rows were removed.
type Repository interface {
	Save(ctx context.Context, entry EventLog) error
	Query(ctx context.Context, org organizations.OrgID, filter Filter) ([]EventLog, error)
	PurgeOlderThan(ctx context.Context, org organizations.OrgID, cutoff time.Time) (int, error)
}

// RetentionReader is PurgeOnce's narrow, consumer-defined port onto the
// installation's organizations (Slice 7, PD44), wired in app/ (BOUNDARIES:
// logging already depends on organizations for OrgID, but PurgeOnce's own
// per-org effective window additionally depends on the installation's
// BEECON_RETENTION_DAYS config value — logging itself never imports config,
// so the concrete adapter combining the two lives in the composition root).
// ListOrgIDs enumerates every organization to purge (installation-level,
// like organizations.Repository.ListAll — there is no single "org" to scope
// this by; PurgeOnce iterates every org itself, mirroring
// delivery.WorkQueue/triggers.PollQueue's own installation-level shape for
// the same "a shared background loop, not a per-org one" reason).
// EffectiveLogRetentionDays resolves one org's own governance override
// combined with the installation default; 0 means unlimited/disabled for
// that org — PurgeOnce skips it entirely.
type RetentionReader interface {
	ListOrgIDs(ctx context.Context) ([]organizations.OrgID, error)
	EffectiveLogRetentionDays(ctx context.Context, org organizations.OrgID) (int, error)
}
