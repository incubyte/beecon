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
	// KindWebhookDelivery marks one outbox delivery attempt (Phase 3 Slice
	// 3): every attempt writes one entry, success or failure. ToolSlug
	// carries the delivered event's own type ("trigger.event",
	// "connection.expired", "webhook.test") — the same general-purpose
	// "further classifier, where applicable" role it already plays for a
	// tool execution's slug.
	KindWebhookDelivery Kind = "webhook_delivery"
	// KindTriggerPoll marks one failing poll attempt (Phase 3 Slice 4,
	// PD34): unlike KindWebhookDelivery, only failures write an entry — a
	// successful poll writes none. ToolSlug carries the trigger's own
	// slug, and TriggerInstanceID names which instance failed.
	KindTriggerPoll Kind = "trigger_poll"
)

// EventLog is the domain aggregate root: one provider exchange, scoped to
// the organization that made it. UserID and ConnectionID are plain strings
// (not organizations.UserID / connections.ConnectionID) — logging does not
// depend on the connections module (BOUNDARIES), and carrying the raw id
// value is all a log entry ever needs. ToolSlug is empty for an OAuth
// token-exchange entry ("tool slug (where applicable)", AC8). RateLimited
// marks a tool-execution attempt IsRateLimited normalized as a rate limit
// (PD21, Slice 6) — always false for an OAuth token-exchange entry. EventID
// and Attempt (Phase 3 Slice 3) are set only for a KindWebhookDelivery
// entry: the outbox event's own evt_ id and this delivery attempt's
// 1-indexed number. TriggerInstanceID (Phase 3 Slice 4) is set only for a
// KindTriggerPoll entry — a plain string (not triggers.TriggerInstanceID):
// logging does not depend on the triggers module (BOUNDARIES), the same
// reason ConnectionID is a plain string rather than connections.ConnectionID.
type EventLog struct {
	ID                LogID
	OrgID             organizations.OrgID
	UserID            string
	ConnectionID      string
	ToolSlug          string
	Kind              Kind
	Status            int
	DurationMs        int64
	RequestBody       string
	ResponseBody      string
	RateLimited       bool
	EventID           string
	Attempt           int
	TriggerInstanceID string
	CreatedAt         time.Time
}

// RecordInput is what Record persists as a new EventLog (AC8): every field a
// caller (execution's tool call, connections' OAuth token exchange,
// delivery's outbox attempt, triggers' poll failure) supplies about one
// provider exchange. RequestBody and ResponseBody may carry secrets in
// cleartext when a caller builds them — Record redacts them (AC9) before
// anything reaches the repository.
type RecordInput struct {
	OrgID             organizations.OrgID
	UserID            string
	ConnectionID      string
	ToolSlug          string
	Kind              Kind
	Status            int
	DurationMs        int64
	RequestBody       string
	ResponseBody      string
	RateLimited       bool
	EventID           string
	Attempt           int
	TriggerInstanceID string
}

// newEventLog builds the EventLog Record persists: id and CreatedAt come
// from the facade's injected minter/clock, and RequestBody/ResponseBody are
// redacted here — the single place every entry passes through before
// Save (AC9) — scoped by in.Kind (PD25).
func newEventLog(id LogID, in RecordInput, now time.Time) EventLog {
	return EventLog{
		ID:                id,
		OrgID:             in.OrgID,
		UserID:            in.UserID,
		ConnectionID:      in.ConnectionID,
		ToolSlug:          in.ToolSlug,
		Kind:              in.Kind,
		Status:            in.Status,
		DurationMs:        in.DurationMs,
		RequestBody:       Redact(in.Kind, in.RequestBody),
		ResponseBody:      Redact(in.Kind, in.ResponseBody),
		RateLimited:       in.RateLimited,
		EventID:           in.EventID,
		Attempt:           in.Attempt,
		TriggerInstanceID: in.TriggerInstanceID,
		CreatedAt:         now,
	}
}
