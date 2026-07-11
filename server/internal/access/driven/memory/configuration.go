package memory

import (
	"fmt"
	"sync/atomic"
	"time"

	"beecon/internal/access"
)

var fixedTestTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Overrides configures NewFacadeWithOverrides. Any zero-value field falls
// back to a deterministic in-memory default.
type Overrides struct {
	Repository   access.Repository
	PrefixLookup access.PrefixLookup
	NewID        func() string
	Now          func() time.Time
}

// NewFacadeWithOverrides builds an access.Facade backed by the in-memory
// Repository unless a fake is supplied, with deterministic ids and a fixed
// clock unless overridden.
func NewFacadeWithOverrides(o Overrides) *access.Facade {
	repository := o.Repository
	prefixLookup := o.PrefixLookup
	if repository == nil || prefixLookup == nil {
		shared := NewRepository()
		if repository == nil {
			repository = shared
		}
		if prefixLookup == nil {
			prefixLookup = shared
		}
	}
	newID := o.NewID
	if newID == nil {
		newID = sequentialIDs("key_")
	}
	now := o.Now
	if now == nil {
		now = func() time.Time { return fixedTestTime }
	}
	return access.NewFacade(repository, prefixLookup, newID, now)
}

func sequentialIDs(prefix string) func() string {
	var n int64
	return func() string {
		return fmt.Sprintf("%s%d", prefix, atomic.AddInt64(&n, 1))
	}
}
