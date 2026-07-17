package registryservice

import "context"

// Store is the registry service's driven persistence port (PD60): one
// provider's published bundle versions. Save persists (or overwrites) one
// version; Find returns (nil, nil) on a miss; LatestVersion reports the
// most recently published version for providerSlug, and false when none has
// ever been published — Publish uses it both to assign "1.0.0" to a
// provider's first bundle (Slice 1 AC) and to look up the prior version's
// tool_ id for a slug it has already seen (PD61: stable across versions).
// ListVersions returns every version providerSlug has published (Slice 3),
// an empty slice (not an error) for a provider that has never published.
type Store interface {
	Save(ctx context.Context, providerSlug string, stored StoredBundle) error
	Find(ctx context.Context, providerSlug, version string) (*StoredBundle, error)
	LatestVersion(ctx context.Context, providerSlug string) (string, bool, error)
	ListVersions(ctx context.Context, providerSlug string) ([]StoredBundle, error)
}
