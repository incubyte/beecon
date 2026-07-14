package memory

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/organizations"
	"beecon/internal/vault"
)

var fixedTestTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// defaultTestVaultKey is a fixed 32-byte AES-256 key used when Overrides
// doesn't supply its own Vault — harmless for tests, since it never leaves
// the in-memory process.
var defaultTestVaultKey = []byte("catalog-test-vault-key-32-bytes!")

// Overrides configures NewFacadeWithOverrides. Any zero-value field falls
// back to a deterministic in-memory default. Governance defaults to
// unrestrictedGovernanceReader (Slice 5) — every org inherits the full
// catalog unless a test explicitly wires its own organizations.GovernanceReader
// (or the real organizations.Facade), matching PD42's continuity guarantee
// for every catalog test written before governance existed.
type Overrides struct {
	Repository  catalog.Repository
	Definitions []catalog.ProviderDefinition
	NewID       func() string
	Now         func() time.Time
	Vault       *vault.Vault
	Governance  organizations.GovernanceReader
}

// unrestrictedGovernanceReader is the default Overrides.Governance: every
// organization inherits the full installation catalog (PD42's default),
// regardless of which org id is asked about.
type unrestrictedGovernanceReader struct{}

func (unrestrictedGovernanceReader) GetGovernance(_ context.Context, org organizations.OrgID) (organizations.Governance, error) {
	return organizations.NewDefaultGovernance(org), nil
}

// NewFacadeWithOverrides builds a catalog.Facade backed by the in-memory
// Repository unless a fake is supplied, with deterministic ids, a fixed
// clock, and a fixed-key Vault unless overridden. Definitions default to the
// real embedded provider definitions (the same ones production boots with)
// unless overridden — e.g. with fake OAuth endpoints for an OAuth-handshake
// test.
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
	tokenVault := o.Vault
	if tokenVault == nil {
		tokenVault, _ = vault.NewVault(defaultTestVaultKey)
	}
	governance := o.Governance
	if governance == nil {
		governance = unrestrictedGovernanceReader{}
	}
	return catalog.NewFacade(repository, definitions, newID, now, tokenVault, governance), nil
}

func sequentialIDs(prefix string) func() string {
	var n int64
	return func() string {
		return fmt.Sprintf("%s%d", prefix, atomic.AddInt64(&n, 1))
	}
}
