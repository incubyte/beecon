package memory

import (
	"fmt"
	"sync/atomic"
	"time"

	"beecon/internal/connections"
	"beecon/internal/vault"
)

var fixedTestTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// defaultTestVaultKey is a fixed 32-byte AES-256 key used when Overrides
// doesn't supply its own Vault — harmless for tests, since it never leaves
// the in-memory process.
var defaultTestVaultKey = []byte("memory-test-vault-key-32-bytes!!")

// Overrides configures NewFacadeWithOverrides. Repository, NewID, NewToken,
// NewState, BaseURL, Now, and Vault fall back to a deterministic in-memory
// default when left zero-valued. Organizations, Users, Integrations,
// Providers, and OAuthClient are the narrow cross-module reader ports and
// driven port connections.Facade depends on (BOUNDARIES: connections depends
// on organizations and catalog) — callers supply the other modules' own
// facades (or test doubles satisfying the same narrow interface) directly,
// the same way app/wiring.go composes them in production.
type Overrides struct {
	Repository    connections.Repository
	Organizations connections.OrganizationReader
	Users         connections.UserReader
	Integrations  connections.IntegrationReader
	Providers     connections.ProviderDefinitionReader
	OAuthClient   connections.OAuthClient
	Recorder      connections.Recorder
	Vault         *vault.Vault
	NewID         func() string
	NewToken      func() string
	NewState      func() string
	BaseURL       string
	Now           func() time.Time
}

// NewFacadeWithOverrides builds a connections.Facade backed by the in-memory
// Repository unless a fake is supplied, with deterministic ids/tokens/state,
// a fixed clock, a placeholder base URL, and a fixed-key Vault unless
// overridden.
func NewFacadeWithOverrides(o Overrides) *connections.Facade {
	repository, oauthRepository := repositoryAndOAuthRepository(o.Repository)
	newID := o.NewID
	if newID == nil {
		newID = sequentialIDs("conn_")
	}
	newToken := o.NewToken
	if newToken == nil {
		newToken = sequentialIDs("connect_token_")
	}
	newState := o.NewState
	if newState == nil {
		newState = sequentialIDs("state_")
	}
	baseURL := o.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	now := o.Now
	if now == nil {
		now = func() time.Time { return fixedTestTime }
	}
	tokenVault := o.Vault
	if tokenVault == nil {
		tokenVault, _ = vault.NewVault(defaultTestVaultKey)
	}
	return connections.NewFacade(
		repository,
		oauthRepository,
		o.Organizations,
		o.Users,
		o.Integrations,
		o.Providers,
		tokenVault,
		o.OAuthClient,
		o.Recorder,
		newID,
		newToken,
		newState,
		baseURL,
		now,
	)
}

// repositoryAndOAuthRepository resolves the connections.Repository and
// connections.OAuthRepository NewFacadeWithOverrides wires the facade with.
// When no Repository is supplied, a single default in-memory Repository
// satisfies both ports. When one is supplied, it is reused as the
// OAuthRepository too if it happens to implement that narrow port as well
// (the in-memory Repository does) — otherwise the OAuth handshake ports stay
// unset, matching how Organizations/Users/Integrations stay unset until a
// test supplies them.
func repositoryAndOAuthRepository(repository connections.Repository) (connections.Repository, connections.OAuthRepository) {
	if repository == nil {
		defaultRepo := NewRepository()
		return defaultRepo, defaultRepo
	}
	oauthRepository, _ := repository.(connections.OAuthRepository)
	return repository, oauthRepository
}

func sequentialIDs(prefix string) func() string {
	var n int64
	return func() string {
		return fmt.Sprintf("%s%d", prefix, atomic.AddInt64(&n, 1))
	}
}
