package delivery

import (
	"context"
	"time"

	"beecon/internal/access"
	"beecon/internal/organizations"
)

// ListCursor is the decoded pagination cursor ListEventsPage's driven port
// accepts: the created_at/id pair of the last event on the previous page,
// so the next page resumes strictly after it in the newest-first ordering
// (mirrors triggers.ListCursor/logging.Cursor).
type ListCursor struct {
	CreatedAt time.Time
	ID        EventID
}

// ListFilter is ListEventsPage's org-scoped driven port query shape: Type
// and DeliveryStatus each optionally narrow the page (empty means
// unrestricted), plus the decoded pagination cursor and the page size to
// fetch.
type ListFilter struct {
	Type           string
	DeliveryStatus Status
	Cursor         *ListCursor
	Limit          int
}

// Repository is the delivery module's org-scoped driven port over both
// entities it owns (BOUNDARIES: WebhookEndpoint and Outbox/WebhookDelivery).
// SaveEndpoint both creates a new endpoint and persists a later change to
// one (URL, event-type filter, status, consecutive-failure count) — there
// is no separate Update; it upserts by the endpoint's own id (Slice 8,
// PD45: many endpoints per org, so the old org-unique upsert no longer
// applies). FindEndpoint returns org's first/oldest endpoint — the PD31
// single-endpoint API's alias target, and dispatchOne's fallback for an
// Event that carries no specific EndpointID (FD7's manually-redelivered
// NO_ENDPOINT case) — while FindEndpointByID/ListEndpoints/DeleteEndpoint
// serve the multi-endpoint CRUD surface. SaveEvent both inserts a freshly
// enqueued Event and persists its later status/attempt transitions
// (DispatchOnce, Redeliver). PurgeTerminalOlderThan (Slice 7, PD44)
// hard-deletes org's own outbox events whose Status is one of
// TerminalStatuses (DELIVERED/FAILED/NO_ENDPOINT) AND whose CreatedAt is
// strictly before cutoff — a PENDING event can never match the status
// predicate, so it is never purged at any age, the critical guarantee this
// port exists to make explicit at the interface itself.
type Repository interface {
	SaveEndpoint(ctx context.Context, endpoint Endpoint) error
	FindEndpoint(ctx context.Context, org organizations.OrgID) (*Endpoint, error)
	FindEndpointByID(ctx context.Context, org organizations.OrgID, id EndpointID) (*Endpoint, error)
	ListEndpoints(ctx context.Context, org organizations.OrgID) ([]Endpoint, error)
	DeleteEndpoint(ctx context.Context, org organizations.OrgID, id EndpointID) error
	SaveEvent(ctx context.Context, event Event) error
	FindEvent(ctx context.Context, org organizations.OrgID, id EventID) (*Event, error)
	ListEventsPage(ctx context.Context, org organizations.OrgID, filter ListFilter) ([]Event, error)
	PurgeTerminalOlderThan(ctx context.Context, org organizations.OrgID, cutoff time.Time) (int, error)
}

// RetentionReader is PurgeOnce's narrow, consumer-defined port onto the
// installation's organizations (Slice 7, PD44) — mirrors logging.
// RetentionReader exactly (same two-method shape, same "wired in app/"
// reasoning: the effective window combines org's own governance override
// with the installation's BEECON_RETENTION_DAYS config value, which
// delivery itself never imports). EffectiveEventRetentionDays is
// EffectiveLogRetentionDays' mirror for terminal outbox events; 0 means
// unlimited/disabled for that org — PurgeOnce skips it entirely.
type RetentionReader interface {
	ListOrgIDs(ctx context.Context) ([]organizations.OrgID, error)
	EffectiveEventRetentionDays(ctx context.Context, org organizations.OrgID) (int, error)
}

