// Package registryservice (in-package test — needs direct access to the
// unexported contentHash helper for the determinism assertion below; every
// other assertion here goes through Facade's public API). Exercises Publish
// against the in-memory Store (Slice 1's walking skeleton): version
// assignment, tool_ id minting and its PD61 stability guarantee across
// republishing, and the content hash's shape/determinism.
package registryservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"beecon/internal/httpx"
	"beecon/internal/registrybundle"
)

func fixedClock() func() time.Time {
	return func() time.Time { return time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC) }
}

// sequentialToolIDs returns a deterministic tool_ id minter for assertions:
// tool_1, tool_2, ... in call order.
func sequentialToolIDs() func() string {
	n := 0
	return func() string {
		n++
		return "tool_" + string(rune('0'+n))
	}
}

func oneToolBundle(slug string) registrybundle.Bundle {
	return registrybundle.Bundle{
		FormatVersion: 1,
		Name:          "Outlook",
		BaseURL:       "https://graph.microsoft.com",
		Tools: []registrybundle.Tool{
			{
				Slug: slug, Name: "List messages",
				InputSchema:  map[string]any{"type": "object"},
				OutputSchema: map[string]any{"type": "object"},
				Sample:       map[string]any{"status": "ok"},
			},
		},
	}
}

// fakeStore is a minimal in-memory Store used only by this package's own
// in-package tests — declared locally (rather than importing driven/memory)
// since driven/memory itself imports this package, and Go forbids an
// internal test file completing that cycle.
type fakeStore struct {
	byVersion map[string]map[string]StoredBundle
	latest    map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{byVersion: map[string]map[string]StoredBundle{}, latest: map[string]string{}}
}

func (s *fakeStore) Save(_ context.Context, providerSlug string, stored StoredBundle) error {
	if s.byVersion[providerSlug] == nil {
		s.byVersion[providerSlug] = map[string]StoredBundle{}
	}
	s.byVersion[providerSlug][stored.Bundle.Version] = stored
	s.latest[providerSlug] = stored.Bundle.Version
	return nil
}

func (s *fakeStore) Find(_ context.Context, providerSlug, version string) (*StoredBundle, error) {
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

func (s *fakeStore) LatestVersion(_ context.Context, providerSlug string) (string, bool, error) {
	version, ok := s.latest[providerSlug]
	return version, ok, nil
}

func (s *fakeStore) ListVersions(_ context.Context, providerSlug string) ([]StoredBundle, error) {
	versions := s.byVersion[providerSlug]
	items := make([]StoredBundle, 0, len(versions))
	for _, stored := range versions {
		items = append(items, stored)
	}
	return items, nil
}

func memoryStoreForTest() Store {
	return newFakeStore()
}

func newFacadeForTest() *Facade {
	return NewFacade(memoryStoreForTest(), sequentialToolIDs(), fixedClock())
}

func TestPublish_AProvidersFirstBundleIsAssignedVersion1_0_0(t *testing.T) {
	f := newFacadeForTest()

	result, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages"))

	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if result.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", result.Version, "1.0.0")
	}
}

func TestPublish_MintsAToolPrefixedIDForEveryNewToolSlug(t *testing.T) {
	f := newFacadeForTest()

	result, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages"))

	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("Tools = %+v, want exactly 1", result.Tools)
	}
	if result.Tools[0].Slug != "outlook-list-messages" {
		t.Errorf("Tools[0].Slug = %q, want %q", result.Tools[0].Slug, "outlook-list-messages")
	}
	if result.Tools[0].ID == "" {
		t.Fatal("Tools[0].ID is empty, want a minted tool_ id")
	}
	if result.Tools[0].ID[:5] != "tool_" {
		t.Errorf("Tools[0].ID = %q, want it to start with %q", result.Tools[0].ID, "tool_")
	}
}

func TestPublish_RepublishingASeenSlugReusesItsExactPriorToolID(t *testing.T) {
	f := newFacadeForTest()
	first, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages"))
	if err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	// A second publish for the same provider, same tool slug, simulating a
	// later version (PD61: the tool_ id must not change just because the
	// bundle was republished).
	second, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages"))
	if err != nil {
		t.Fatalf("second Publish: %v", err)
	}

	if second.Version == first.Version {
		t.Fatalf("second Publish's version %q must differ from the first's %q for this assertion to mean anything", second.Version, first.Version)
	}
	if second.Tools[0].ID != first.Tools[0].ID {
		t.Errorf("tool_ id changed across republish: first=%q second=%q, want the same immutable id (PD61)", first.Tools[0].ID, second.Tools[0].ID)
	}
}

