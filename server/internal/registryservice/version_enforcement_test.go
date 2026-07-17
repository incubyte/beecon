// Package registryservice (in-package, reusing publish_test.go's
// newFacadeForTest/oneToolBundle harness). Exercises Slice 2's semver rules
// (PD62): a caller-supplied version is validated (conflict, strictly-greater,
// bump-direction), an omitted version is auto-computed instead, and the
// publish response reports what changed relative to the provider's previous
// version.
package registryservice

import (
	"context"
	"errors"
	"testing"

	"beecon/internal/httpx"
	"beecon/internal/registrybundle"
)

// twoToolBundle is oneToolBundle plus a second, distinctly-slugged tool —
// used by the addition-side of the bump-direction matrix.
func twoToolBundle(firstSlug, secondSlug string) registrybundle.Bundle {
	bundle := oneToolBundle(firstSlug)
	bundle.Tools = append(bundle.Tools, registrybundle.Tool{
		Slug:         secondSlug,
		Name:         "Second tool",
		InputSchema:  map[string]any{"type": "object"},
		OutputSchema: map[string]any{"type": "object"},
		Sample:       map[string]any{"status": "ok"},
	})
	return bundle
}

// noToolBundle is oneToolBundle with its tool removed — used by the
// removal-side of the bump-direction matrix.
func noToolBundle() registrybundle.Bundle {
	bundle := oneToolBundle("unused")
	bundle.Tools = nil
	return bundle
}

func withVersion(bundle registrybundle.Bundle, version string) registrybundle.Bundle {
	bundle.Version = version
	return bundle
}

func assertIllegalSemverBump(t *testing.T, err error) {
	t.Helper()
	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("err = %T, want *httpx.DomainError", err)
	}
	if de.Status != 422 {
		t.Errorf("Status = %d, want 422", de.Status)
	}
	if de.Code != CodeIllegalSemverBump {
		t.Errorf("Code = %q, want %q", de.Code, CodeIllegalSemverBump)
	}
}

func TestPublish_RepublishingAnAlreadyPublishedVersionIsRejectedAndLeavesTheStoreUnchanged(t *testing.T) {
	f := newFacadeForTest()
	if _, err := f.Publish(context.Background(), "outlook", withVersion(oneToolBundle("outlook-list-messages"), "1.0.0")); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	renamed := oneToolBundle("outlook-list-messages")
	renamed.Name = "Outlook (renamed)"
	_, err := f.Publish(context.Background(), "outlook", withVersion(renamed, "1.0.0"))

	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("second Publish err = %T, want *httpx.DomainError", err)
	}
	if de.Status != 409 {
		t.Errorf("Status = %d, want 409", de.Status)
	}
	if de.Code != CodeVersionConflict {
		t.Errorf("Code = %q, want %q", de.Code, CodeVersionConflict)
	}

	pulled, pullErr := f.Pull(context.Background(), "outlook", "1.0.0")
	if pullErr != nil {
		t.Fatalf("Pull after rejected republish: %v", pullErr)
	}
	if pulled.Name != "Outlook" {
		t.Errorf("stored bundle Name = %q after a rejected republish, want the original %q unchanged", pulled.Name, "Outlook")
	}
}

func TestPublish_ExplicitVersionNotStrictlyGreaterThanLatestIsRejected(t *testing.T) {
	f := newFacadeForTest()
	if _, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages")); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	_, err := f.Publish(context.Background(), "outlook", withVersion(oneToolBundle("outlook-list-messages"), "0.5.0"))

	assertIllegalSemverBump(t, err)
}

func TestPublish_AddingAToolWithOnlyAPatchBumpIsRejected(t *testing.T) {
	f := newFacadeForTest()
	if _, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages")); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	_, err := f.Publish(context.Background(), "outlook",
		withVersion(twoToolBundle("outlook-list-messages", "outlook-get-message"), "1.0.1"))

	assertIllegalSemverBump(t, err)
}

func TestPublish_AddingAToolWithAMinorBumpIsAccepted(t *testing.T) {
	f := newFacadeForTest()
	if _, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages")); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	result, err := f.Publish(context.Background(), "outlook",
		withVersion(twoToolBundle("outlook-list-messages", "outlook-get-message"), "1.1.0"))

	if err != nil {
		t.Fatalf("Publish with a minor bump for an addition: %v", err)
	}
	if result.Version != "1.1.0" {
		t.Errorf("Version = %q, want %q", result.Version, "1.1.0")
	}
}

func TestPublish_AddingAToolWithAMajorBumpIsAccepted(t *testing.T) {
	f := newFacadeForTest()
	if _, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages")); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	result, err := f.Publish(context.Background(), "outlook",
		withVersion(twoToolBundle("outlook-list-messages", "outlook-get-message"), "2.0.0"))

	if err != nil {
		t.Fatalf("Publish with a major bump for an addition: %v", err)
	}
	if result.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", result.Version, "2.0.0")
	}
}

func TestPublish_RemovingAToolWithOnlyAMinorBumpIsRejected(t *testing.T) {
	f := newFacadeForTest()
	if _, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages")); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	_, err := f.Publish(context.Background(), "outlook", withVersion(noToolBundle(), "1.1.0"))

	assertIllegalSemverBump(t, err)
}

