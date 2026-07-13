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
	repo  Repository
	newID func() string
	now   func() time.Time
}

// NewFacade wires the facade with an injected id minter and a clock so tests
// can supply deterministic ids and a fixed time.
func NewFacade(repo Repository, newID func() string, now func() time.Time) *Facade {
	return &Facade{repo: repo, newID: newID, now: now}
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