func TestPublish_ANewToolSlugInALaterVersionGetsItsOwnFreshID(t *testing.T) {
	f := newFacadeForTest()
	first, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages"))
	if err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	bundleWithNewTool := oneToolBundle("outlook-list-messages")
	bundleWithNewTool.Tools = append(bundleWithNewTool.Tools, registrybundle.Tool{
		Slug: "outlook-get-message", Name: "Get message",
		InputSchema:  map[string]any{"type": "object"},
		OutputSchema: map[string]any{"type": "object"},
		Sample:       map[string]any{"status": "ok"},
	})
	second, err := f.Publish(context.Background(), "outlook", bundleWithNewTool)
	if err != nil {
		t.Fatalf("second Publish: %v", err)
	}

	if len(second.Tools) != 2 {
		t.Fatalf("second Publish tools = %+v, want 2", second.Tools)
	}
	idsBySlug := map[string]string{}
	for _, tool := range second.Tools {
		idsBySlug[tool.Slug] = tool.ID
	}
	if idsBySlug["outlook-list-messages"] != first.Tools[0].ID {
		t.Errorf("previously-seen slug's id changed: got %q, want %q", idsBySlug["outlook-list-messages"], first.Tools[0].ID)
	}
	if idsBySlug["outlook-get-message"] == "" || idsBySlug["outlook-get-message"] == idsBySlug["outlook-list-messages"] {
		t.Errorf("new slug's id = %q, want a distinct fresh id from %q", idsBySlug["outlook-get-message"], idsBySlug["outlook-list-messages"])
	}
}

func TestPublish_ContentHashIsSHA256PrefixedAndDeterministicForByteIdenticalBundles(t *testing.T) {
	firstFacade := NewFacade(memoryStoreForTest(), sequentialToolIDs(), fixedClock())
	secondFacade := NewFacade(memoryStoreForTest(), sequentialToolIDs(), fixedClock())

	first, err := firstFacade.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages"))
	if err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	second, err := secondFacade.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages"))
	if err != nil {
		t.Fatalf("second Publish: %v", err)
	}

	if first.ContentHash == "" || second.ContentHash == "" {
		t.Fatalf("ContentHash must not be empty: first=%q second=%q", first.ContentHash, second.ContentHash)
	}
	if len(first.ContentHash) < 7 || first.ContentHash[:7] != "sha256:" {
		t.Errorf("ContentHash = %q, want it to start with %q", first.ContentHash, "sha256:")
	}
	if first.ContentHash != second.ContentHash {
		t.Errorf("two independent publishes of the byte-identical bundle produced different hashes: %q vs %q, want the same", first.ContentHash, second.ContentHash)
	}
}

func TestPublish_ContentHashDiffersWhenTheBundleContentDiffers(t *testing.T) {
	firstFacade := NewFacade(memoryStoreForTest(), sequentialToolIDs(), fixedClock())
	secondFacade := NewFacade(memoryStoreForTest(), sequentialToolIDs(), fixedClock())

	first, err := firstFacade.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages"))
	if err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	differentBundle := oneToolBundle("outlook-list-messages")
	differentBundle.Name = "Outlook (renamed)"
	second, err := secondFacade.Publish(context.Background(), "outlook", differentBundle)
	if err != nil {
		t.Fatalf("second Publish: %v", err)
	}

	if first.ContentHash == second.ContentHash {
		t.Errorf("two bundles with different content produced the same hash %q, want them to differ", first.ContentHash)
	}
}

func TestPublish_RejectsAnUnsupportedFormatVersion(t *testing.T) {
	f := newFacadeForTest()
	bundle := oneToolBundle("outlook-list-messages")
	bundle.FormatVersion = 2

	_, err := f.Publish(context.Background(), "outlook", bundle)

	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("Publish err = %T, want *httpx.DomainError", err)
	}
	if de.Status != 422 {
		t.Errorf("Status = %d, want 422", de.Status)
	}
	if de.Code != CodeValidationFailed {
		t.Errorf("Code = %q, want %q", de.Code, CodeValidationFailed)
	}
}
