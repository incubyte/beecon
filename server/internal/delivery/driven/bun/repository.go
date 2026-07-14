// Package bun is the delivery module's persistence adapter. It is the only
// place in the module that imports database/sql or uptrace/bun; the row
// structs' bun tags are the schema's source of truth.
package bun

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/db"
	"beecon/internal/delivery"
	"beecon/internal/organizations"
)

// EndpointRow is the webhook_endpoints table schema (Slice 8, migration
// 0020: many rows per org now — organization_id's old unique index is
// dropped in favor of a plain lookup index). EventTypes is a nullable
// JSON-encoded array in a text column (nil = "match every event type",
// PD45), matching the org_governance AllowList convention.
type EndpointRow struct {
	upstreambun.BaseModel `bun:"table:webhook_endpoints,alias:we"`

	ID                  string    `bun:"id,pk"`
	OrgID               string    `bun:"organization_id,notnull"`
	URL                 string    `bun:"url,notnull"`
	EventTypes          *string   `bun:"event_types"`
	Status              string    `bun:"status,notnull"`
	ConsecutiveFailures int       `bun:"consecutive_failures,notnull"`
	CreatedAt           time.Time `bun:"created_at,notnull"`
}

// EventRow is the outbox_events table schema. LeaseUntil is never set by
// SaveEvent (always persisted nil) — only ClaimDue's own raw claim query
// writes a real lease; a facade-level save always means "this attempt is
// over, whatever the outcome," so the lease is released in the same
// breath. EndpointID (Slice 8, migration 0020) is nullable: NULL for a
// NO_ENDPOINT placeholder Event with no specific endpoint yet resolved
// (FD7), and for every pre-Slice-8 historical row (the migration leaves
// existing outbox_events rows' endpoint_id NULL) — every fan-out Event
// Enqueue creates from Slice 8 onward always sets it.
type EventRow struct {
	upstreambun.BaseModel `bun:"table:outbox_events,alias:oe"`

	ID            string     `bun:"id,pk"`
	OrgID         string     `bun:"organization_id,notnull"`
	EndpointID    *string    `bun:"endpoint_id"`
	Type          string     `bun:"type,notnull"`
	Body          string     `bun:"body,notnull"`
	Status        string     `bun:"status,notnull"`
	Attempts      int        `bun:"attempts,notnull"`
	NextAttemptAt time.Time  `bun:"next_attempt_at,notnull"`
	LastAttemptAt *time.Time `bun:"last_attempt_at"`
	LeaseUntil    *time.Time `bun:"lease_until"`
	CreatedAt     time.Time  `bun:"created_at,notnull"`
}

// Repository is the bun-backed delivery.Repository and delivery.WorkQueue.
type Repository struct {
	db *upstreambun.DB
}

var _ delivery.Repository = (*Repository)(nil)
var _ delivery.WorkQueue = (*Repository)(nil)
var _ delivery.OutboxStats = (*Repository)(nil)

func NewRepository(db *upstreambun.DB) *Repository {
	return &Repository{db: db}
}

// SaveEndpoint both creates a new endpoint and persists a later change to
// one — the upsert conflicts on id, not organization_id (Slice 8: many
// endpoints per org now), so an existing row's own created_at is left
// untouched — url, event_types, status, and consecutive_failures are the
// only columns a later save ever changes.
func (r *Repository) SaveEndpoint(ctx context.Context, endpoint delivery.Endpoint) error {
	row, err := endpointRowFrom(endpoint)
	if err != nil {
		return err
	}
	_, err = r.db.NewInsert().
		Model(&row).
		On("CONFLICT (id) DO UPDATE").
		Set("url = EXCLUDED.url").
		Set("event_types = EXCLUDED.event_types").
		Set("status = EXCLUDED.status").
		Set("consecutive_failures = EXCLUDED.consecutive_failures").
		Exec(ctx)
	return err
}

