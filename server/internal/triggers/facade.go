package triggers

import (
	"context"
	"time"

	"beecon/internal/connections"
	"beecon/internal/httpx"
	"beecon/internal/metrics"
	"beecon/internal/organizations"
	"beecon/internal/schema"
)

// defaultListLimit and maxListLimit bound List's page size when a caller
// supplies none, or supplies one larger than Beecon allows — the same
// PD10-style bounds every other list endpoint applies.
const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// defaultPollMinInterval is NewFacade's built-in floor for how often any
// instance is scheduled to poll (PD28's "clamped to a platform minimum"),
// used until/unless WithPolling overrides it with BEECON_POLL_MIN_INTERVAL
// (Slice 4) — the same value PD28's own boot-time clamp in catalog already
// defaults to, so a Facade a test never calls WithPolling on still behaves
// sensibly.
const defaultPollMinInterval = 30 * time.Second

// Facade is the triggers module's only public surface.
type Facade struct {
	repo        Repository
	definitions DefinitionReader
	connections ConnectionReader
	newID       func() string
	now         func() time.Time

	pollQueue       PollQueue
	recordSource    RecordSource
	events          EventSink
	recorder        Recorder
	pollMinInterval time.Duration
	metrics         *metrics.Registry
}

// NewFacade wires the facade with its driven Repository, the narrow
// cross-module reader ports it depends on (catalog for trigger
// definitions, connections for connection status/ownership — BOUNDARIES:
// triggers depends on connections and catalog), an injected id minter, and a
// clock so tests can supply deterministic ids and a fixed time. Polling
// (PollOnce, Slice 4) is wired separately via WithPolling — every other
// method (Create/Get/List/Enable/Disable/Delete) works without it.
func NewFacade(repo Repository, definitions DefinitionReader, connectionReader ConnectionReader, newID func() string, now func() time.Time) *Facade {
	return &Facade{
		repo:            repo,
		definitions:     definitions,
		connections:     connectionReader,
		newID:           newID,
		now:             now,
		pollMinInterval: defaultPollMinInterval,
	}
}

// WithPolling wires this facade's PollOnce support (Slice 4): the
// installation-level PollQueue claim port, the RecordSource and EventSink
// ports (app/wiring.go's adapters onto execution.FetchTriggerRecords and
// delivery.Enqueue), the narrow poll-failure Recorder (nil is a silent
// no-op, mirroring execution.Recorder/delivery.Recorder), and
// BEECON_POLL_MIN_INTERVAL — a non-positive value leaves defaultPollMinInterval
// in place rather than disabling the floor entirely.
func (f *Facade) WithPolling(pollQueue PollQueue, recordSource RecordSource, events EventSink, recorder Recorder, pollMinInterval time.Duration) *Facade {
	f.pollQueue = pollQueue
	f.recordSource = recordSource
	f.events = events
	f.recorder = recorder
	if pollMinInterval > 0 {
		f.pollMinInterval = pollMinInterval
	}
	return f
}

// WithMetrics wires this facade's Prometheus recording (PD38d): poller
// PollOnce runs and trigger.event deliveries emitted. A facade built without
// one (the nil zero value NewFacade leaves it at) makes every metrics call a
// silent no-op, exactly like a nil Recorder already does for logging.
func (f *Facade) WithMetrics(registry *metrics.Registry) *Facade {
	f.metrics = registry
	return f
}

// CreateParams is Create's caller-facing input (PD33): the Connection to
// bind to, the trigger's slug, and its config — validated against the
// definition's config schema before anything is persisted.
type CreateParams struct {
	ConnectionID connections.ConnectionID
	TriggerSlug  string
	Config       map[string]any
}

// Create validates params.Config against the trigger definition's config
// schema (via internal/schema — the second consumer of the tidy-first
// extraction), confirms the connection exists (org-scoped) and is ACTIVE,
// and persists a freshly minted TriggerInstance born ACTIVE, owned by the
// connection's own user (PD33). An unknown trigger slug surfaces as
// catalog's own not-found error; an unknown or cross-org connection id
// surfaces as connections' own not-found error; a connection that exists
// but is not ACTIVE is rejected with a status-explaining validation error
// and no instance is created.
func (f *Facade) Create(ctx context.Context, org organizations.OrgID, params CreateParams) (TriggerInstance, error) {
	definition, err := f.definitions.TriggerDefinitionDetail(ctx, params.TriggerSlug)
	if err != nil {
		return TriggerInstance{}, err
	}
	if err := schema.Validate(definition.ConfigSchema, params.Config); err != nil {
		return TriggerInstance{}, ErrInvalidConfig(err)
	}

	connection, err := f.connections.Get(ctx, org, params.ConnectionID)
	if err != nil {
		return TriggerInstance{}, err
	}
	if connection.Status != connections.StatusActive {
		return TriggerInstance{}, ErrConnectionNotActive(string(connection.Status))
	}

	instance := NewTriggerInstance(
		TriggerInstanceID(f.newID()),
		org,
		connection.UserID,
		params.ConnectionID,
		params.TriggerSlug,
		params.Config,
		f.now(),
	)
	if err := f.repo.Save(ctx, instance); err != nil {
		return TriggerInstance{}, err
	}
	return instance, nil
}

