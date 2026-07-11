package memory

import (
	"fmt"
	"sync/atomic"
	"time"

	"beecon/internal/connections"
)

var fixedTestTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Overrides configures NewFacadeWithOverrides. Repository, NewID, NewToken,
// BaseURL, and Now fall back to a deterministic in-memory default when left
// zero-valued. Organizations, Users, and Integrations are the narrow
// cross-module reader ports connections.Facade depends on (BOUNDARIES:
// connections depends on organizations and catalog) — callers supply the
// other modules' own facades (or test doubles satisfying the same narrow
// interface) directly, the same way app/wiring.go composes them in
// production.
type Overrides struct {
	Repository    connections.Repository
	Organizations connections.OrganizationReader
	Users         connections.UserReader
	Integrations  connections.IntegrationReader
	NewID         func() string
	NewToken      func() string
	BaseURL       string
	Now           func() time.Time
}

// NewFacadeWithOverrides builds a connections.Facade backed by the in-memory
// Repository unless a fake is supplied, with deterministic ids/tokens, a
// fixed clock, and a placeholder base URL unless overridden.
func NewFacadeWithOverrides(o Overrides) *connections.Facade {
	repository := o.Repository
	if repository == nil {
		repository = NewRepository()
	}
	newID := o.NewID
	if newID == nil {
		newID = sequentialIDs("conn_")
	}
	newToken := o.NewToken
	if newToken == nil {
		newToken = sequentialIDs("connect_token_")
	}
	baseURL := o.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	now := o.Now
	if now == nil {
		now = func() time.Time { return fixedTestTime }
	}
	return connections.NewFacade(repository, o.Organizations, o.Users, o.Integrations, newID, newToken, baseURL, now)
}

func sequentialIDs(prefix string) func() string {
	var n int64
	return func() string {
		return fmt.Sprintf("%s%d", prefix, atomic.AddInt64(&n, 1))
	}
}