func TestPublish_RemovingAToolWithAMajorBumpIsAccepted(t *testing.T) {
	f := newFacadeForTest()
	if _, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages")); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	result, err := f.Publish(context.Background(), "outlook", withVersion(noToolBundle(), "2.0.0"))

	if err != nil {
		t.Fatalf("Publish with a major bump for a removal: %v", err)
	}
	if result.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", result.Version, "2.0.0")
	}
}

func TestPublish_OmittedVersionAutoAdvancesMajorOnRemoval(t *testing.T) {
	f := newFacadeForTest()
	if _, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages")); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	result, err := f.Publish(context.Background(), "outlook", noToolBundle())

	if err != nil {
		t.Fatalf("Publish with an omitted version after a removal: %v", err)
	}
	if result.Version != "2.0.0" {
		t.Errorf("Version = %q, want auto-advanced major %q", result.Version, "2.0.0")
	}
}

func TestPublish_OmittedVersionAutoAdvancesMinorOnAddition(t *testing.T) {
	f := newFacadeForTest()
	if _, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages")); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	result, err := f.Publish(context.Background(), "outlook", twoToolBundle("outlook-list-messages", "outlook-get-message"))

	if err != nil {
		t.Fatalf("Publish with an omitted version after an addition: %v", err)
	}
	if result.Version != "1.1.0" {
		t.Errorf("Version = %q, want auto-advanced minor %q", result.Version, "1.1.0")
	}
}

func TestPublish_OmittedVersionAutoAdvancesPatchWhenNothingAddedOrRemoved(t *testing.T) {
	f := newFacadeForTest()
	if _, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages")); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	unchangedSlugs := oneToolBundle("outlook-list-messages")
	unchangedSlugs.Name = "Outlook (content-only change)"
	result, err := f.Publish(context.Background(), "outlook", unchangedSlugs)

	if err != nil {
		t.Fatalf("Publish with an omitted version and no slug changes: %v", err)
	}
	if result.Version != "1.0.1" {
		t.Errorf("Version = %q, want auto-advanced patch %q", result.Version, "1.0.1")
	}
}

func TestPublish_ExplicitVersionIsEnforcedWhileAnOmittedVersionIsAutoComputed(t *testing.T) {
	f := newFacadeForTest()
	if _, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages")); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	// An explicit version that violates the bump-direction rule is rejected
	// rather than silently corrected...
	if _, err := f.Publish(context.Background(), "outlook",
		withVersion(twoToolBundle("outlook-list-messages", "outlook-get-message"), "1.0.1")); err == nil {
		t.Fatal("Publish with an explicit but illegal version bump succeeded, want it rejected")
	}

	// ...while the very same content change with the version field left
	// empty is accepted and assigned the correct auto-computed version.
	result, err := f.Publish(context.Background(), "outlook", twoToolBundle("outlook-list-messages", "outlook-get-message"))
	if err != nil {
		t.Fatalf("Publish with an omitted version: %v", err)
	}
	if result.Version != "1.1.0" {
		t.Errorf("Version = %q, want auto-computed %q", result.Version, "1.1.0")
	}
}

func TestPublish_ReportsAddedToolsAndTriggersRelativeToThePreviousVersion(t *testing.T) {
	f := newFacadeForTest()
	first := oneToolBundle("outlook-list-messages")
	first.Triggers = []registrybundle.Trigger{{Slug: "outlook-message-received", Name: "Message received"}}
	if _, err := f.Publish(context.Background(), "outlook", first); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	next := twoToolBundle("outlook-list-messages", "outlook-get-message")
	next.Triggers = []registrybundle.Trigger{
		{Slug: "outlook-message-received", Name: "Message received"},
		{Slug: "outlook-message-deleted", Name: "Message deleted"},
	}

	result, err := f.Publish(context.Background(), "outlook", next)

	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(result.Added.Tools) != 1 || result.Added.Tools[0] != "outlook-get-message" {
		t.Errorf("Added.Tools = %v, want [\"outlook-get-message\"]", result.Added.Tools)
	}
	if len(result.Added.Triggers) != 1 || result.Added.Triggers[0] != "outlook-message-deleted" {
		t.Errorf("Added.Triggers = %v, want [\"outlook-message-deleted\"]", result.Added.Triggers)
	}
	if len(result.Removed.Tools) != 0 || len(result.Removed.Triggers) != 0 {
		t.Errorf("Removed = %+v, want nothing removed", result.Removed)
	}
}

func TestPublish_ReportsRemovedToolsRelativeToThePreviousVersion(t *testing.T) {
	f := newFacadeForTest()
	if _, err := f.Publish(context.Background(), "outlook", twoToolBundle("outlook-list-messages", "outlook-get-message")); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	result, err := f.Publish(context.Background(), "outlook", withVersion(oneToolBundle("outlook-list-messages"), "2.0.0"))

	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(result.Removed.Tools) != 1 || result.Removed.Tools[0] != "outlook-get-message" {
		t.Errorf("Removed.Tools = %v, want [\"outlook-get-message\"]", result.Removed.Tools)
	}
	if len(result.Added.Tools) != 0 {
		t.Errorf("Added.Tools = %v, want nothing added", result.Added.Tools)
	}
}
