package memory

import (
	"fmt"
	"sync/atomic"
	"time"

	"beecon/internal/logging"
)

var fixedTestTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Overrides configures NewFacadeWithOverrides. Any zero-value field falls
// back to a deterministic in-memory default.
type Overrides struct {
	Repository logging.Repository
	NewID      func() string
	Now        func() time.Time
}

// NewFacadeWithOverrides builds a logging.Facade backed by the in-memory
// Repository unless a fake is supplied, with a deterministic id minter and a
// fixed clock unless overridden.
func NewFacadeWithOverrides(o Overrides) *logging.Facade {
	repository := o.Repository
	if repository == nil {
		repository = NewRepository()
	}
	newID := o.NewID
	if newID == nil {
		newID = sequentialIDs("log_")
	}
	now := o.Now
	if now == nil {
		now = func() time.Time { return fixedTestTime }
	}
	return logging.NewFacade(repository, newID, now)
}

func sequentialIDs(prefix string) func() string {
	var n int64
	return func() string {
		return fmt.Sprintf("%s%d", prefix, atomic.AddInt64(&n, 1))
	}
}
