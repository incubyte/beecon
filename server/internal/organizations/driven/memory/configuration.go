package memory

import (
	"fmt"
	"sync/atomic"
	"time"

	"beecon/internal/organizations"
)

var fixedTestTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Overrides configures NewFacadeWithOverrides. Any zero-value field falls
// back to a deterministic in-memory default.
type Overrides struct {
	Repository           organizations.Repository
	UserRepository       organizations.UserRepository
	GovernanceRepository organizations.GovernanceRepository
	NewID                func() string
	NewUserID            func() string
	Now                  func() time.Time
}

// NewFacadeWithOverrides builds an organizations.Facade backed by the
// in-memory Repository unless a fake is supplied, with deterministic ids and
// a fixed clock unless overridden.
func NewFacadeWithOverrides(o Overrides) *organizations.Facade {
	repository := o.Repository
	userRepository := o.UserRepository
	governanceRepository := o.GovernanceRepository
	if repository == nil || userRepository == nil || governanceRepository == nil {
		shared := NewRepository()
		if repository == nil {
			repository = shared
		}
		if userRepository == nil {
			userRepository = shared
		}
		if governanceRepository == nil {
			governanceRepository = shared
		}
	}
	newID := o.NewID
	if newID == nil {
		newID = sequentialIDs("org_")
	}
	newUserID := o.NewUserID
	if newUserID == nil {
		newUserID = sequentialIDs("user_")
	}
	now := o.Now
	if now == nil {
		now = func() time.Time { return fixedTestTime }
	}
	return organizations.NewFacade(repository, userRepository, governanceRepository, newID, newUserID, now)
}

func sequentialIDs(prefix string) func() string {
	var n int64
	return func() string {
		return fmt.Sprintf("%s%d", prefix, atomic.AddInt64(&n, 1))
	}
}
