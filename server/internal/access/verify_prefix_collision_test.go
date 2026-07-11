// White-box (package access): a lookup prefix carries only
// LookupPrefixLength (12) characters, and the fixed literal SecretPrefix
// itself eats 10 of those — only 2 random characters actually narrow the
// lookup, so two issued keys can genuinely share a lookup prefix. Verify must
// still pick the correct key by comparing secret hashes, not just prefixes.
// Engineering a real collision through Issue's crypto/rand secret generator
// isn't practical (astronomically unlikely), so this test builds the
// fixture directly with the package's own lookupPrefix/hashSecretRemainder
// helpers — the same ones Verify uses — rather than re-implementing the
// hashing scheme. A local fake Repository/PrefixLookup is defined here
// (rather than importing driven/memory) because driven/memory imports
// access, and importing it from an in-package test file would be an import
// cycle.
package access

import (
	"context"
	"errors"
	"testing"
	"time"

	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

type collisionFixtureRepo struct {
	keys []ServerApiKey
}

func (r *collisionFixtureRepo) Save(_ context.Context, key ServerApiKey) error {
	r.keys = append(r.keys, key)
	return nil
}

func (r *collisionFixtureRepo) ListByOrg(context.Context, organizations.OrgID) ([]ServerApiKey, error) {
	return nil, nil
}

func (r *collisionFixtureRepo) FindByID(context.Context, organizations.OrgID, KeyID) (*ServerApiKey, error) {
	return nil, nil
}

func (r *collisionFixtureRepo) MarkRevoked(context.Context, organizations.OrgID, KeyID, time.Time) error {
	return nil
}

func (r *collisionFixtureRepo) FindByPrefix(_ context.Context, prefix string) ([]ServerApiKey, error) {
	matches := make([]ServerApiKey, 0)
	for _, k := range r.keys {
		if k.LookupPrefix == prefix {
			matches = append(matches, k)
		}
	}
	return matches, nil
}

func newKeyWithSecret(id KeyID, org organizations.OrgID, secret string) ServerApiKey {
	return ServerApiKey{
		ID:           id,
		OrgID:        org,
		LookupPrefix: lookupPrefix(secret),
		SecretHash:   hashSecretRemainder(secret),
	}
}

func TestVerify_PicksTheCorrectOrgByHashWhenTwoKeysShareALookupPrefix(t *testing.T) {
	const sharedPrefix = "beecon_sk_AA" // exactly LookupPrefixLength (12) chars.
	secretA := sharedPrefix + "-secret-belonging-to-org-a"
	secretB := sharedPrefix + "-secret-belonging-to-org-b"

	repo := &collisionFixtureRepo{}
	repo.keys = []ServerApiKey{
		newKeyWithSecret("key_a", "org_a", secretA),
		newKeyWithSecret("key_b", "org_b", secretB),
	}
	if repo.keys[0].LookupPrefix != repo.keys[1].LookupPrefix {
		t.Fatalf("test fixture bug: keys do not actually share a lookup prefix (%q vs %q)", repo.keys[0].LookupPrefix, repo.keys[1].LookupPrefix)
	}
	facade := NewFacade(repo, repo, func() string { return "unused" }, func() time.Time { return time.Time{} })

	gotOrgForA, err := facade.Verify(context.Background(), secretA)
	if err != nil {
		t.Fatalf("Verify(secretA) unexpected error: %v", err)
	}
	if gotOrgForA != "org_a" {
		t.Errorf("Verify(secretA) org = %q, want %q", gotOrgForA, "org_a")
	}

	gotOrgForB, err := facade.Verify(context.Background(), secretB)
	if err != nil {
		t.Fatalf("Verify(secretB) unexpected error: %v", err)
	}
	if gotOrgForB != "org_b" {
		t.Errorf("Verify(secretB) org = %q, want %q", gotOrgForB, "org_b")
	}
}

func TestVerify_RejectsAThirdSecretSharingTheLookupPrefixOfTwoOtherKeys(t *testing.T) {
	const sharedPrefix = "beecon_sk_AA"
	secretA := sharedPrefix + "-secret-belonging-to-org-a"
	secretB := sharedPrefix + "-secret-belonging-to-org-b"
	secretC := sharedPrefix + "-secret-never-issued-to-anyone"

	repo := &collisionFixtureRepo{}
	repo.keys = []ServerApiKey{
		newKeyWithSecret("key_a", "org_a", secretA),
		newKeyWithSecret("key_b", "org_b", secretB),
	}
	facade := NewFacade(repo, repo, func() string { return "unused" }, func() time.Time { return time.Time{} })

	_, err := facade.Verify(context.Background(), secretC)

	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *httpx.DomainError, got %T: %v", err, err)
	}
	if de.Status != 401 || de.Code != "unauthorized" {
		t.Errorf("error = %+v, want 401 unauthorized", de)
	}
}
