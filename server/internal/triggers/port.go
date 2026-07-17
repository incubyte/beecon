package triggers

import (
	"context"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/organizations"
)

// ListCursor is the decoded pagination cursor ListPage's driven port
// accepts: the created_at/id pair of the last instance on the previous
// page, so the next page resumes strictly after it in the newest-first
// ordering (mirrors connections.ListCursor).
type ListCursor struct {
	CreatedAt time.Time
	ID        TriggerInstanceID
}

// ListFilter is ListPage's org-scoped driven port query shape: ConnectionID
// and UserID each optionally narrow the page (empty means unrestricted),
// plus the decoded pagination cursor and the page size to fetch.
type ListFilter struct {
	ConnectionID connections.ConnectionID
	UserID       organizations.UserID
	Cursor       *ListCursor
	Limit        int
}

// Repository is the triggers module's org-scoped driven port. Every method
// takes the owning OrgID as its second parameter, so a query without org
// scope cannot be expressed. Save both inserts a freshly created instance
// and persists its later status transitions (Disable/Enable) — there is no
// separate Update, mirroring how a TriggerInstance's only mutable field is
// its own Status. FindByID returns (nil, nil) on a miss (including an
// instance belonging to a different organization); the facade translates
// that into ErrNotFound. ListPage returns instances scoped to org, matching
// filter, newest first. Delete permanently removes one instance.
// DeleteByConnection removes every instance bound to connID, scoped to org —
// the connection-delete cascade (PD33); deleting zero instances is not an
// error.
type Repository interface {
	Save(ctx context.Context, instance TriggerInstance) error
	FindByID(ctx context.Context, org organizations.OrgID, id TriggerInstanceID) (*TriggerInstance, error)
	ListPage(ctx context.Context, org organizations.OrgID, filter ListFilter) ([]TriggerInstance, error)
	Delete(ctx context.Context, org organizations.OrgID, id TriggerInstanceID) error
	DeleteByConnection(ctx context.Context, org organizations.OrgID, connID connections.ConnectionID) error
}

// DefinitionReader is a narrow, consumer-defined port satisfied by
// *catalog.Facade: Create needs the trigger definition's config schema to
// validate the instance's config against (PD33). An unknown slug surfaces
// as catalog's own not-found error (ErrTriggerDefinitionNotFound) — this is
// the "unknown trigger slug -> not-found" rule.
type DefinitionReader interface {
	TriggerDefinitionDetail(ctx context.Context, slug string) (catalog.TriggerDefinitionSummary, error)
}

// ConnectionReader is a narrow, consumer-defined port satisfied by
// *connections.Facade: Create needs the connection's status (PD33: it must
// be ACTIVE) and owning userId, org-scoped. PollOnce (Slice 4) reuses the
// same port to check whether an instance's connection has left or rejoined
// ACTIVE (PD33/PD34's pause/resume). An unknown or cross-org id surfaces as
// connections' own not-found error.
type ConnectionReader interface {
	Get(ctx context.Context, org organizations.OrgID, id connections.ConnectionID) (connections.Connection, error)
}

// PollQueue is deliberately installation-level, not org-scoped (mirrors
// delivery.WorkQueue, same rationale — see test/arch/orgscope_test.go's
// whitelist): PollOnce claims due TriggerInstances across every
// organization by design — the poller is one shared background loop
// (PD29), not a per-org one — but every claimed TriggerInstance still
// carries its own OrgID.
type PollQueue interface {
	// ClaimDuePolls leases up to limit ACTIVE TriggerInstances whose
	// NextPollAt is due as of now (section 3 of the architecture doc:
	// Postgres FOR UPDATE SKIP LOCKED, SQLite the same predicate without
	// it), oldest-created first, setting each claimed row's lease until
	// now+leaseTTL so a second binary instance never claims the same row.
	// A DISABLED instance is never claimed — PD33's "disable stops
	// firing" applies to polling too.
	ClaimDuePolls(ctx context.Context, now time.Time, leaseTTL time.Duration, limit int) ([]TriggerInstance, error)
}

// TriggerSlugIndex is deliberately installation-level, not org-scoped
// (mirrors PollQueue's own rationale — see test/arch/orgscope_test.go's
// whitelist): PauseInstancesForRemovedTrigger (Phase 5 registry sub-phase,
// Slice 4, PD66) touches every organization's instances bound to one
// trigger slug when a catalog activation removes that trigger definition —
// a genuinely cross-org bulk operation a single org's Repository call could
// not express, the same shape as ClaimDuePolls' own cross-org poller scan.
type TriggerSlugIndex interface {
	ListByTriggerSlug(ctx context.Context, triggerSlug string) ([]TriggerInstance, error)
}

// PollRecord is one provider record RecordSource returns for one poll tick
// — triggers' own copy of execution.Record's shape (BOUNDARIES: triggers
// does not depend on execution; RecordSource plus an app/wiring.go adapter
// is the seam, architecture section "Placement answers").
type PollRecord struct {
	ID        string
	Timestamp time.Time
	Payload   map[string]any
}

// PollRecordQuery is what RecordSource.FetchRecords needs for one
// instance's current tick — triggers' own copy of execution.PollQuery's
// shape. Watermark's zero value means "no watermark yet" (the instance's
// baseline poll, PD34).
type PollRecordQuery struct {
	OrgID        organizations.OrgID
	UserID       organizations.UserID
	ConnectionID connections.ConnectionID
	TriggerSlug  string
	Config       map[string]any
	Watermark    time.Time
}

// RecordSource is a narrow, consumer-defined port for fetching one poll
// tick's provider records (Slice 4): wired in app/wiring.go
// (executionRecordSource) to execution.Facade.FetchTriggerRecords —
// triggers itself never imports execution (BOUNDARIES: the dependency runs
// the other way; execution has no dependency on triggers either — this is
// the consumer-defined-port seam the architecture doc calls out).
type RecordSource interface {
	FetchRecords(ctx context.Context, query PollRecordQuery) ([]PollRecord, error)
}

// EventSink is a narrow, consumer-defined port for emitting one fired
// trigger.event (PD32) through the outbox: wired to delivery.Facade.Enqueue
// in app/wiring.go (triggersEventSink) — triggers itself never imports
// delivery (BOUNDARIES: triggers and delivery talk only through this
// port).
type EventSink interface {
	Enqueue(ctx context.Context, org organizations.OrgID, eventType string, data any) error
}

// LogEntry is what PollOnce hands to a Recorder after a failing poll
// attempt (PD34: "poll failure... writes a log entry"). A successful poll
// writes no log entry at all — only the failure path does; this is the
// AC's whole requirement, unlike delivery.LogEntry's "every attempt,
// always".
type LogEntry struct {
	OrgID             organizations.OrgID
	TriggerInstanceID TriggerInstanceID
	TriggerSlug       string
	ConnectionID      connections.ConnectionID
	Error             string
}

// Recorder is a narrow, consumer-defined port for writing a poll-failure
// log entry, so tests can substitute a fake instead of depending on the
// logging module directly (BOUNDARIES: triggers does not depend on logging
// — the composition root wires a logging-backed adapter, mirroring
// execution.Recorder/delivery.Recorder). A nil Recorder is a silent no-op.
type Recorder interface {
	Record(ctx context.Context, entry LogEntry) error
}
