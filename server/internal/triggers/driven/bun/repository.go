// Package bun is the triggers module's persistence adapter. It is the only
// place in the module that imports database/sql or uptrace/bun; the row
// struct's bun tags are the schema's source of truth.
package bun

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/connections"
	"beecon/internal/db"
	"beecon/internal/organizations"
	"beecon/internal/triggers"
)

// TriggerInstanceRow is the trigger_instances table schema. Config is
// stored as a JSON-encoded object in a single text column so the same
// schema works identically across both the Postgres and SQLite dialects
// (mirrors organizations.OrganizationRow's AllowedRedirectURIs). SeenIDs
// (Slice 4) is stored the same way, as a JSON-encoded array. PollLeaseUntil
// is never set by Save (always persisted nil) — only ClaimDuePolls' own raw
// claim query writes a real lease, mirroring delivery.EventRow's own
// LeaseUntil convention: a facade-level save always means "this tick is
// over, whatever the outcome," so the lease is released in the same
// breath.
type TriggerInstanceRow struct {
	upstreambun.BaseModel `bun:"table:trigger_instances,alias:ti"`

	ID             string     `bun:"id,pk"`
	OrgID          string     `bun:"organization_id,notnull"`
	UserID         string     `bun:"user_id,notnull"`
	ConnectionID   string     `bun:"connection_id,notnull"`
	TriggerSlug    string     `bun:"trigger_slug,notnull"`
	Config         string     `bun:"config,notnull"`
	Status         string     `bun:"status,notnull"`
	WatermarkAt    *time.Time `bun:"watermark_at"`
	SeenIDs        string     `bun:"seen_ids,notnull"`
	PausedAt       *time.Time `bun:"paused_at"`
	NextPollAt     *time.Time `bun:"next_poll_at"`
	PollLeaseUntil *time.Time `bun:"poll_lease_until"`
	CreatedAt      time.Time  `bun:"created_at,notnull"`
}

// Repository is the bun-backed triggers.Repository and triggers.PollQueue.
type Repository struct {
	db *upstreambun.DB
}

var _ triggers.Repository = (*Repository)(nil)
var _ triggers.PollQueue = (*Repository)(nil)

func NewRepository(db *upstreambun.DB) *Repository {
	return &Repository{db: db}
}

// Save inserts a freshly created TriggerInstance, or — on a conflicting id —
// persists its later status transition (Disable/Enable) and, since Slice 4,
// its advanced poll state (watermark, seen-ids, pause, schedule):
// triggers.Repository deliberately declares no separate Update method.
func (r *Repository) Save(ctx context.Context, instance triggers.TriggerInstance) error {
	row, err := rowFromInstance(instance)
	if err != nil {
		return err
	}
	_, err = r.db.NewInsert().
		Model(&row).
		On("CONFLICT (id) DO UPDATE").
		Set("status = EXCLUDED.status").
		Set("watermark_at = EXCLUDED.watermark_at").
		Set("seen_ids = EXCLUDED.seen_ids").
		Set("paused_at = EXCLUDED.paused_at").
		Set("next_poll_at = EXCLUDED.next_poll_at").
		Set("poll_lease_until = EXCLUDED.poll_lease_until").
		Exec(ctx)
	return err
}