// Get fetches a TriggerInstance by id, translating a repository miss (or a
// cross-org match) into ErrNotFound (PD33).
func (f *Facade) Get(ctx context.Context, org organizations.OrgID, id TriggerInstanceID) (TriggerInstance, error) {
	instance, err := f.repo.FindByID(ctx, org, id)
	if err != nil {
		return TriggerInstance{}, err
	}
	if instance == nil {
		return TriggerInstance{}, ErrNotFound()
	}
	return *instance, nil
}

// Disable transitions a TriggerInstance to DISABLED (PD33): it stops
// firing; its poll state (introduced in Slice 4) is retained so a later
// Enable resumes rather than re-baselining. An unknown id, or one belonging
// to another organization, is not-found.
func (f *Facade) Disable(ctx context.Context, org organizations.OrgID, id TriggerInstanceID) (TriggerInstance, error) {
	instance, err := f.Get(ctx, org, id)
	if err != nil {
		return TriggerInstance{}, err
	}
	disabled := instance.Disable()
	if err := f.repo.Save(ctx, disabled); err != nil {
		return TriggerInstance{}, err
	}
	return disabled, nil
}

// Enable transitions a TriggerInstance back to ACTIVE (PD33), resetting its
// watermark to now (Slice 4, PD34/FD6: records that arrived while disabled
// are skipped, never delivered). An unknown id, or one belonging to another
// organization, is not-found.
func (f *Facade) Enable(ctx context.Context, org organizations.OrgID, id TriggerInstanceID) (TriggerInstance, error) {
	instance, err := f.Get(ctx, org, id)
	if err != nil {
		return TriggerInstance{}, err
	}
	enabled := instance.Enable(f.now())
	if err := f.repo.Save(ctx, enabled); err != nil {
		return TriggerInstance{}, err
	}
	return enabled, nil
}

// Delete permanently removes a TriggerInstance (PD33): a subsequent Get
// returns not-found. An unknown id, or one belonging to another
// organization, is not-found.
func (f *Facade) Delete(ctx context.Context, org organizations.OrgID, id TriggerInstanceID) error {
	if _, err := f.Get(ctx, org, id); err != nil {
		return err
	}
	return f.repo.Delete(ctx, org, id)
}

// DeleteByConnection permanently removes every TriggerInstance bound to
// connID within org (PD33: deleting a connection deletes its trigger
// instances). Called through the connections.Dependents port
// (connections.Facade.Delete), wired to this method in app/wiring.go — a
// connection with no instances is a no-op, not an error.
func (f *Facade) DeleteByConnection(ctx context.Context, org organizations.OrgID, connID connections.ConnectionID) error {
	return f.repo.DeleteByConnection(ctx, org, connID)
}

// ListParams is List's caller-facing filter shape (PD33): ConnectionID
// and/or UserID optionally narrow the page (empty means unrestricted);
// Cursor is the opaque cursor a consumer sends back exactly as a previous
// page's NextCursor returned it.
type ListParams struct {
	ConnectionID string
	UserID       string
	Cursor       string
	Limit        int
}

// ListResult is one cursor-paginated page of TriggerInstances, newest
// first; NextCursor is empty when this was the last page.
type ListResult struct {
	Items      []TriggerInstance
	NextCursor string
}

// List returns a page of TriggerInstances scoped to org (PD33), optionally
// narrowed by connectionId and/or userId, newest first.
func (f *Facade) List(ctx context.Context, org organizations.OrgID, params ListParams) (ListResult, error) {
	cursor, err := decodeInstanceCursor(params.Cursor)
	if err != nil {
		return ListResult{}, err
	}
	limit := normalizeListLimit(params.Limit)

	items, err := f.repo.ListPage(ctx, org, ListFilter{
		ConnectionID: connections.ConnectionID(params.ConnectionID),
		UserID:       organizations.UserID(params.UserID),
		Cursor:       cursor,
		Limit:        limit + 1,
	})
	if err != nil {
		return ListResult{}, err
	}
	return paginateInstances(items, limit), nil
}

func paginateInstances(items []TriggerInstance, limit int) ListResult {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	result := ListResult{Items: items}
	if hasMore {
		last := items[len(items)-1]
		result.NextCursor = encodeInstanceCursor(last.CreatedAt, last.ID)
	}
	return result
}

func normalizeListLimit(requested int) int {
	if requested <= 0 {
		return defaultListLimit
	}
	if requested > maxListLimit {
		return maxListLimit
	}
	return requested
}

func encodeInstanceCursor(createdAt time.Time, id TriggerInstanceID) string {
	return httpx.EncodeCursor(createdAt.UTC().Format(time.RFC3339Nano), string(id))
}

func decodeInstanceCursor(raw string) (*ListCursor, error) {
	fields, err := httpx.DecodeCursor(raw, 2)
	if err != nil {
		return nil, ErrInvalidCursor()
	}
	if fields == nil {
		return nil, nil
	}
	createdAt, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return nil, ErrInvalidCursor()
	}
	return &ListCursor{CreatedAt: createdAt, ID: TriggerInstanceID(fields[1])}, nil
}