// WorkQueue is deliberately installation-level, not org-scoped (see
// test/arch/orgscope_test.go's whitelist): DispatchOnce claims due events
// across every organization by design — the dispatcher is one shared
// background loop, not a per-org one (PD29) — but every claimed Event
// still carries its own OrgID. Split into its own narrow interface (rather
// than a method on Repository) so the org-scope architecture test's
// whitelist stays honest about exactly which operation is deliberately
// unscoped (mirrors triggers.PollQueue and connections.RefreshQueue, added
// in later slices).
type WorkQueue interface {
	// ClaimDue leases up to limit PENDING events whose NextAttemptAt is due
	// as of now (section 3 of the architecture doc: Postgres FOR UPDATE
	// SKIP LOCKED, SQLite the same predicate without it), oldest-created
	// first (PD30: no head-of-line blocking — a failing event's own
	// next_attempt_at moves into the future, so newer due events claim
	// ahead of it), setting each claimed row's lease until now+leaseTTL so
	// a second binary instance never claims the same row.
	ClaimDue(ctx context.Context, now time.Time, leaseTTL time.Duration, limit int) ([]Event, error)
}

// SecretIssuer is a narrow, consumer-defined port satisfied by
// *access.Facade (BOUNDARIES: delivery depends on access): CreateEndpoint
// mints a webhook signing secret at creation (IssueWebhookSecret),
// RotateEndpointSecret rotates it (RotateWebhookSecret, PD31 mirroring
// PD23), GetEndpoint/ListEndpoints show the currently active secret's
// display prefix (WebhookSecretPrefix), and dispatchOne signs every attempt
// with every currently active secret (ActiveWebhookSecrets, 1-2 during a
// rotation's overlap window) — all four now scoped to one specific
// endpoint (Slice 8, PD45: secrets are per-endpoint, not per-org).
// endpointID is delivery's own EndpointID, carried as a plain string:
// access cannot import delivery.EndpointID (BOUNDARIES: access depends on
// organizations only), so *access.Facade's own methods take a plain string
// here — see access/types.go's own EndpointID doc comment for the mirrored
// reasoning on the other side of this port.
type SecretIssuer interface {
	IssueWebhookSecret(ctx context.Context, org organizations.OrgID, endpointID string) (access.IssuedWebhookSecret, error)
	RotateWebhookSecret(ctx context.Context, org organizations.OrgID, endpointID string, overlapHours *int) (access.RotateWebhookSecretResult, error)
	ActiveWebhookSecrets(ctx context.Context, org organizations.OrgID, endpointID string) ([]string, error)
	WebhookSecretPrefix(ctx context.Context, org organizations.OrgID, endpointID string) (string, error)
}

// EndpointCaller is the delivery module's driven port for actually
// reaching a consumer's webhook receiver: a dumb POST with headers, a
// timeout, and no interpretation of the response body — Standard Webhooks'
// success rule is purely the status code (PD30: 2xx within the timeout).
// It returns an error only when the endpoint could not be reached at all
// (including a timeout); a response that was received at all, even a
// non-2xx one, returns (status, nil) so DispatchOnce can apply PD30's
// retry rule uniformly.
type EndpointCaller interface {
	Post(ctx context.Context, url string, headers map[string]string, body []byte, timeout time.Duration) (status int, err error)
}

// OutboxStats is the delivery module's driven port for the outbox
// depth/oldest-pending-age metrics gauges (PD38d, Phase 2 review
// carry-forward): an installation-wide, scrape-time query across every
// organization — a metrics gauge has no per-org dimension anywhere in this
// codebase — so it is deliberately not part of Repository, mirroring
// WorkQueue's own separation for the same reason (keeping the org-scope
// architecture test's reflection over Repository honest about exactly which
// operations are org-scoped).
type OutboxStats interface {
	// PendingDepthAndOldestAge returns how many PENDING events exist right
	// now and the age of the oldest one's CreatedAt as of now (zero when
	// there are none).
	PendingDepthAndOldestAge(ctx context.Context, now time.Time) (depth int, oldestAge time.Duration, err error)
}

// LogEntry is what every delivery attempt writes, always — success or
// failure (the AC: "every delivery attempt writes a log entry with event
// id, event type, attempt number, response status, and duration").
type LogEntry struct {
	OrgID      organizations.OrgID
	EventID    string
	EventType  string
	Attempt    int
	Status     int
	DurationMs int64
}

// Recorder is a narrow, consumer-defined port for writing a
// delivery-attempt log entry (BOUNDARIES: delivery does not depend on
// logging — only the composition root, which already depends on every
// module, wires this through app/recorders.go's deliveryLogRecorder,
// mirroring connections.Recorder and execution.Recorder). A nil Recorder
// is a silent no-op, the same convention those two already established.
type Recorder interface {
	Record(ctx context.Context, entry LogEntry) error
}
