// Package memory holds the in-memory driven adapter for the registry
// service: the test-substitution Store.
package memory

import (
	"context"
	"sort"
	"sync"

	"beecon/internal/registryservice"
)

// Store is an in-memory registryservice.Store for tests.
type Store struct {
	mu        sync.RWMutex
	byVersion map[string]map[string]registryservice.StoredBundle
	latest    map[string]string
}

var _ registryservice.Store = (*Store)(nil)

func NewStore() *Store {
	return &Store{
		byVersion: map[string]map[string]registryservice.StoredBundle{},
		latest:    map[string]string{},
	}
}

func (s *Store) Save(_ context.Context, providerSlug string, stored registryservice.StoredBundle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byVersion[providerSlug] == nil {
		s.byVersion[providerSlug] = map[string]registryservice.StoredBundle{}
	}
	s.byVersion[providerSlug][stored.Bundle.Version] = stored
	s.latest[providerSlug] = stored.Bundle.Version
	return nil
}

func (s *Store) Find(_ context.Context, providerSlug, version string) (*registryservice.StoredBundle, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	versions, ok := s.byVersion[providerSlug]
	if !ok {
		return nil, nil
	}
	stored, ok := versions[version]
	if !ok {
		return nil, nil
	}
	copied := stored
	return &copied, nil
}

func (s *Store) LatestVersion(_ context.Context, providerSlug string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	version, ok := s.latest[providerSlug]
	return version, ok, nil
}

func (s *Store) ListVersions(_ context.Context, providerSlug string) ([]registryservice.StoredBundle, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	versions := s.byVersion[providerSlug]
	items := make([]registryservice.StoredBundle, 0, len(versions))
	for _, stored := range versions {
		items = append(items, stored)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].PublishedAt.Before(items[j].PublishedAt) })
	return items, nil
}