// FindEndpoint returns org's first/oldest endpoint — the PD31 single-
// endpoint API's alias target (Slice 8), and dispatchOne's fallback lookup
// for an Event carrying no specific EndpointID.
func (r *Repository) FindEndpoint(ctx context.Context, org organizations.OrgID) (*delivery.Endpoint, error) {
	row := new(EndpointRow)
	err := r.db.NewSelect().
		Model(row).
		Where("organization_id = ?", string(org)).
		Order("created_at ASC", "id ASC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return endpointFromRow(row)
}

// FindEndpointByID looks up one specific endpoint by id, scoped to org
// (Slice 8): the multi-endpoint CRUD surface's read.
func (r *Repository) FindEndpointByID(ctx context.Context, org organizations.OrgID, id delivery.EndpointID) (*delivery.Endpoint, error) {
	row := new(EndpointRow)
	err := r.db.NewSelect().
		Model(row).
		Where("id = ?", string(id)).
		Where("organization_id = ?", string(org)).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return endpointFromRow(row)
}

// ListEndpoints returns every one of org's endpoints, oldest first (Slice
// 8: cap enforcement counts this list's length, and it's the multi-endpoint
// CRUD surface's own list read).
func (r *Repository) ListEndpoints(ctx context.Context, org organizations.OrgID) ([]delivery.Endpoint, error) {
	var rows []EndpointRow
	err := r.db.NewSelect().
		Model(&rows).
		Where("organization_id = ?", string(org)).
		Order("created_at ASC", "id ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	endpoints := make([]delivery.Endpoint, 0, len(rows))
	for _, row := range rows {
		endpoint, err := endpointFromRow(&row)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, *endpoint)
	}
	return endpoints, nil
}

// DeleteEndpoint permanently removes one endpoint, scoped to org (Slice 8,
// AC8).
func (r *Repository) DeleteEndpoint(ctx context.Context, org organizations.OrgID, id delivery.EndpointID) error {
	_, err := r.db.NewDelete().
		Model((*EndpointRow)(nil)).
		Where("id = ?", string(id)).
		Where("organization_id = ?", string(org)).
		Exec(ctx)
	return err
}

// SaveEvent both inserts a freshly enqueued Event and persists its later
// status/attempt transitions (DispatchOnce, Redeliver) — delivery.Repository
// declares no separate Update, mirroring triggers.Repository's own Save.
func (r *Repository) SaveEvent(ctx context.Context, event delivery.Event) error {
	row := eventRowFrom(event)
	_, err := r.db.NewInsert().
		Model(&row).
		On("CONFLICT (id) DO UPDATE").
		Set("status = EXCLUDED.status").
		Set("attempts = EXCLUDED.attempts").
		Set("next_attempt_at = EXCLUDED.next_attempt_at").
		Set("last_attempt_at = EXCLUDED.last_attempt_at").
		Set("lease_until = EXCLUDED.lease_until").
		Exec(ctx)
	return err
}

func (r *Repository) FindEvent(ctx context.Context, org organizations.OrgID, id delivery.EventID) (*delivery.Event, error) {
	row := new(EventRow)
	err := r.db.NewSelect().
		Model(row).
		Where("id = ?", string(id)).
		Where("organization_id = ?", string(org)).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	event := eventFromRow(row)
	return &event, nil
}

// ListEventsPage returns Events scoped to org, optionally narrowed to
// filter.Type and/or filter.DeliveryStatus, newest first (created_at DESC,
// id DESC as a deterministic tiebreaker), limited to filter.Limit rows.
func (r *Repository) ListEventsPage(ctx context.Context, org organizations.OrgID, filter delivery.ListFilter) ([]delivery.Event, error) {
	var rows []EventRow
	query := r.db.NewSelect().Model(&rows).Where("organization_id = ?", string(org))

	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}
	if filter.DeliveryStatus != "" {
		query = query.Where("status = ?", string(filter.DeliveryStatus))
	}
	if filter.Cursor != nil {
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))",
			filter.Cursor.CreatedAt, filter.Cursor.CreatedAt, string(filter.Cursor.ID))
	}

	err := query.
		Order("created_at DESC", "id DESC").
		Limit(filter.Limit).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	events := make([]delivery.Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, eventFromRow(&row))
	}
	return events, nil
}

// ClaimDue leases up to limit due events, oldest-created first, via the
// shared internal/db.ClaimDue lease-claim primitive (FD7, dual-dialect per
// section 3 of the architecture doc).
func (r *Repository) ClaimDue(ctx context.Context, now time.Time, leaseTTL time.Duration, limit int) ([]delivery.Event, error) {
	var rows []EventRow
	err := db.ClaimDue(ctx, r.db, &rows, "outbox_events", "lease_until",
		"status = ? AND next_attempt_at <= ?", []any{string(delivery.StatusPending), now},
		now, leaseTTL, limit)
	if err != nil {
		return nil, err
	}

	events := make([]delivery.Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, eventFromRow(&row))
	}
	return events, nil
}

