package memory

import (
	"context"
	"sort"
	"sync"

	"beecon/internal/catalog"
	"beecon/internal/registrybundle"
)

// RegistryClient is an in-memory catalog.RegistryClient for tests (PD64):
// stands in for a real registry HTTP server. Seed registers what
// FetchBundle returns for a given provider/version, the same
// "seed-then-exercise" convention every other in-memory fake in this
// codebase already follows.
type RegistryClient struct {
	mu      sync.RWMutex
	bundles map[string]map[string]registrybundle.Bundle
}

var _ catalog.RegistryClient = (*RegistryClient)(nil)

func NewRegistryClient() *RegistryClient {
	return &RegistryClient{bundles: map[string]map[string]registrybundle.Bundle{}}
}

// Seed registers bundle as providerSlug's bundle at bundle.Version, so a
// test can drive Facade.Activate without standing up a real registry HTTP
// server.
func (c *RegistryClient) Seed(providerSlug string, bundle registrybundle.Bundle) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bundles[providerSlug] == nil {
		c.bundles[providerSlug] = map[string]registrybundle.Bundle{}
	}
	c.bundles[providerSlug][bundle.Version] = bundle
}

func (c *RegistryClient) FetchBundle(_ context.Context, providerSlug, version string) (registrybundle.Bundle, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	versions, ok := c.bundles[providerSlug]
	if !ok {
		return registrybundle.Bundle{}, catalog.ErrBundleVersionNotFound()
	}
	bundle, ok := versions[version]
	if !ok {
		return registrybundle.Bundle{}, catalog.ErrBundleVersionNotFound()
	}
	return bundle, nil
}

// ListVersions returns every version Seed has registered for providerSlug,
// sorted, so a test can drive Facade.ListRegistryVersions/DiffRegistryVersion
// without standing up a real registry HTTP server (Slice 3).
func (c *RegistryClient) ListVersions(_ context.Context, providerSlug string) ([]string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	versions := c.bundles[providerSlug]
	items := make([]string, 0, len(versions))
	for version := range versions {
		items = append(items, version)
	}
	sort.Strings(items)
	return items, nil
}
