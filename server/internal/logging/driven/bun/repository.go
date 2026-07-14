// Package bun is the logging module's persistence adapter. It is the only
// place in the module that imports database/sql or uptrace/bun; the row
// struct's bun tags are the schema's source of truth.
package bun

import (
	"context"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/logging"
	"beecon/internal/organizations"
)

// EventLogRow is the event_logs table schema.
type EventLogRow struct {
	upstreambun.BaseModel `bun:"table:event_logs,alias:el"`

	ID                string    `bun:"id,pk"`
	OrgID             string    `bun:"org_id,notnull"`
	UserID            string    `bun:"user_id,notnull"`
	ConnectionID      string    `bun:"connection_id,notnull"`
	ToolSlug          string    `bun:"tool_slug,notnull"`
	Kind              string    `bun:"kind,notnull"`
	Status            int       `bun:"status,notnull"`
	DurationMs        int64     `bun:"duration_ms,notnull"`
	RequestBody       string    `bun:"request_body,notnull"`
	ResponseBody      string    `bun:"response_body,notnull"`
	RateLimited       bool      `bun:"rate_limited,notnull"`
	EventID           *string   `bun:"event_id"`
	Attempt           int       `bun:"attempt,notnull"`
	TriggerInstanceID *string   `bun:"trigger_instance_id"`
	CreatedAt         time.Time `bun:"created_at,notnull"`
}

// Repository is the bun-backed logging.Repository.
type Repository struct {
	db *upstreambun.DB
}

var _ logging.Repository = (*Repository)(nil)

func NewRepository(db *upstreambun.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Save(ctx context.Context, entry logging.EventLog) error {
	row := rowFromEventLog(entry)
	_, err := r.db.NewInsert().Model(&row).Exec(ctx)
	return err
}

// Query returns entries scoped to org (AC10), matching filter, newest first
// (created_at DESC, id DESC as a deterministic tiebreaker), limited to
// filter.Limit rows.
func (r *Repository) Query(ctx context.Context, org organizations.OrgID, filter logging.Filter) ([]logging.EventLog, error) {
	var rows []EventLogRow
	query := r.db.NewSelect().Model(&rows).Where("org_id = ?", string(org))

	if filter.ConnectionID != "" {
		query = query.Where("connection_id = ?", filter.ConnectionID)
	}
	if filter.UserID != "" {
		query = query.Where("user_id = ?", filter.UserID)
	}
	if filter.ToolSlug != "" {
		query = query.Where("tool_slug = ?", filter.ToolSlug)
	}
	if filter.From != nil {
		query = query.Where("created_at >= ?", *filter.From)
	}
	if filter.To != nil {
		query = query.Where("created_at <= ?", *filter.To)
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

	entries := make([]logging.EventLog, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, eventLogFromRow(&row))
	}
	return entries, nil
}

func rowFromEventLog(entry logging.EventLog) EventLogRow {
	return EventLogRow{
		ID:                string(entry.ID),
		OrgID:             string(entry.OrgID),
		UserID:            entry.UserID,
		ConnectionID:      entry.ConnectionID,
		ToolSlug:          entry.ToolSlug,
		Kind:              string(entry.Kind),
		Status:            entry.Status,
		DurationMs:        entry.DurationMs,
		RequestBody:       entry.RequestBody,
		ResponseBody:      entry.ResponseBody,
		RateLimited:       entry.RateLimited,
		EventID:           nullableString(entry.EventID),
		Attempt:           entry.Attempt,
		TriggerInstanceID: nullableString(entry.TriggerInstanceID),
		CreatedAt:         entry.CreatedAt,
	}
}

func eventLogFromRow(row *EventLogRow) logging.EventLog {
	return logging.EventLog{
		ID:                logging.LogID(row.ID),
		OrgID:             organizations.OrgID(row.OrgID),
		UserID:            row.UserID,
		ConnectionID:      row.ConnectionID,
		ToolSlug:          row.ToolSlug,
		Kind:              logging.Kind(row.Kind),
		Status:            row.Status,
		DurationMs:        row.DurationMs,
		RequestBody:       row.RequestBody,
		ResponseBody:      row.ResponseBody,
		RateLimited:       row.RateLimited,
		EventID:           stringFromNullable(row.EventID),
		Attempt:           row.Attempt,
		TriggerInstanceID: stringFromNullable(row.TriggerInstanceID),
		CreatedAt:         row.CreatedAt,
	}
}

// nullableString converts an EventLog's own "" (no event id — every kind
// but KindWebhookDelivery) into the NULL event_id column PD5's schema
// growth calls for, rather than storing an empty string.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func stringFromNullable(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
