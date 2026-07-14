// Package access_test (see facade_test.go for why this is an external test
// package, and its shared orgA/orgB/assertDomainError helpers reused here).
// This file mirrors rotate_test.go's coverage of ApiKeySecret rotation
// (PD23), but for WebhookSigningSecret (PD27/PD31): issue-once, ≤2 live
// secrets at any time, the default and a custom overlap window, clock
// travel past the window, and a rapid second rotation before the first
// window ends.
package access_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"beecon/internal/access"
)

// endpointA is a representative endpoint id: WebhookSigningSecret is scoped
// to (org, endpoint) since Slice 8's per-endpoint secrets (PD45); these
// tests exercise the org-scoping dimension, so every secret in this file
// belongs to the same endpoint unless a test says otherwise.
const endpointA = "wep_a"

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestIssueWebhookSecret_ReturnsAWhsPrefixedIDAndAWhsecPrefixedSecret(t *testing.T) {
	f := newFacade()

	issued, err := f.IssueWebhookSecret(context.Background(), orgA, endpointA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(string(issued.ID), "whs_") {
		t.Errorf("id = %q, want it to start with %q", issued.ID, "whs_")
	}
	if !strings.HasPrefix(issued.Secret, access.WebhookSecretPrefix) {
		t.Errorf("secret = %q, want it to start with %q", issued.Secret, access.WebhookSecretPrefix)
	}
	if issued.Prefix != issued.Secret[:access.WebhookSecretDisplayPrefixLength] {
		t.Errorf("prefix = %q, want the first %d chars of the secret", issued.Prefix, access.WebhookSecretDisplayPrefixLength)
	}
}

func TestActiveWebhookSecrets_ReturnsTheIssuedSecretDecrypted(t *testing.T) {
	f := newFacade()
	issued, err := f.IssueWebhookSecret(context.Background(), orgA, endpointA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	active, err := f.ActiveWebhookSecrets(context.Background(), orgA, endpointA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(active) != 1 || active[0] != issued.Secret {
		t.Fatalf("active = %v, want exactly [%q]", active, issued.Secret)
	}
}

func TestActiveWebhookSecrets_ReturnsEmptyForAnOrgWithNoSecret(t *testing.T) {
	f := newFacade()

	active, err := f.ActiveWebhookSecrets(context.Background(), orgA, endpointA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("active = %v, want empty", active)
	}
}

func TestActiveWebhookSecrets_NeverLeaksAnotherOrganizationsSecret(t *testing.T) {
	f := newFacade()
	if _, err := f.IssueWebhookSecret(context.Background(), orgA, endpointA); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	active, err := f.ActiveWebhookSecrets(context.Background(), orgB, endpointA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("active = %v, want empty — org B has no secret of its own", active)
	}
}

func TestWebhookSecretPrefix_ReturnsEmptyForAnOrgWithNoSecret(t *testing.T) {
	f := newFacade()

	prefix, err := f.WebhookSecretPrefix(context.Background(), orgA, endpointA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prefix != "" {
		t.Errorf("prefix = %q, want empty", prefix)
	}
}

func TestRotateWebhookSecret_TheNewSecretIsActiveImmediately(t *testing.T) {
	clock := newMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := newFacadeWithClock(clock)
	if _, err := f.IssueWebhookSecret(context.Background(), orgA, endpointA); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rotated, err := f.RotateWebhookSecret(context.Background(), orgA, endpointA, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	active, err := f.ActiveWebhookSecrets(context.Background(), orgA, endpointA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsString(active, rotated.Secret) {
		t.Errorf("active = %v, want it to contain the freshly rotated secret %q", active, rotated.Secret)
	}
}

// TestRotateWebhookSecret_TheOldSecretStaysActiveInsideTheDefaultOverlapWindow
// pins PD31's rotation-overlap shape (mirroring PD23): both the outgoing and
// the freshly rotated secret must verify — DispatchOnce signs with both, one
// v1 value per secret.
func TestRotateWebhookSecret_TheOldSecretStaysActiveInsideTheDefaultOverlapWindow(t *testing.T) {
	clock := newMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := newFacadeWithClock(clock)
	issued, err := f.IssueWebhookSecret(context.Background(), orgA, endpointA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rotated, err := f.RotateWebhookSecret(context.Background(), orgA, endpointA, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clock.Advance(time.Duration(access.DefaultOverlapHours)*time.Hour - time.Nanosecond)

	active, err := f.ActiveWebhookSecrets(context.Background(), orgA, endpointA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("active = %v, want exactly 2 (outgoing + fresh) inside the overlap window", active)
	}
	if !containsString(active, issued.Secret) || !containsString(active, rotated.Secret) {
		t.Errorf("active = %v, want both %q and %q", active, issued.Secret, rotated.Secret)
	}
}

func TestRotateWebhookSecret_TheOldSecretDropsOutAfterTheDefaultOverlapWindowEnds(t *testing.T) {
	clock := newMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := newFacadeWithClock(clock)
	issued, err := f.IssueWebhookSecret(context.Background(), orgA, endpointA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rotated, err := f.RotateWebhookSecret(context.Background(), orgA, endpointA, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clock.Advance(time.Duration(access.DefaultOverlapHours) * time.Hour)

	active, err := f.ActiveWebhookSecrets(context.Background(), orgA, endpointA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(active) != 1 || active[0] != rotated.Secret {
		t.Fatalf("active = %v, want exactly [%q] — the old secret's overlap window has ended", active, rotated.Secret)
	}
	if containsString(active, issued.Secret) {
		t.Error("the old secret is still active past its overlap window")
	}
}

func TestRotateWebhookSecret_ACustomOverlapHoursOverridesTheDefault(t *testing.T) {
	clock := newMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := newFacadeWithClock(clock)
	issued, err := f.IssueWebhookSecret(context.Background(), orgA, endpointA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	customHours := 2
	rotated, err := f.RotateWebhookSecret(context.Background(), orgA, endpointA, &customHours)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantExpiry := clock.Now().Add(2 * time.Hour)
	if !rotated.OverlapExpiresAt.Equal(wantExpiry) {
		t.Fatalf("OverlapExpiresAt = %v, want %v", rotated.OverlapExpiresAt, wantExpiry)
	}

	clock.Advance(2*time.Hour - time.Nanosecond)
	active, err := f.ActiveWebhookSecrets(context.Background(), orgA, endpointA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsString(active, issued.Secret) {
		t.Error("the old secret must still be active just inside the custom 2h window")
	}

	clock.Advance(time.Nanosecond) // now exactly at the 2h mark
	active, err = f.ActiveWebhookSecrets(context.Background(), orgA, endpointA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(active, issued.Secret) {
		t.Error("the old secret must be rejected exactly at the custom window's end")
	}
}

// TestRotateWebhookSecret_ASecondRotationBeforeTheFirstOverlapEndsLeavesAtMostTwoLiveSecrets
// mirrors rotate_test.go's ApiKeySecret coverage of the same invariant: a
// second rotation force-expires any secret still live from an earlier
// rotation, so the original never accumulates a third live secret.
func TestRotateWebhookSecret_ASecondRotationBeforeTheFirstOverlapEndsLeavesAtMostTwoLiveSecrets(t *testing.T) {
	clock := newMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := newFacadeWithClock(clock)
	issued, err := f.IssueWebhookSecret(context.Background(), orgA, endpointA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	firstOverlap := 2
	firstRotation, err := f.RotateWebhookSecret(context.Background(), orgA, endpointA, &firstOverlap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clock.Advance(1 * time.Hour) // still inside the first rotation's 2h window

	secondOverlap := 5
	secondRotation, err := f.RotateWebhookSecret(context.Background(), orgA, endpointA, &secondOverlap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	active, err := f.ActiveWebhookSecrets(context.Background(), orgA, endpointA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("active = %v, want exactly 2 (at most two secrets are ever live at once)", active)
	}
	if containsString(active, issued.Secret) {
		t.Error("the original secret is still active after a second rotation force-expired it")
	}
	if !containsString(active, firstRotation.Secret) || !containsString(active, secondRotation.Secret) {
		t.Errorf("active = %v, want the once-rotated and freshly-rotated-twice secrets", active)
	}
}

func TestRotateWebhookSecret_ReturnsValidationErrorForANegativeOverlapHours(t *testing.T) {
	f := newFacade()
	if _, err := f.IssueWebhookSecret(context.Background(), orgA, endpointA); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	negative := -1

	_, err := f.RotateWebhookSecret(context.Background(), orgA, endpointA, &negative)

	assertDomainError(t, err, access.CodeValidationFailed, 422)
}

func TestRotateWebhookSecret_OnAnOrgWithNoExistingSecretStillMintsAFirstOne(t *testing.T) {
	// RotateWebhookSecret itself has no "must already have a secret" guard —
	// delivery.Facade.RotateSecret is the layer that rejects "no endpoint
	// configured" (ErrNoEndpoint); this pins that access's own primitive is
	// simply "expire whatever's live (nothing), then mint fresh."
	f := newFacade()

	rotated, err := f.RotateWebhookSecret(context.Background(), orgA, endpointA, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rotated.Secret == "" {
		t.Error("expected a freshly minted secret")
	}
}

// --- WebhookSigningSecret.IsExpired (types.go) — mirrors
// rotation_test.go's ApiKeySecret.IsExpired boundary tests exactly. ---

func TestWebhookSigningSecretIsExpired_FalseWhenExpiresAtIsNil(t *testing.T) {
	secret := access.WebhookSigningSecret{ExpiresAt: nil}

	if secret.IsExpired(time.Now().Add(1000 * time.Hour)) {
		t.Error("a secret with no ExpiresAt (the currently active one) must never report expired")
	}
}

func TestWebhookSigningSecretIsExpired_TrueExactlyAtExpiresAt(t *testing.T) {
	expiresAt := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	secret := access.WebhookSigningSecret{ExpiresAt: &expiresAt}

	if !secret.IsExpired(expiresAt) {
		t.Error("a secret whose ExpiresAt exactly equals now must be expired (now >= expiresAt)")
	}
}

func TestWebhookSigningSecretIsExpired_FalseOneNanosecondBeforeExpiresAt(t *testing.T) {
	expiresAt := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	secret := access.WebhookSigningSecret{ExpiresAt: &expiresAt}

	if secret.IsExpired(expiresAt.Add(-time.Nanosecond)) {
		t.Error("a secret one nanosecond before its ExpiresAt must not be expired")
	}
}
