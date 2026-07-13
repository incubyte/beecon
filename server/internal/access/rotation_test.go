// White-box (package access) tests for Slice 8 (PD23)'s pure rotation
// helpers in secret.go: resolving Rotate's optional overlapHours, the
// currently-active secret's lookup prefix, and the rotatedAt/overlapExpiresAt
// pair List derives from a key's secrets. ApiKeySecret.IsExpired's boundary
// (types.go) is covered here too since it is the other pure predicate the
// rotation feature depends on.
package access

import (
	"testing"
	"time"
)

func TestOverlapHoursOrDefault_FallsBackToDefaultOverlapHoursWhenNil(t *testing.T) {
	got := overlapHoursOrDefault(nil)

	if got != DefaultOverlapHours {
		t.Errorf("overlapHoursOrDefault(nil) = %d, want DefaultOverlapHours (%d)", got, DefaultOverlapHours)
	}
}

func TestOverlapHoursOrDefault_HonorsAnExplicitValue(t *testing.T) {
	custom := 6
	got := overlapHoursOrDefault(&custom)

	if got != custom {
		t.Errorf("overlapHoursOrDefault(&6) = %d, want %d", got, custom)
	}
}

func TestOverlapHoursOrDefault_HonorsAnExplicitZero(t *testing.T) {
	// Zero is a deliberate choice (no overlap at all), distinct from "not
	// given" — overlapHoursOrDefault must not treat *int(0) the same as nil.
	zero := 0
	got := overlapHoursOrDefault(&zero)

	if got != 0 {
		t.Errorf("overlapHoursOrDefault(&0) = %d, want 0", got)
	}
}

func TestApiKeySecretIsExpired_FalseWhenExpiresAtIsNil(t *testing.T) {
	secret := ApiKeySecret{ExpiresAt: nil}

	if secret.IsExpired(time.Now().Add(1000 * time.Hour)) {
		t.Error("a secret with no ExpiresAt (the currently active one) must never report expired")
	}
}

func TestApiKeySecretIsExpired_FalseWhenNowIsBeforeExpiresAt(t *testing.T) {
	expiresAt := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	secret := ApiKeySecret{ExpiresAt: &expiresAt}

	if secret.IsExpired(expiresAt.Add(-time.Nanosecond)) {
		t.Error("a secret one nanosecond before its ExpiresAt must not be expired")
	}
}

// TestApiKeySecretIsExpired_TrueExactlyAtExpiresAt pins the boundary the
// implementation actually chose (!now.Before(expiresAt), i.e. now >=
// expiresAt): the instant a secret's overlap window ends, it is already
// rejected — the window is a closed-at-the-end interval, not
// closed-open. AC3 ("after the overlap window ends... rejected") is
// satisfied by this choice; a test pinning the opposite boundary would be
// equally defensible reading the AC in isolation, so this test exists
// specifically to document which one the code implements.
func TestApiKeySecretIsExpired_TrueExactlyAtExpiresAt(t *testing.T) {
	expiresAt := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	secret := ApiKeySecret{ExpiresAt: &expiresAt}

	if !secret.IsExpired(expiresAt) {
		t.Error("a secret whose ExpiresAt exactly equals now must be expired (the implemented boundary is now >= expiresAt)")
	}
}

func TestApiKeySecretIsExpired_TrueWhenNowIsAfterExpiresAt(t *testing.T) {
	expiresAt := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	secret := ApiKeySecret{ExpiresAt: &expiresAt}

	if !secret.IsExpired(expiresAt.Add(time.Nanosecond)) {
		t.Error("a secret one nanosecond after its ExpiresAt must be expired")
	}
}

func TestActiveSecretPrefix_ReturnsEmptyStringForNoSecrets(t *testing.T) {
	got := activeSecretPrefix(nil)

	if got != "" {
		t.Errorf("activeSecretPrefix(nil) = %q, want empty string", got)
	}
}

func TestActiveSecretPrefix_ReturnsTheLastEntrysPrefixGivenOldestFirstOrder(t *testing.T) {
	secrets := []ApiKeySecret{
		{LookupPrefix: "beecon_sk_AA"},
		{LookupPrefix: "beecon_sk_BB"},
	}

	got := activeSecretPrefix(secrets)

	if got != "beecon_sk_BB" {
		t.Errorf("activeSecretPrefix = %q, want the newest (last) entry's prefix %q", got, "beecon_sk_BB")
	}
}

