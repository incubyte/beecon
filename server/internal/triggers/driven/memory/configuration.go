package memory

import (
	"fmt"
	"sync/atomic"
	"time"

	"beecon/internal/triggers"
)

var fixedTestTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Overrides configures NewFacadeWithOverrides. Repository/PollQueue, NewID,
// and Now fall back to a deterministic in-memory default when left
// zero-valued. Definitions and Connections are the narrow cross-module
// reader ports triggers.Facade depends on (BOUNDARIES: triggers depends on
// catalog and connections) — callers supply the other modules' own facades
// (or test doubles satisfying the same narrow interface) directly, the same
// way app/wiring.go composes them in production. RecordSource, Events, and
// Recorder (Slice 4) are polling's own narrow ports, wired only when at
// least one of them is supplied — a caller that only exercises
// Create/List/Get/Enable/Disable/Delete never needs to.
type Overrides struct {
	Repository      triggers.Repository
	Definitions     triggers.DefinitionReader
	Connections     triggers.ConnectionReader
	RecordSource    triggers.RecordSource
	Events          triggers.EventSink
	Recorder        triggers.Recorder
	PollMinInterval time.Duration
	NewID           func() string
	Now             func() time.Time
}

// NewFacadeWithOverrides builds a triggers.Facade backed by the in-memory
// Repository unless a fake is supplied, with deterministic ids and a fixed
// clock unless overridden. WithPolling is always called, so PollOnce works
// out of the box against the in-memory PollQueue even when a caller supplies
// no RecordSource/Events/Recorder of its own (a facade exercising only the
// non-polling surface never calls PollOnce, so the zero-valued ports are
// never dereferenced).
func NewFacadeWithOverrides(o Overrides) *triggers.Facade {
	repository := o.Repository
	pollQueue := triggers.PollQueue(nil)
	slugIndex := triggers.TriggerSlugIndex(nil)
	if repository == nil {
		shared := NewRepository()
		repository = shared
		pollQueue = shared
		slugIndex = shared
	} else {
		if asPollQueue, ok := repository.(triggers.PollQueue); ok {
			pollQueue = asPollQueue
		}
		if asSlugIndex, ok := repository.(triggers.TriggerSlugIndex); ok {
			slugIndex = asSlugIndex
		}
	}
	newID := o.NewID
	if newID == nil {
		newID = sequentialIDs("trg_")
	}
	now := o.Now
	if now == nil {
		now = func() time.Time { return fixedTestTime }
	}
	facade := triggers.NewFacade(repository, o.Definitions, o.Connections, newID, now)
	facade = facade.WithPolling(pollQueue, o.RecordSource, o.Events, o.Recorder, o.PollMinInterval)
	return facade.WithTriggerSlugIndex(slugIndex)
}

func sequentialIDs(prefix string) func() string {
	var n int64
	return func() string {
		return fmt.Sprintf("%s%d", prefix, atomic.AddInt64(&n, 1))
	}
}
