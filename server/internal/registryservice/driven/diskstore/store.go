// Package diskstore is registryservice.Store's filesystem adapter (PD60:
// git-backed storage behind a port). This is the concrete first adapter —
// one JSON file per published bundle version under a directory, plus a
// small per-provider "latest version" pointer file. A real git-backed
// adapter (git init once, git add + git commit per publish, so publish
// governance can ride on git permissions as PD60 intends) is a later
// hardening step behind this same Store interface; nothing about the port
// signature needs to change for it.
package diskstore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"beecon/internal/registryservice"
)

const latestVersionFileName = "latest.txt"

// Store is registryservice.Store's filesystem adapter.
type Store struct {
	dir string
}

var _ registryservice.Store = (*Store)(nil)

// NewStore creates dir (and any missing parents) if needed and returns a
// Store rooted there.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Save(_ context.Context, providerSlug string, stored registryservice.StoredBundle) error {
	providerDir := filepath.Join(s.dir, providerSlug)
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		return err
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	versionFile := filepath.Join(providerDir, stored.Bundle.Version+".json")
	if err := os.WriteFile(versionFile, encoded, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(providerDir, latestVersionFileName), []byte(stored.Bundle.Version), 0o644)
}

func (s *Store) Find(_ context.Context, providerSlug, version string) (*registryservice.StoredBundle, error) {
	raw, err := os.ReadFile(filepath.Join(s.dir, providerSlug, version+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var stored registryservice.StoredBundle
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, err
	}
	return &stored, nil
}

func (s *Store) LatestVersion(_ context.Context, providerSlug string) (string, bool, error) {
	raw, err := os.ReadFile(filepath.Join(s.dir, providerSlug, latestVersionFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(raw), true, nil
}

// ListVersions returns every version file stored under providerSlug's
// directory (Slice 3): an empty slice, not an error, when the provider has
// never published (no directory on disk yet).
func (s *Store) ListVersions(_ context.Context, providerSlug string) ([]registryservice.StoredBundle, error) {
	providerDir := filepath.Join(s.dir, providerSlug)
	entries, err := os.ReadDir(providerDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	items := make([]registryservice.StoredBundle, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(providerDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var stored registryservice.StoredBundle
		if err := json.Unmarshal(raw, &stored); err != nil {
			return nil, err
		}
		items = append(items, stored)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].PublishedAt.Before(items[j].PublishedAt) })
	return items, nil
}
