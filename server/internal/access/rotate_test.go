// Package access_test (see facade_test.go for why this is an external test
// package, and its shared newFacade/orgA/orgB/assertDomainError helpers reused
// here). This file covers Slice 8/PD23's Rotate, its interaction with Verify
// and List, and the prefix-collision edge case across the
// server_api_key_secrets table introduced by migration 0012.
package access_test

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"beecon/internal/access"
	memory "beecon/internal/access/driven/memory"
	"beecon/internal/organizations"
)

// movableClock lets these facade-level tests travel time deterministically
// (past an overlap window's end) without a real sleep, the same role
// support.MovableClock plays for the integration journeys.
type movableClock struct{ now time.Time }

func newMovableClock(start time.Time) *movableClock { return &movableClock{now: start} }
func (c *movableClock) Now() time.Time              { return c.now }
func (c *movableClock) Advance(d time.Duration)     { c.now = c.now.Add(d) }

func newFacadeWithClock(clock *movableClock) *access.Facade {
	return memory.NewFacadeWithOverrides(memory.Overrides{Now: clock.Now})
}

func TestRotate_TheNewSecretAuthenticatesImmediately(t *testing.T) {
	clock := newMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := newFacadeWithClock(clock)
	issued, err := f.Issue(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rotated, err := f.Rotate(context.Background(), orgA, issued.ID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := f.Verify(context.Background(), rotated.Secret)
	if err != nil {
		t.Fatalf("the freshly rotated secret must authenticate immediately, got error: %v", err)
	}
	if got != orgA {
		t.Errorf("Verify(rotated secret) org = %q, want %q", got, orgA)
	}
}

func TestRotate_ReturnsANewSecretDifferentFromTheOriginalAndPrefixedCorrectly(t *testing.T) {
	clock := newMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := newFacadeWithClock(clock)
	issued, err := f.Issue(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rotated, err := f.Rotate(context.Background(), orgA, issued.ID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rotated.ID != issued.ID {
		t.Errorf("rotated.ID = %q, want the same key id %q", rotated.ID, issued.ID)
	}
	if rotated.Secret == issued.Secret {
		t.Fatal("Rotate returned the same secret Issue did — it must mint a fresh one")
	}
	if !strings.HasPrefix(rotated.Secret, access.SecretPrefix) {
		t.Errorf("rotated.Secret = %q, want it to start with %q", rotated.Secret, access.SecretPrefix)
	}
	if rotated.Prefix != rotated.Secret[:access.LookupPrefixLength] {
		t.Errorf("rotated.Prefix = %q, want the first %d chars of the new secret", rotated.Prefix, access.LookupPrefixLength)
	}
}

func TestRotate_TheOldSecretStillAuthenticatesInsideTheDefaultOverlapWindow(t *testing.T) {
	clock := newMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := newFacadeWithClock(clock)
	issued, err := f.Issue(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := f.Rotate(context.Background(), orgA, issued.ID, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clock.Advance(time.Duration(access.DefaultOverlapHours)*time.Hour - time.Nanosecond)

	got, err := f.Verify(context.Background(), issued.Secret)
	if err != nil {
		t.Fatalf("the old secret must still authenticate one nanosecond before the default %dh overlap window ends, got error: %v", access.DefaultOverlapHours, err)
	}
	if got != orgA {
		t.Errorf("Verify(old secret) org = %q, want %q", got, orgA)
	}
}

func TestRotate_TheOldSecretIsRejectedOnceTheDefaultOverlapWindowEnds(t *testing.T) {
	clock := newMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := newFacadeWithClock(clock)
	issued, err := f.Issue(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := f.Rotate(context.Background(), orgA, issued.ID, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clock.Advance(time.Duration(access.DefaultOverlapHours) * time.Hour)

	_, err = f.Verify(context.Background(), issued.Secret)
	assertDomainError(t, err, "unauthorized", 401)
}

func TestRotate_ACustomOverlapHoursOverridesTheDefaultAndIsHonoredByVerify(t *testing.T) {
	clock := newMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := newFacadeWithClock(clock)
	issued, err := f.Issue(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	customHours := 2
	rotated, err := f.Rotate(context.Background(), orgA, issued.ID, &customHours)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantExpiry := clock.Now().Add(2 * time.Hour)
	if !rotated.OverlapExpiresAt.Equal(wantExpiry) {
		t.Fatalf("OverlapExpiresAt = %v, want %v (the custom 2h overlap)", rotated.OverlapExpiresAt, wantExpiry)
	}

	clock.Advance(2*time.Hour - time.Nanosecond)
	if _, err := f.Verify(context.Background(), issued.Secret); err != nil {
		t.Fatalf("old secret must still authenticate just inside the custom 2h window, got: %v", err)
	}

	clock.Advance(time.Nanosecond) // now exactly at the 2h mark
	_, err = f.Verify(context.Background(), issued.Secret)
	assertDomainError(t, err, "unauthorized", 401)
}

func TestRotate_ReturnsNotFoundForAnUnknownKeyID(t *testing.T) {
	f := newFacade()

	_, err := f.Rotate(context.Background(), orgA, access.KeyID("key_missing"), nil)

	assertDomainError(t, err, access.CodeNotFound, 404)
}

// TestRotate_ReturnsValidationErrorForANegativeOverlapHours pins the facade's
// guard rejecting a negative overlapHours outright (a negative overlap
// window is meaningless — it would schedule an expiry before the retiring
// secret was even created).
func TestRotate_ReturnsValidationErrorForANegativeOverlapHours(t *testing.T) {
	f := newFacade()
	issued, err := f.Issue(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	negative := -1

	_, err = f.Rotate(context.Background(), orgA, issued.ID, &negative)

	assertDomainError(t, err, access.CodeValidationFailed, 422)
}

func TestRotate_ReturnsNotFoundForAKeyBelongingToAnotherOrg(t *testing.T) {
	f := newFacade()
	issuedToA, err := f.Issue(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = f.Rotate(context.Background(), orgB, issuedToA.ID, nil)

	assertDomainError(t, err, access.CodeNotFound, 404)
}

// TestRotate_OnAnAlreadyRevokedKeyIsRejectedAsNotFound pins Rotate's explicit
// key.IsRevoked() guard: a revoked key has nothing left to rotate into
// working order, so Rotate refuses outright (not-found, matching Revoke's
// and every other cross-org lookup's PD5 shape) rather than silently minting
// a secret that can never authenticate.
func TestRotate_OnAnAlreadyRevokedKeyIsRejectedAsNotFound(t *testing.T) {
	f := newFacade()
	issued, err := f.Issue(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := f.Revoke(context.Background(), orgA, issued.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = f.Rotate(context.Background(), orgA, issued.ID, nil)

	assertDomainError(t, err, access.CodeNotFound, 404)
}

// TestRotate_ASecondRotationBeforeTheFirstOverlapWindowEndsImmediatelyRejectsTheOriginalSecret
// pins the "at most two live secrets" invariant types.go's ApiKeySecret doc
// comment describes: a second rotation force-expires any secret still live
// from an earlier rotation (not just the currently active one), so the
// original secret is rejected the moment the second rotation runs, even
// though its own first-rotation overlap window hasn't lapsed yet. Only the
// once-rotated and freshly-rotated-twice secrets keep authenticating.
func TestRotate_ASecondRotationBeforeTheFirstOverlapWindowEndsImmediatelyRejectsTheOriginalSecret(t *testing.T) {
	clock := newMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := newFacadeWithClock(clock)
	issued, err := f.Issue(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	firstOverlap := 2
	firstRotation, err := f.Rotate(context.Background(), orgA, issued.ID, &firstOverlap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clock.Advance(1 * time.Hour) // still inside the first rotation's 2h window

	secondOverlap := 5
	secondRotation, err := f.Rotate(context.Background(), orgA, issued.ID, &secondOverlap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := f.Verify(context.Background(), issued.Secret); err == nil {
		t.Error("the original secret authenticated after a second rotation force-expired it — at most two secrets must be live at once")
	}
	for name, secret := range map[string]string{
		"once-rotated":            firstRotation.Secret,
		"freshly rotated (twice)": secondRotation.Secret,
	} {
		if _, err := f.Verify(context.Background(), secret); err != nil {
			t.Errorf("%s secret failed to authenticate right after the second rotation: %v", name, err)
		}
	}
}

func TestList_ShowsNoRotationStateBeforeAnyRotation(t *testing.T) {
	f := newFacade()
	issued, err := f.Issue(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	keys, err := f.List(context.Background(), orgA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	if keys[0].RotatedAt != nil {
		t.Errorf("RotatedAt = %v, want nil before any rotation", keys[0].RotatedAt)
	}
	if keys[0].OverlapExpiresAt != nil {
		t.Errorf("OverlapExpiresAt = %v, want nil before any rotation", keys[0].OverlapExpiresAt)
	}
	if keys[0].Prefix != issued.Prefix {
		t.Errorf("Prefix = %q, want %q", keys[0].Prefix, issued.Prefix)
	}
}

func TestList_ShowsRotatedAtAndOverlapExpiresAtAfterARotationAndNeverAnySecret(t *testing.T) {
	clock := newMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := newFacadeWithClock(clock)
	issued, err := f.Issue(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	clock.Advance(1 * time.Hour) // distinct CreatedAt from Issue's secret, so the in-memory repo's map iteration order can never make the wrong one "active"
	rotatedAtTime := clock.Now()
	rotated, err := f.Rotate(context.Background(), orgA, issued.ID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	keys, err := f.List(context.Background(), orgA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	listing := keys[0]
	if listing.RotatedAt == nil || !listing.RotatedAt.Equal(rotatedAtTime) {
		t.Errorf("RotatedAt = %v, want %v", listing.RotatedAt, rotatedAtTime)
	}
	if listing.OverlapExpiresAt == nil || !listing.OverlapExpiresAt.Equal(rotated.OverlapExpiresAt) {
		t.Errorf("OverlapExpiresAt = %v, want %v", listing.OverlapExpiresAt, rotated.OverlapExpiresAt)
	}
	if listing.Prefix != rotated.Prefix {
		t.Errorf("Prefix = %q, want the newly rotated secret's prefix %q", listing.Prefix, rotated.Prefix)
	}
	// KeyListing carries no field capable of holding a secret at all — this
	// assertion documents that structural guarantee rather than serializing
	// and grepping, which duplicates the HTTP-layer test's job.
	if listing.Prefix == rotated.Secret {
		t.Fatal("test fixture bug: listing.Prefix unexpectedly equals the full secret")
	}
}

func TestRevoke_AfterARotationImmediatelyRejectsBothTheOldAndNewSecret(t *testing.T) {
	f := newFacade()
	issued, err := f.Issue(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rotated, err := f.Rotate(context.Background(), orgA, issued.ID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Sanity: both secrets verify before revocation (the overlap window is
	// the default 24h and no clock has moved, so the old one is still live).
	if _, err := f.Verify(context.Background(), issued.Secret); err != nil {
		t.Fatalf("expected the old secret to verify before revocation, got: %v", err)
	}
	if _, err := f.Verify(context.Background(), rotated.Secret); err != nil {
		t.Fatalf("expected the new secret to verify before revocation, got: %v", err)
	}

	if err := f.Revoke(context.Background(), orgA, issued.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = f.Verify(context.Background(), issued.Secret)
	assertDomainError(t, err, "unauthorized", 401)
	_, err = f.Verify(context.Background(), rotated.Secret)
	assertDomainError(t, err, "unauthorized", 401)
}

// TestVerify_PicksTheCorrectSecretByHashAcrossTheSecretsTableWhenTwoDifferentKeysCollideAndOneHasBeenRotated
// extends verify_prefix_collision_test.go's cross-key collision coverage
// (which uses a hand-rolled fixture to sidestep an import cycle) to the real
// driven/memory Repository/PrefixLookup/ApiKeySecrets adapter migration 0012
// introduced, and to a secrets table that now holds more than one row per
// key after a rotation. Two keys' secrets are seeded to genuinely share a
// lookup prefix (same hashing scheme Issue/Rotate use: SHA-256 of the
// remainder after LookupPrefixLength), one of the two keys is then rotated
// through the real facade, and Verify must still resolve every secret —
// old, new, and the untouched colliding one — to the correct organization
// purely by hash.
func TestVerify_PicksTheCorrectSecretByHashAcrossTheSecretsTableWhenTwoDifferentKeysCollideAndOneHasBeenRotated(t *testing.T) {
	const sharedPrefix = "beecon_sk_AA" // exactly LookupPrefixLength (12) chars.
	secretA := sharedPrefix + "-secret-belonging-to-org-a"
	secretB := sharedPrefix + "-secret-belonging-to-org-b"

	repo := memory.NewRepository()
	f := memory.NewFacadeWithOverrides(memory.Overrides{Repository: repo, PrefixLookup: repo, ApiKeySecrets: repo})
	seedKeyWithSecret(t, repo, "key_a", orgA, secretA)
	seedKeyWithSecret(t, repo, "key_b", orgB, secretB)

	rotated, err := f.Rotate(context.Background(), orgA, "key_a", nil)
	if err != nil {
		t.Fatalf("unexpected error rotating key_a: %v", err)
	}

	for name, tc := range map[string]struct {
		secret  string
		wantOrg organizations.OrgID
	}{
		"org A's pre-rotation secret (still live inside the overlap window)": {secretA, orgA},
		"org A's freshly rotated secret":                                     {rotated.Secret, orgA},
		"org B's untouched, unrelated secret":                                {secretB, orgB},
	} {
		got, err := f.Verify(context.Background(), tc.secret)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
			continue
		}
		if got != tc.wantOrg {
			t.Errorf("%s: Verify() org = %q, want %q", name, got, tc.wantOrg)
		}
	}

	if _, err := f.Verify(context.Background(), sharedPrefix+"-never-issued-to-anyone"); err == nil {
		t.Error("a third secret merely sharing the lookup prefix, never actually issued, must not authenticate")
	}
}

// seedKeyWithSecret writes a key and its one secret directly to repo using
// the production hashing scheme (mirrored here with crypto/sha256 since this
// external test package cannot reach access's unexported
// hashSecretRemainder), so secret is genuinely verifiable — not just
// prefix-matching — the same fixture-construction approach
// key_secret_never_persisted_integration_test.go uses to check the stored
// hash from the other direction.
func seedKeyWithSecret(t *testing.T, repo *memory.Repository, id access.KeyID, org organizations.OrgID, secret string) {
	t.Helper()
	if err := repo.SaveKey(context.Background(), access.ServerApiKey{ID: id, OrgID: org, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("seed key %q: %v", id, err)
	}
	hash := sha256.Sum256([]byte(secret[access.LookupPrefixLength:]))
	err := repo.Save(context.Background(), org, access.ApiKeySecret{
		ID:           access.ApiKeySecretID(string(id) + "_secret"),
		KeyID:        id,
		LookupPrefix: secret[:access.LookupPrefixLength],
		SecretHash:   hash[:],
		CreatedAt:    time.Now(),
	})
	if err != nil {
		t.Fatalf("seed secret for key %q: %v", id, err)
	}
}
