// list_versions_test.go (package registryservice, same package as
// publish_test.go — reuses newFacadeForTest/oneToolBundle/fixedClock).
// Exercises Facade.ListVersions (Slice 3's review-before-adopting flow,
// registry side): what an installation operator sees before pulling or
// activating any particular version.
package registryservice

import (
	"context"
	"testing"
)

func TestListVersions_AnUnpublishedProviderReturnsNoVersions(t *testing.T) {
	f := newFacadeForTest()

	versions, err := f.ListVersions(context.Background(), "never-published")

	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("versions = %+v, want empty for a provider that has never published", versions)
	}
}

func TestListVersions_APublishedProviderReturnsEveryVersionWithItsContentHash(t *testing.T) {
	f := newFacadeForTest()
	first, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages"))
	if err != nil {
		t.Fatalf("Publish (1.0.0): %v", err)
	}
	secondBundle := oneToolBundle("outlook-list-messages")
	secondBundle.Tools = append(secondBundle.Tools, oneToolBundle("outlook-get-message").Tools...)
	second, err := f.Publish(context.Background(), "outlook", secondBundle)
	if err != nil {
		t.Fatalf("Publish (1.1.0): %v", err)
	}

	versions, err := f.ListVersions(context.Background(), "outlook")

	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("versions = %+v, want exactly the 2 published versions", versions)
	}
	byVersion := map[string]BundleVersionSummary{}
	for _, v := range versions {
		byVersion[v.Version] = v
	}
	firstSummary, ok := byVersion[first.Version]
	if !ok {
		t.Fatalf("versions %+v missing the first published version %q", versions, first.Version)
	}
	if firstSummary.ContentHash != first.ContentHash {
		t.Errorf("first version's ContentHash = %q, want %q", firstSummary.ContentHash, first.ContentHash)
	}
	secondSummary, ok := byVersion[second.Version]
	if !ok {
		t.Fatalf("versions %+v missing the second published version %q", versions, second.Version)
	}
	if secondSummary.ContentHash != second.ContentHash {
		t.Errorf("second version's ContentHash = %q, want %q", secondSummary.ContentHash, second.ContentHash)
	}
	if firstSummary.ContentHash == secondSummary.ContentHash {
		t.Errorf("the two published versions must not share the same content hash: %q", firstSummary.ContentHash)
	}
}
