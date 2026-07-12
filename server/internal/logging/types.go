// Package logging owns the EventLog entity and its redaction rules
// (Slice 5): every tool execution and OAuth token exchange writes one
// EventLog entry (AC8), with secrets stripped from its request/response
// bodies before persistence (AC9).
package logging

import (
	"time"

	"beecon/internal/organizations"
)

// LogID is minted only by Record.
type LogID string

// Kind distinguishes which kind of provider exchange an EventLog entry
// recorded.
type Kind string

const (
	KindToolExecution      Kind = "tool_execution"
	KindOAuthTokenExchange Kind = "oauth_token_exchange"
)

// EventLog is the domain aggregate root: one provider exchange, scoped to
// the organization that made it. UserID and ConnectionID are plain strings
// (not organizations.UserID / connections.ConnectionID) — logging does not
// depend on the connections module (BOUNDARIES), and carrying the raw id
// value is all a log entry ever needs. ToolSlug is empty for an OAuth
// token-exchange entry ("tool slug (where applicable)", AC8).
type EventLog struct {
	ID           LogID
	OrgID        organizations.OrgID
	UserID       string
	ConnectionID string
	ToolSlug     string
	Kind         Kind
	Status       int
	DurationMs   int64
	RequestBody  string
	ResponseBody string
	CreatedAt    time.Time
}

// RecordInput is what Record persists as a new EventLog (AC8): every field a
// caller (execution's tool call, connections' OAuth token exchange) supplies
// about one provider exchange. RequestBody and ResponseBody may carry
// secrets in cleartext when a caller builds them — Record redacts them
// (AC9) before anything reaches the repository.
type RecordInput struct {
	OrgID        organizations.OrgID
	UserID       string
	ConnectionID string
	ToolSlug     string
	Kind         Kind
	Status       int
	DurationMs   int64
	RequestBody  string
	ResponseBody string
}

// newEventLog builds the EventLog Record persists: id and CreatedAt come
// from the facade's injected minter/clock, and RequestBody/ResponseBody are
// redacted here — the single place every entry passes through before
// Save (AC9).
func newEventLog(id LogID, in RecordInput, now time.Time) EventLog {
	return EventLog{
		ID:           id,
		OrgID:        in.OrgID,
		UserID:       in.UserID,
		ConnectionID: in.ConnectionID,
		ToolSlug:     in.ToolSlug,
		Kind:         in.Kind,
		Status:       in.Status,
		DurationMs:   in.DurationMs,
		RequestBody:  Redact(in.RequestBody),
		ResponseBody: Redact(in.ResponseBody),
		CreatedAt:    now,
	}
}
