// Package delivery owns the outbound webhook channel (PD27/PD30/PD31,
// Phase 3 Slice 3): the org's single WebhookEndpoint, the Outbox of Events
// persisted before delivery, Standard Webhooks signing, the retry
// schedule, and manual redelivery. Depends on access (webhook signing
// secrets) and organizations (BOUNDARIES.md).
package delivery

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"beecon/internal/organizations"
)

// EndpointID is minted only by Facade.SetEndpoint/CreateEndpoint, on first
// creation (PD31: wep_-prefixed).
type EndpointID string

// EndpointStatus is an Endpoint's current fan-out eligibility (PD45, Slice
// 8): ENABLED receives fan-out normally; DISABLED was turned off by an
// operator; DISABLED_AUTO was turned off automatically after
// BEECON_ENDPOINT_AUTODISABLE_FAILURES consecutive terminal FAILED
// deliveries (dispatchOne's own inline bookkeeping, facade.go). Enqueue's
// fan-out only ever selects ENABLED endpoints (both DISABLED and
// DISABLED_AUTO stop receiving new events); an operator re-enabling an
// endpoint (EnableEndpoint) resets ConsecutiveFailures and resumes fan-out
// regardless of which status it was in.
type EndpointStatus string

const (
	EndpointStatusEnabled      EndpointStatus = "ENABLED"
	EndpointStatusDisabled     EndpointStatus = "DISABLED"
	EndpointStatusDisabledAuto EndpointStatus = "DISABLED_AUTO"
)

// Endpoint is one of an org's webhook receivers (PD45, Slice 8: many per
// org, up to BEECON_WEBHOOK_ENDPOINT_CAP — the Phase 3 model of exactly one
// per org is now the special case of an org with a single Endpoint row).
// The signing secret behind it is a separate, per-endpoint
// access.WebhookSigningSecret, reached only through the SecretIssuer port —
// Endpoint itself carries no secret material. EventTypes is the endpoint's
// optional event-type filter: nil matches every event type (PD45's
// continuity-preserving default — the Phase 3 migration leaves the
// migrated single endpoint with EventTypes nil); a non-nil, non-empty slice
// restricts fan-out to exactly those types.
type Endpoint struct {
	ID                  EndpointID
	OrgID               organizations.OrgID
	URL                 string
	EventTypes          []string
	Status              EndpointStatus
	ConsecutiveFailures int
	CreatedAt           time.Time
}

// IsEnabled reports whether Enqueue's fan-out may still select this
// endpoint (PD45): false for both DISABLED and DISABLED_AUTO.
func (e Endpoint) IsEnabled() bool {
	return e.Status == EndpointStatusEnabled
}

// MatchesEventType reports whether eventType passes this endpoint's own
// event-type filter (PD45): a nil EventTypes matches every type (the
// continuity-preserving default); otherwise eventType must be named
// explicitly.
func (e Endpoint) MatchesEventType(eventType string) bool {
	if e.EventTypes == nil {
		return true
	}
	for _, t := range e.EventTypes {
		if t == eventType {
			return true
		}
	}
	return false
}

// KnownEventTypes is PD32's fixed event-type vocabulary, also the set an
// endpoint's own EventTypes filter (PD45) is validated against.
var KnownEventTypes = []string{EventTypeTriggerEvent, EventTypeConnectionExpired, EventTypeWebhookTest}

// ValidateEventTypeFilter rejects an endpoint's proposed event-type filter
// (PD45's "a subset of the event-type enum"): nil (absent/JSON null) is
// always valid — "match every type" — but a non-nil filter must name at
// least one type (an empty, non-nil filter would silently receive nothing,
// forever, which is never what an operator setting one intends) and every
// named type must be one of KnownEventTypes.
func ValidateEventTypeFilter(eventTypes []string) error {
	if eventTypes == nil {
		return nil
	}
	if len(eventTypes) == 0 {
		return ErrValidation("eventTypes", "must name at least one event type, or be omitted to match every type")
	}
	for _, t := range eventTypes {
		if !isKnownEventType(t) {
			return ErrValidation("eventTypes", fmt.Sprintf("unknown event type %q", t))
		}
	}
	return nil
}

