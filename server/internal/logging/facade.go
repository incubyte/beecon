package logging

import (
	"context"
	"time"

	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// defaultPageLimit and maxPageLimit bound Query's page size (PD10) when a
// caller supplies none, or supplies one larger than Beecon allows.
const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

// Facade is the logging module's only public surface.
type Facade struct {
	repo      Repository
	retention RetentionReader
	newID     func() string
	now       func() time.Time
}

// NewFacade wires the facade with an injected id minter and a clock so tests
// can supply deterministic ids and a fixed time.
func NewFacade(repo Repository, newID func() string, now func() time.Time) *Facade {
	return &Facade{repo: repo, newID: newID, now: now}
}

// WithRetention wires PurgeOnce's RetentionReader port (Slice 7, PD44). A
// Facade built without one (NewFacade's own zero value) makes PurgeOnce a
// silent no-op, the same nil-safe convention delivery.Facade's WithMetrics/
// WithOutboxStats already established.
func (f *Facade) WithRetention(retention RetentionReader) *Facade {
	f.retention = retention
	return f
}

// Record persists one EventLog for a tool execution or an OAuth token
// exchange (AC8), redacting its request/response bodies before they ever
// reach the repository (AC9).
func (f *Facade) Record(ctx context.Context, in RecordInput) error {
	entry := newEventLog(LogID(f.newID()), in, f.now())
	return f.repo.Save(ctx, entry)
}

// QueryParams is Query's caller-facing filter shape: an encoded cursor
// string (as a consumer would send it back on the next request) rather than
// the decoded Cursor the Repository port takes.
type QueryParams struct {
	ConnectionID string
	UserID       string
	ToolSlug     string
	From         *time.Time
	To           *time.Time
	Cursor       string
	Limit        int
}

// QueryResult is one page of matching entries, newest first, plus the
// opaque cursor to request the next page — empty when this was the last
// page.
type QueryResult struct {
	Entries    []EventLog
	NextCursor string
}

// Query returns log entries scoped to org (AC10: a caller only ever sees its
// own organization's entries), filtered by params, newest first,
// cursor-paginated.
func (f *Facade) Query(ctx context.Context, org organizations.OrgID, params QueryParams) (QueryResult, error) {
	cursor, err := decodeCursor(params.Cursor)
	if err != nil {
		return QueryResult{}, err
	}
	limit := normalizeLimit(params.Limit)

	entries, err := f.repo.Query(ctx, org, Filter{
		ConnectionID: params.ConnectionID,
		UserID:       params.UserID,
		ToolSlug:     params.ToolSlug,
		From:         params.From,
		To:           params.To,
		Cursor:       cursor,
		Limit:        limit + 1,
	})
	if err != nil {
		return QueryResult{}, err
	}

	return paginate(entries, limit), nil
}

// paginate trims entries (fetched one over limit) down to a page, deriving
// the next-page cursor from the last entry when there was one over.
func paginate(entries []EventLog, limit int) QueryResult {
	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}
	result := QueryResult{Entries: entries}
	if hasMore {
		last := entries[len(entries)-1]
		result.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return result
}

func normalizeLimit(requested int) int {
	if requested <= 0 {
		return defaultPageLimit
	}
	if requested > maxPageLimit {
		return maxPageLimit
	}
	return requested
}

func encodeCursor(createdAt time.Time, id LogID) string {
	return httpx.EncodeCursor(createdAt.UTC().Format(time.RFC3339Nano), string(id))
}

func decodeCursor(raw string) (*Cursor, error) {
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
	return &Cursor{CreatedAt: createdAt, ID: LogID(fields[1])}, nil
}

// PurgeOnce is the purge worker's Run func for logging's half of PD44
// (Slice 7): for every organization in the installation, it resolves that
// org's own effective log-retention window and, unless the window is 0
// (unlimited/disabled for that org — skipped entirely), hard-deletes its
// EventLog rows older than now minus that window. A Facade built without
// WithRetention makes this a silent no-op, mirroring delivery's own
// nil-Recorder convention. Age-only: unlike delivery's own PurgeOnce, a log
// entry carries no in-flight state to protect — every entry past the
// window is eligible regardless of its Kind.
func (f *Facade) PurgeOnce(ctx context.Context) error {
	if f.retention == nil {
		return nil
	}
	orgs, err := f.retention.ListOrgIDs(ctx)
	if err != nil {
		return err
	}
	now := f.now()
	for _, org := range orgs {
		days, err := f.retention.EffectiveLogRetentionDays(ctx, org)
		if err != nil {
			return err
		}
		if days <= 0 {
			continue
		}
		cutoff := now.AddDate(0, 0, -days)
		if _, err := f.repo.PurgeOlderThan(ctx, org, cutoff); err != nil {
			return err
		}
	}
	return nil
}