func TestRotationState_ReturnsNilNilForASingleSecret(t *testing.T) {
	secrets := []ApiKeySecret{{LookupPrefix: "beecon_sk_AA"}}

	rotatedAt, overlapExpiresAt := rotationState(secrets)

	if rotatedAt != nil || overlapExpiresAt != nil {
		t.Errorf("rotationState(1 secret) = (%v, %v), want (nil, nil) — a key is only \"rotated\" once a second secret exists", rotatedAt, overlapExpiresAt)
	}
}

func TestRotationState_ReturnsNilNilForNoSecrets(t *testing.T) {
	rotatedAt, overlapExpiresAt := rotationState(nil)

	if rotatedAt != nil || overlapExpiresAt != nil {
		t.Errorf("rotationState(nil) = (%v, %v), want (nil, nil)", rotatedAt, overlapExpiresAt)
	}
}

func TestRotationState_DerivesRotatedAtAndOverlapExpiresAtFromTheRetiredSecretsExpiry(t *testing.T) {
	rotatedAt := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	overlapExpiresAt := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	secrets := []ApiKeySecret{
		{LookupPrefix: "beecon_sk_AA", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ExpiresAt: &overlapExpiresAt},
		{LookupPrefix: "beecon_sk_BB", CreatedAt: rotatedAt},
	}

	gotRotatedAt, gotOverlapExpiresAt := rotationState(secrets)

	if gotRotatedAt == nil || !gotRotatedAt.Equal(rotatedAt) {
		t.Errorf("rotatedAt = %v, want the newest secret's CreatedAt %v", gotRotatedAt, rotatedAt)
	}
	if gotOverlapExpiresAt == nil || !gotOverlapExpiresAt.Equal(overlapExpiresAt) {
		t.Errorf("overlapExpiresAt = %v, want the retired secret's ExpiresAt %v", gotOverlapExpiresAt, overlapExpiresAt)
	}
}

// TestRotationState_AfterARapidDoubleRotationPairsRotatedAtWithTheImmediatelyPrecedingSecretsExpiry
// covers the 3-row shape a rapid second rotation leaves behind (verified
// end-to-end by TestRotate_ASecondRotationBeforeTheFirstOverlapWindowEndsImmediatelyRejectsTheOriginalSecret
// in rotate_test.go): the original secret's ExpiresAt is now a stale,
// already-past instant (expireLiveSecrets force-expired it to the moment the
// second Rotate call ran), while the once-rotated secret's ExpiresAt is the
// genuine, still-future end of the *current* overlap window — the one
// Rotate's own RotateResult reported to the caller at the second rotation.
// The verifier flagged that scanning secrets[:len-1] oldest-first and
// returning on the first non-nil ExpiresAt (the original naive
// implementation) mispairs the newest secret's CreatedAt with the stale,
// already-elapsed original secret's ExpiresAt instead of the secret
// immediately preceding the newest — which is always the one the most
// recent rotation actually retired, by construction of expireLiveSecrets'
// oldest-first append order. This test pins the CORRECT pairing.
func TestRotationState_AfterARapidDoubleRotationPairsRotatedAtWithTheImmediatelyPrecedingSecretsExpiry(t *testing.T) {
	originalIssuedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	firstRotatedAt := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	secondRotatedAt := time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC)
	staleForceExpiredAt := secondRotatedAt                        // original force-expired the instant the second rotation ran
	genuineOverlapExpiresAt := secondRotatedAt.Add(5 * time.Hour) // the once-rotated secret's real, still-future overlap end

	secrets := []ApiKeySecret{
		{LookupPrefix: "beecon_sk_AA", CreatedAt: originalIssuedAt, ExpiresAt: &staleForceExpiredAt},
		{LookupPrefix: "beecon_sk_BB", CreatedAt: firstRotatedAt, ExpiresAt: &genuineOverlapExpiresAt},
		{LookupPrefix: "beecon_sk_CC", CreatedAt: secondRotatedAt},
	}

	gotRotatedAt, gotOverlapExpiresAt := rotationState(secrets)

	if gotRotatedAt == nil || !gotRotatedAt.Equal(secondRotatedAt) {
		t.Fatalf("rotatedAt = %v, want the most recent rotation's timestamp %v", gotRotatedAt, secondRotatedAt)
	}
	if gotOverlapExpiresAt == nil || !gotOverlapExpiresAt.Equal(genuineOverlapExpiresAt) {
		t.Errorf("overlapExpiresAt = %v, want the immediately-preceding (once-rotated) secret's genuine overlap expiry %v — not the older, already-stale force-expired secret's ExpiresAt %v",
			gotOverlapExpiresAt, genuineOverlapExpiresAt, staleForceExpiredAt)
	}
}
