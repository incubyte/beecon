// Package delivery owns the outbound webhook channel (PD27/PD30/PD31,
// Phase 3 Slice 3): the org's single WebhookEndpoint, the Outbox of Events
// persisted before delivery, Standard Webhooks signing, the retry
// schedule, and manual redelivery. Depends on access (webhook signing
// secrets) and organizations (BOUNDARIES.md).
package delivery

import (
	"encoding/json"
	"net/url"
	"time"

	"beecon/internal/organizations"
)

// EndpointID is minted only by Facade.SetEndpoint, on first creation
// (PD31: wep_-prefixed).
type EndpointID string

// Endpoint is the org's single webhook receiver (PD31: one per
// organization). The signing secret behind it is a separate
// access.WebhookSigningSecret, reached only through the SecretIssuer port —
// Endpoint itself carries no secret material.
type Endpoint struct {
	ID        EndpointID
	OrgID     organizations.OrgID
	URL       string
	CreatedAt time.Time
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

// PD32's fixed event-type vocabulary.
const (
	EventTypeTriggerEvent      = "trigger.event"
	EventTypeConnectionExpired = "connection.expired"
	EventTypeWebhookTest       = "webhook.test"
)

// Event is one outbox entry: the exact envelope bytes persisted once at
// Enqueue (PD32) and re-signed with a fresh timestamp on every delivery
// attempt — retries and manual redeliveries are byte-identical by
// construction. An org with no configured Endpoint at Enqueue time gets
// Status NO_ENDPOINT and zero attempts (FD7: it stays put until a manual
// Redeliver, even after an endpoint is later configured).
type Event struct {
	ID            EventID
	OrgID         organizations.OrgID
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
