package memory

import (
	"fmt"
	"sync/atomic"
	"time"

	"beecon/internal/catalog"
)

var fixedTestTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Overrides configures NewFacadeWithOverrides. Any zero-value field falls
// back to a deterministic in-memory default.
type Overrides struct {
	Repository  catalog.Repository
	Definitions []catalog.ProviderDefinition
	NewID       func() string
	Now         func() time.Time
}

// NewFacadeWithOverrides builds a catalog.Facade backed by the in-memory
// Repository unless a fake is supplied, with deterministic ids and a fixed
// clock unless overridden. Definitions default to the real embedded provider
// definitions (the same ones production boots with) unless overridden — e.g.
// with fake OAuth endpoints for an OAuth-handshake test.
func NewFacadeWithOverrides(o Overrides) (*catalog.Facade, error) {
	repository := o.Repository
	if repository == nil {
		repository = NewRepository()
	}
	definitions := o.Definitions
	if definitions == nil {
		loaded, err := catalog.DefaultProviderDefinitions()
		if err != nil {
			return nil, err
		}
		definitions = loaded
	}
	newID := o.NewID
	if newID == nil {
		newID = sequentialIDs("intg_")
	}
	now := o.Now
	if now == nil {
		now = func() time.Time { return fixedTestTime }
	}
	return catalog.NewFacade(repository, definitions, newID, now), nil
}

func sequentialIDs(prefix string) func() string {
	var n int64
	return func() string {
		return fmt.Sprintf("%s%d", prefix, atomic.AddInt64(&n, 1))
	}
}
