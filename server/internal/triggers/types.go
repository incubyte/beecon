// Package triggers owns the TriggerInstance entity (PD33): a consumer's
// binding of a Connection to a catalog TriggerDefinition, from creation
// through enable/disable/delete — and, from Slice 4, the poll ingestion that
// fires it. Depends on connections and catalog (BOUNDARIES.md).
package triggers

import (
	"time"

	"beecon/internal/connections"
	"beecon/internal/organizations"
)

// TriggerInstanceID is minted only by Facade.Create (PD33: trg_-prefixed,
// stable for the instance's whole life).
type TriggerInstanceID string

// Status is a TriggerInstance's lifecycle state (PD33). There is no separate
// "paused" status: a connection leaving ACTIVE pausing its instances is a
// poll-time concern (Slice 4), not a stored status here.
type Status string

const (
	// StatusActive is a TriggerInstance's status at birth (PD33: instances are
	// born enabled) and after Enable.
	StatusActive Status = "ACTIVE"
	// StatusDisabled is a TriggerInstance's status after Disable: it stops
	// firing, but its poll state (introduced in Slice 4) is retained so a
	// later Enable resumes rather than re-baselining.
	StatusDisabled Status = "DISABLED"
)

// TriggerInstance is the domain aggregate root: one consumer's subscription
// to a TriggerDefinition (identified by slug, PD14), bound to a Connection.
// Config is the instance's own config values, already validated against the
// definition's configSchema before this was constructed (Facade.Create's
// responsibility — TriggerInstance itself does no validation, mirroring how
// connections.Connection trusts its own callers). WatermarkAt, SeenIDs,
// PausedAt, and NextPollAt (Slice 4, PD34) are the poll engine's own state:
// WatermarkAt is nil only before the instance's first poll tick has ever
// run — the baseline poll (watermark.go); SeenIDs guards the exact boundary
// timestamp against a re-delivered record; PausedAt is set, independent of
// Status, exactly when the instance's connection has left ACTIVE (there is
// no separate "paused" Status — see Status' own doc comment); NextPollAt is
// when PollOnce's claim next considers this instance due.
type TriggerInstance struct {
	ID           TriggerInstanceID
	OrgID        organizations.OrgID
	UserID       organizations.UserID
	ConnectionID connections.ConnectionID
	TriggerSlug  string
	Config       map[string]any
	Status       Status
	WatermarkAt  *time.Time
	SeenIDs      []string
	PausedAt     *time.Time
	NextPollAt   *time.Time
	CreatedAt    time.Time
}

// NewTriggerInstance constructs a freshly created TriggerInstance. Callers
// are responsible for validating config against the trigger definition's
// config schema, and confirming the connection exists (org-scoped) and is
// ACTIVE, before calling this — it always starts ACTIVE (PD33: born
// enabled), and userID is always the owning connection's own UserID (a
// trigger instance has no independent owner). NextPollAt starts at now
// (Slice 4, PD34): the instance becomes claimable by the very next poller
// scan, whose first poll tick — since WatermarkAt starts nil — is the
// baseline poll.
func NewTriggerInstance(
	id TriggerInstanceID,
	org organizations.OrgID,
	userID organizations.UserID,
	connectionID connections.ConnectionID,
	triggerSlug string,
	config map[string]any,
	now time.Time,
) TriggerInstance {
	nextPollAt := now
	return TriggerInstance{
		ID:           id,
		OrgID:        org,
		UserID:       userID,
		ConnectionID: connectionID,
		TriggerSlug:  triggerSlug,
		Config:       config,
		Status:       StatusActive,
		NextPollAt:   &nextPollAt,
		CreatedAt:    now,
	}
}

// Disable returns a copy of t transitioned to DISABLED (PD33): it stops
// firing; its config and connection binding are untouched. Poll state
// (watermark, seen-ids) is also left untouched here — the reset PD34
// requires happens at Enable, not at Disable (FD6: "pause-resume skips the
// gap" — implemented as reset-at-resume, so records that arrive between
// this Disable and a later Enable are always skipped, never buffered).
func (t TriggerInstance) Disable() TriggerInstance {
	disabled := t
	disabled.Status = StatusDisabled
	return disabled
}

// Enable returns a copy of t transitioned back to ACTIVE (PD33), with its
// watermark reset to now and its seen-ids cleared (Slice 4, PD34/FD6):
// records that arrived while disabled are skipped, never delivered — the
// same "skip the gap" semantics a connection leaving and rejoining ACTIVE
// applies via watermark.Resume.
func (t TriggerInstance) Enable(now time.Time) TriggerInstance {
	enabled := t
	enabled.Status = StatusActive
	enabled.WatermarkAt = &now
	enabled.SeenIDs = nil
	return enabled
}
