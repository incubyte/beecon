// Package bun is the delivery module's persistence adapter. It is the only
// place in the module that imports database/sql or uptrace/bun; the row
// structs' bun tags are the schema's source of truth.
package bun

import (
	"context"
	"database/sql"
	"errors"
	"time"

	upstreambun "github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"

	"beecon/internal/delivery"
	"beecon/internal/organizations"
)

// EndpointRow is the webhook_endpoints table schema (PD31: organization_id
// is UNIQUE — one endpoint per org).
type EndpointRow struct {
	upstreambun.BaseModel `bun:"table:webhook_endpoints,alias:we"`

	ID        string    `bun:"id,pk"`
	OrgID     string    `bun:"organization_id,notnull"`
	URL       string    `bun:"url,notnull"`
	CreatedAt time.Time `bun:"created_at,notnull"`
}

// EventRow is the outbox_events table schema. LeaseUntil is never set by
// SaveEvent (always persisted nil) — only ClaimDue's own raw claim query
// writes a real lease; a facade-level save always means "this attempt is
// over, whatever the outcome," so the lease is released in the same
// breath.
type EventRow struct {
	upstreambun.BaseModel `bun:"table:outbox_events,alias:oe"`

	ID            string     `bun:"id,pk"`
	OrgID         string     `bun:"organization_id,notnull"`
	Type          string     `bun:"type,notnull"`
	Body          string     `bun:"body,notnull"`
	Status        string     `bun:"status,notnull"`
	Attempts      int        `bun:"attempts,notnull"`
	NextAttemptAt time.Time  `bun:"next_attempt_at,notnull"`
	LastAttemptAt *time.Time `bun:"last_attempt_at"`
	LeaseUntil    *time.Time `bun:"lease_until"`
	CreatedAt     time.Time  `bun:"created_at,notnull"`
}

// claimDuePostgres and claimDueSQLite are section 3 of the architecture
// doc's dual-dialect claim primitive: an atomic "find due rows, lease them,
// return them" UPDATE...RETURNING. Postgres adds FOR UPDATE SKIP LOCKED so
// two binary instances never claim the same row and never block each
// other; SQLite needs neither (its single-writer lock already makes the
// UPDATE-with-lease-predicate atomic) and doesn't support the clause.
const claimDuePostgres = `
UPDATE outbox_events
SET lease_until = ?
WHERE id IN (
	SELECT id FROM outbox_events
	WHERE status = ? AND next_attempt_at <= ?
		AND (lease_until IS NULL OR lease_until < ?)
	ORDER BY created_at
	LIMIT ?
	FOR UPDATE SKIP LOCKED
)
RETURNING *
`

const claimDueSQLite = `
UPDATE outbox_events
SET lease_until = ?
WHERE id IN (
	SELECT id FROM outbox_events
	WHERE status = ? AND next_attempt_at <= ?
		AND (lease_until IS NULL OR lease_until < ?)
	ORDER BY created_at
	LIMIT ?
)
RETURNING *
`

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

// SaveEndpoint both creates org's one endpoint and persists a later URL
// change (PD31): the upsert conflicts on organization_id (not id), so an
// existing row's own id and created_at are left untouched — only url is
// ever updated.
func (r *Repository) SaveEndpoint(ctx context.Context, endpoint delivery.Endpoint) error {
	row := endpointRowFrom(endpoint)
	_, err := r.db.NewInsert().
		Model(&row).
		On("CONFLICT (organization_id) DO UPDATE").
		Set("url = EXCLUDED.url").
		Exec(ctx)
	return err
}

func (r *Repository) FindEndpoint(ctx context.Context, org organizations.OrgID) (*delivery.Endpoint, error) {
	row := new(EndpointRow)
	err := r.db.NewSelect().
		Model(row).
		Where("organization_id = ?", string(org)).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	endpoint := endpointFromRow(row)
	return &endpoint, nil
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

// ClaimDue leases up to limit due events, oldest-created first, using the
// dialect-appropriate claim query (dual-dialect per section 3 of the
// architecture doc).
func (r *Repository) ClaimDue(ctx context.Context, now time.Time, leaseTTL time.Duration, limit int) ([]delivery.Event, error) {
	query := claimDueSQLite
	if r.db.Dialect().Name() == dialect.PG {
		query = claimDuePostgres
	}
	leaseUntil := now.Add(leaseTTL)

	var rows []EventRow
	err := r.db.NewRaw(query, leaseUntil, string(delivery.StatusPending), now, now, limit).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}

	events := make([]delivery.Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, eventFromRow(&row))
	}
	return events, nil
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

func endpointRowFrom(endpoint delivery.Endpoint) EndpointRow {
	return EndpointRow{
		ID:        string(endpoint.ID),
		OrgID:     string(endpoint.OrgID),
		URL:       endpoint.URL,
		CreatedAt: endpoint.CreatedAt,
	}
}

func endpointFromRow(row *EndpointRow) delivery.Endpoint {
	return delivery.Endpoint{
		ID:        delivery.EndpointID(row.ID),
		OrgID:     organizations.OrgID(row.OrgID),
		URL:       row.URL,
		CreatedAt: row.CreatedAt,
	}
}

func eventRowFrom(event delivery.Event) EventRow {
	return EventRow{
		ID:            string(event.ID),
		OrgID:         string(event.OrgID),
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
	return delivery.Event{
		ID:            delivery.EventID(row.ID),
		OrgID:         organizations.OrgID(row.OrgID),
		Type:          row.Type,
		Body:          []byte(row.Body),
		Status:        delivery.Status(row.Status),
		Attempts:      row.Attempts,
		NextAttemptAt: row.NextAttemptAt,
		LastAttemptAt: row.LastAttemptAt,
		CreatedAt:     row.CreatedAt,
	}
}
