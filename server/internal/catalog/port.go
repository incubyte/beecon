package catalog

import (
	"context"

	"beecon/internal/registrybundle"
)

// Repository is the catalog module's driven port for the Integration entity.
// Integrations are installation-level, not org-scoped (PD7: visible to every
// organization in the installation) — there is no organization id to filter
// by, mirroring organizations.Repository's own installation-level scope.
// FindByID returns (nil, nil) on a miss; the facade translates that into
// ErrIntegrationNotFound. UpdateEncryptedClientSecret persists the boot
// backfill's re-sealed ciphertext for a row that predates the vault (PD17) —
// it never receives a plaintext value.
type Repository interface {
	Save(ctx context.Context, integration Integration) error
	FindByID(ctx context.Context, id IntegrationID) (*Integration, error)
	ListAll(ctx context.Context) ([]Integration, error)
	UpdateEncryptedClientSecret(ctx context.Context, id IntegrationID, encryptedClientSecret string) error
}

// ActivatedDefinitions is the catalog module's driven port for the
// DB-backed activated-definition store (PD65, Phase 5 registry sub-phase):
// installation-level, like Repository — a provider's activated definition
// applies to every organization in the installation, so there is no
// organization id to scope by. Save upserts (one row per provider slug: an
// installation is pinned to exactly one activated version per provider at a
// time, PD66); FindByProviderSlug returns (nil, nil) on a miss; ListAll
// feeds LoadActivatedDefinitions' boot-time rebuild of every
// previously-activated provider's served definition after a restart. Delete
// removes providerSlug's row entirely (Slice 4, PD66): Activate's own
// rollback path uses it to undo a persisted row it just wrote when a later
// step in the same activation fails and this provider had never been
// activated before (so there is no earlier row to restore instead) — a
// no-op deleting zero rows is not an error.
type ActivatedDefinitions interface {
	Save(ctx context.Context, activated ActivatedDefinition) error
	FindByProviderSlug(ctx context.Context, providerSlug string) (*ActivatedDefinition, error)
	ListAll(ctx context.Context) ([]ActivatedDefinition, error)
	Delete(ctx context.Context, providerSlug string) error
}

// RegistryClient is the catalog module's driven port for pulling from the
// separate registry service (PD64): an HTTP adapter (driven/registryhttp)
// implements this against a real registry over
// BEECON_REGISTRY_URL/BEECON_REGISTRY_API_KEY; an in-memory fake
// (driven/memory) is used by tests that don't stand up a real registry HTTP
// server. FetchBundle returns the full bundle for providerSlug at version,
// including every tool's tool_ id and its input/output schemas (Slice 1).
// ListVersions returns every version the registry offers for providerSlug
// (Slice 3's review-before-adopting flow) — just the version strings, since
// that is all an operator's version-list/diff review needs. An unreachable
// registry or an unknown version surface as this package's own errors
// (ErrRegistryUnavailable/ErrBundleVersionNotFound) so callers never depend
// on the adapter's own error shape.
type RegistryClient interface {
	FetchBundle(ctx context.Context, providerSlug, version string) (registrybundle.Bundle, error)
	ListVersions(ctx context.Context, providerSlug string) ([]string, error)
}

// TriggerInstancePauser is a narrow, consumer-defined port for pausing live
// trigger-instances a newly-activated version's removed trigger definitions
// leave dangling (PD66, Phase 5 registry sub-phase Slice 4): wired in
// app/wiring.go to an adapter over *triggers.Facade — catalog never depends
// on triggers (BOUNDARIES: the dependency runs the other way; triggers
// depends on catalog), mirroring RegistryClient's own consumer-defined-port
// shape for the opposite module pairing. Activate calls this once per
// trigger slug it has determined the newly-activated version no longer
// declares, relative to whatever this installation was serving immediately
// before (the embedded seed, or an earlier activated version) — never for a
// provider's very first-ever activation, since there is nothing yet to diff
// against.
type TriggerInstancePauser interface {
	PauseInstancesForRemovedTrigger(ctx context.Context, triggerSlug string) error
}