func isKnownEventType(eventType string) bool {
	for _, known := range KnownEventTypes {
		if eventType == known {
			return true
		}
	}
	return false
}

// ValidateEndpointURL rejects anything that isn't an absolute http(s) URL
// (PD31's own AC). Beecon deliberately does not block private/loopback
// addresses here: org-key holders are trusted operators of their own
// self-hosted installation, not untrusted third parties — this is an
// operator note, not an oversight (PD31).
func ValidateEndpointURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ErrValidation("url", "must be an absolute http(s) URL")
	}
	return nil
}

// EventID is the outbox's idempotency key (PD32): stable across every
// retry and manual redelivery of the same event — the "webhook-id" header
// value a consumer deduplicates on.
type EventID string

// Status is an Event's current delivery state.
type Status string

const (
	StatusPending    Status = "PENDING"
	StatusDelivered  Status = "DELIVERED"
	StatusFailed     Status = "FAILED"
	StatusNoEndpoint Status = "NO_ENDPOINT"
)

// TerminalStatuses are the delivery outcomes PD44's purge worker is allowed
// to hard-delete once they are older than an org's effective retention
// window (Slice 7) — deliberately excluding StatusPending: an event that is
// still pending or mid-retry must never be purged, at any age, regardless
// of the org's configured window. This is the single source of truth both
// the bun and in-memory Repository adapters build their purge predicate
// from, so "never purge a pending event" is guaranteed at the domain layer,
// not re-derived (and possibly re-broken) per adapter.
var TerminalStatuses = []Status{StatusDelivered, StatusFailed, StatusNoEndpoint}

// IsTerminal reports whether status is one PD44's purge worker may
// hard-delete once past the retention window — false for StatusPending,
// always.
func IsTerminal(status Status) bool {
	for _, terminal := range TerminalStatuses {
		if status == terminal {
			return true
		}
	}
	return false
}

// PD32's fixed event-type vocabulary.
const (
	EventTypeTriggerEvent      = "trigger.event"
	EventTypeConnectionExpired = "connection.expired"
	EventTypeWebhookTest       = "webhook.test"
)

// Event is one outbox entry: the exact envelope bytes persisted once at
// Enqueue (PD32) and re-signed with a fresh timestamp on every delivery
// attempt — retries and manual redeliveries are byte-identical by
// construction. EndpointID (Slice 8, PD45) is the specific endpoint this
// copy fans out to — every enabled, filter-matching endpoint gets its own
// Event row, its own delivery record, and its own retry schedule, so one
// endpoint's failure never blocks another. An org with no enabled,
// filter-matching endpoint at Enqueue time (including "no endpoint
// configured at all", Phase 3's own case) gets a single Status NO_ENDPOINT
// placeholder Event with an empty EndpointID and zero attempts (FD7: it
// stays put until a manual Redeliver, even after a matching endpoint is
// later configured or enabled — dispatchOne then resolves it against the
// org's first endpoint, mirroring Phase 3's own single-endpoint lookup).
type Event struct {
	ID            EventID
	OrgID         organizations.OrgID
	EndpointID    EndpointID
	Type          string
	Body          []byte
	Status        Status
	Attempts      int
	NextAttemptAt time.Time
	LastAttemptAt *time.Time
	CreatedAt     time.Time
}

// envelope is the PD32 event envelope: marshaled exactly once at Enqueue
// and persisted verbatim in Event.Body — every retry and manual
// redelivery signs these same bytes with a fresh timestamp.
type envelope struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	CreatedAt string `json:"createdAt"`
	Data      any    `json:"data"`
}

// marshalEnvelope builds the PD32 envelope's exact wire bytes, once.
func marshalEnvelope(id EventID, eventType string, data any, now time.Time) ([]byte, error) {
	if data == nil {
		data = map[string]any{}
	}
	return json.Marshal(envelope{
		ID:        string(id),
		Type:      eventType,
		CreatedAt: now.UTC().Format(time.RFC3339Nano),
		Data:      data,
	})
}