// ClaimDuePolls leases up to limit due ACTIVE TriggerInstances, oldest-
// created first, via the shared internal/db.ClaimDue lease-claim primitive
// (FD7, dual-dialect per section 3 of the architecture doc, mirrors
// delivery.Repository.ClaimDue).
func (r *Repository) ClaimDuePolls(ctx context.Context, now time.Time, leaseTTL time.Duration, limit int) ([]triggers.TriggerInstance, error) {
	var rows []TriggerInstanceRow
	err := db.ClaimDue(ctx, r.db, &rows, "trigger_instances", "poll_lease_until",
		"status = ? AND next_poll_at IS NOT NULL AND next_poll_at <= ?", []any{string(triggers.StatusActive), now},
		now, leaseTTL, limit)
	if err != nil {
		return nil, err
	}

	instances := make([]triggers.TriggerInstance, 0, len(rows))
	for _, row := range rows {
		instance, err := instanceFromRow(&row)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

func (r *Repository) FindByID(ctx context.Context, org organizations.OrgID, id triggers.TriggerInstanceID) (*triggers.TriggerInstance, error) {
	row := new(TriggerInstanceRow)
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
	instance, err := instanceFromRow(row)
	if err != nil {
		return nil, err
	}
	return &instance, nil
}

// ListPage returns TriggerInstances scoped to org, optionally narrowed to
// filter.ConnectionID and/or filter.UserID, newest first (created_at DESC,
// id DESC as a deterministic tiebreaker), limited to filter.Limit rows.
func (r *Repository) ListPage(ctx context.Context, org organizations.OrgID, filter triggers.ListFilter) ([]triggers.TriggerInstance, error) {
	var rows []TriggerInstanceRow
	query := r.db.NewSelect().Model(&rows).Where("organization_id = ?", string(org))

	if filter.ConnectionID != "" {
		query = query.Where("connection_id = ?", string(filter.ConnectionID))
	}
	if filter.UserID != "" {
		query = query.Where("user_id = ?", string(filter.UserID))
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

	results := make([]triggers.TriggerInstance, 0, len(rows))
	for _, row := range rows {
		instance, err := instanceFromRow(&row)
		if err != nil {
			return nil, err
		}
		results = append(results, instance)
	}
	return results, nil
}

// Delete permanently removes the row for id scoped to org (PD33): a
// cross-org or unknown id affects zero rows — the facade has already turned
// that into ErrNotFound via a preceding FindByID.
func (r *Repository) Delete(ctx context.Context, org organizations.OrgID, id triggers.TriggerInstanceID) error {
	_, err := r.db.NewDelete().
		Model((*TriggerInstanceRow)(nil)).
		Where("id = ?", string(id)).
		Where("organization_id = ?", string(org)).
		Exec(ctx)
	return err
}

// DeleteByConnection permanently removes every row bound to connID scoped
// to org (PD33's connection-delete cascade); a connection with no instances
// affects zero rows, which is not an error.
func (r *Repository) DeleteByConnection(ctx context.Context, org organizations.OrgID, connID connections.ConnectionID) error {
	_, err := r.db.NewDelete().
		Model((*TriggerInstanceRow)(nil)).
		Where("connection_id = ?", string(connID)).
		Where("organization_id = ?", string(org)).
		Exec(ctx)
	return err
}

func rowFromInstance(instance triggers.TriggerInstance) (TriggerInstanceRow, error) {
	config, err := json.Marshal(instance.Config)
	if err != nil {
		return TriggerInstanceRow{}, err
	}
	seenIDs, err := json.Marshal(instance.SeenIDs)
	if err != nil {
		return TriggerInstanceRow{}, err
	}
	return TriggerInstanceRow{
		ID:           string(instance.ID),
		OrgID:        string(instance.OrgID),
		UserID:       string(instance.UserID),
		ConnectionID: string(instance.ConnectionID),
		TriggerSlug:  instance.TriggerSlug,
		Config:       string(config),
		Status:       string(instance.Status),
		WatermarkAt:  instance.WatermarkAt,
		SeenIDs:      string(seenIDs),
		PausedAt:     instance.PausedAt,
		NextPollAt:   instance.NextPollAt,
		// PollLeaseUntil is deliberately never carried through from the
		// domain instance (which has no such field at all) — a facade-level
		// Save always releases whatever lease ClaimDuePolls last set.
		PollLeaseUntil: nil,
		CreatedAt:      instance.CreatedAt,
	}, nil
}

func instanceFromRow(row *TriggerInstanceRow) (triggers.TriggerInstance, error) {
	var config map[string]any
	if row.Config != "" {
		if err := json.Unmarshal([]byte(row.Config), &config); err != nil {
			return triggers.TriggerInstance{}, err
		}
	}
	var seenIDs []string
	if row.SeenIDs != "" {
		if err := json.Unmarshal([]byte(row.SeenIDs), &seenIDs); err != nil {
			return triggers.TriggerInstance{}, err
		}
	}
	return triggers.TriggerInstance{
		ID:           triggers.TriggerInstanceID(row.ID),
		OrgID:        organizations.OrgID(row.OrgID),
		UserID:       organizations.UserID(row.UserID),
		ConnectionID: connections.ConnectionID(row.ConnectionID),
		TriggerSlug:  row.TriggerSlug,
		Config:       config,
		Status:       triggers.Status(row.Status),
		WatermarkAt:  row.WatermarkAt,
		SeenIDs:      seenIDs,
		PausedAt:     row.PausedAt,
		NextPollAt:   row.NextPollAt,
		CreatedAt:    row.CreatedAt,
	}, nil
}