// PurgeTerminalOlderThan hard-deletes org's own terminal (Status one of
// delivery.TerminalStatuses) outbox events whose CreatedAt is strictly
// before cutoff (Slice 7, PD44) — the critical guarantee is the WHERE
// clause's own status IN (...) filter naming exactly delivery.
// TerminalStatuses: a PENDING event can never match it, at any age, so a
// pending or retry-scheduled event always survives a purge run regardless
// of how old it is. Unlike ClaimDue's own lease-then-release cycle, a purge
// delete needs no lease column of its own: the DELETE itself is the
// terminal action, so a concurrent second binary instance's identical
// DELETE simply finds nothing left to remove (idempotent) — exactly PD44's
// "two binaries never double-purge" guarantee, without inventing a lease
// column on outbox_events nobody asked for.
func (r *Repository) PurgeTerminalOlderThan(ctx context.Context, org organizations.OrgID, cutoff time.Time) (int, error) {
	statuses := make([]string, 0, len(delivery.TerminalStatuses))
	for _, status := range delivery.TerminalStatuses {
		statuses = append(statuses, string(status))
	}
	result, err := r.db.NewDelete().
		Model((*EventRow)(nil)).
		Where("organization_id = ?", string(org)).
		Where("created_at < ?", cutoff).
		Where("status IN (?)", upstreambun.In(statuses)).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

// pendingOutboxStatsRow is the scan target for PendingDepthAndOldestAge's
// aggregate query (PD38d). OldestCreatedAt is nil when there are no PENDING
// events at all.
type pendingOutboxStatsRow struct {
	Count           int        `bun:"count"`
	OldestCreatedAt *time.Time `bun:"oldest_created_at"`
}

// PendingDepthAndOldestAge returns how many PENDING events exist right now
// and the age of the oldest one (PD38d) — deliberately not org-scoped
// (delivery.OutboxStats' own doc comment): a metrics gauge is an
// installation-wide signal.
func (r *Repository) PendingDepthAndOldestAge(ctx context.Context, now time.Time) (int, time.Duration, error) {
	var row pendingOutboxStatsRow
	err := r.db.NewSelect().
		Model((*EventRow)(nil)).
		ColumnExpr("COUNT(*) AS count").
		ColumnExpr("MIN(created_at) AS oldest_created_at").
		Where("status = ?", string(delivery.StatusPending)).
		Scan(ctx, &row)
	if err != nil {
		return 0, 0, err
	}
	if row.OldestCreatedAt == nil {
		return row.Count, 0, nil
	}
	return row.Count, now.Sub(*row.OldestCreatedAt), nil
}

func endpointRowFrom(endpoint delivery.Endpoint) (EndpointRow, error) {
	var eventTypes *string
	if endpoint.EventTypes != nil {
		encoded, err := json.Marshal(endpoint.EventTypes)
		if err != nil {
			return EndpointRow{}, err
		}
		value := string(encoded)
		eventTypes = &value
	}
	return EndpointRow{
		ID:                  string(endpoint.ID),
		OrgID:               string(endpoint.OrgID),
		URL:                 endpoint.URL,
		EventTypes:          eventTypes,
		Status:              string(endpoint.Status),
		ConsecutiveFailures: endpoint.ConsecutiveFailures,
		CreatedAt:           endpoint.CreatedAt,
	}, nil
}

func endpointFromRow(row *EndpointRow) (*delivery.Endpoint, error) {
	var eventTypes []string
	if row.EventTypes != nil {
		if err := json.Unmarshal([]byte(*row.EventTypes), &eventTypes); err != nil {
			return nil, err
		}
	}
	endpoint := delivery.Endpoint{
		ID:                  delivery.EndpointID(row.ID),
		OrgID:               organizations.OrgID(row.OrgID),
		URL:                 row.URL,
		EventTypes:          eventTypes,
		Status:              delivery.EndpointStatus(row.Status),
		ConsecutiveFailures: row.ConsecutiveFailures,
		CreatedAt:           row.CreatedAt,
	}
	return &endpoint, nil
}

func eventRowFrom(event delivery.Event) EventRow {
	var endpointID *string
	if event.EndpointID != "" {
		value := string(event.EndpointID)
		endpointID = &value
	}
	return EventRow{
		ID:            string(event.ID),
		OrgID:         string(event.OrgID),
		EndpointID:    endpointID,
		Type:          event.Type,
		Body:          string(event.Body),
		Status:        string(event.Status),
		Attempts:      event.Attempts,
		NextAttemptAt: event.NextAttemptAt,
		LastAttemptAt: event.LastAttemptAt,
		LeaseUntil:    nil,
		CreatedAt:     event.CreatedAt,
	}
}

func eventFromRow(row *EventRow) delivery.Event {
	var endpointID delivery.EndpointID
	if row.EndpointID != nil {
		endpointID = delivery.EndpointID(*row.EndpointID)
	}
	return delivery.Event{
		ID:            delivery.EventID(row.ID),
		OrgID:         organizations.OrgID(row.OrgID),
		EndpointID:    endpointID,
		Type:          row.Type,
		Body:          []byte(row.Body),
		Status:        delivery.Status(row.Status),
		Attempts:      row.Attempts,
		NextAttemptAt: row.NextAttemptAt,
		LastAttemptAt: row.LastAttemptAt,
		CreatedAt:     row.CreatedAt,
	}
}
