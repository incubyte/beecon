// Package organizations_test (see facade_test.go's header for why this must
// be an external test package). Covers PD4's redirect-uri allow-list: the
// pure Organization.AllowsRedirectURI matching rules, and the
// SetAllowedRedirectURIs facade operation the installation admin uses to set
// them (AC5).
package organizations_test

import (
	"context"
	"testing"

	"beecon/internal/organizations"
)

func TestAllowsRedirectURI_RejectsEveryCandidateWhenTheAllowListIsEmpty(t *testing.T) {
	org := organizations.Organization{AllowedRedirectURIs: []string{}}

	if org.AllowsRedirectURI("https://consumer.example.com/callback") {
		t.Error("an empty allow-list must reject every redirectUri (PD4: secure default, no open redirect)")
	}
}

func TestAllowsRedirectURI_AllowsAnExactURLMatch(t *testing.T) {
	org := organizations.Organization{AllowedRedirectURIs: []string{"https://consumer.example.com/callback"}}

	if !org.AllowsRedirectURI("https://consumer.example.com/callback") {
		t.Error("expected an exact URL match to be allowed")
	}
}

func TestAllowsRedirectURI_RejectsAURLNotOnTheList(t *testing.T) {
	org := organizations.Organization{AllowedRedirectURIs: []string{"https://consumer.example.com/callback"}}

	if org.AllowsRedirectURI("https://evil.example.com/callback") {
		t.Error("expected a URL not on the allow-list to be rejected")
	}
}

func TestAllowsRedirectURI_AnOriginOnlyEntryAllowsAnyPathUnderThatOrigin(t *testing.T) {
	org := organizations.Organization{AllowedRedirectURIs: []string{"https://consumer.example.com"}}

	if !org.AllowsRedirectURI("https://consumer.example.com/any/deep/path") {
		t.Error("an origin-only allow-list entry (no path) must match any path under that origin")
	}
}

func TestAllowsRedirectURI_AnOriginOnlyEntryRejectsADifferentSchemeOrHost(t *testing.T) {
	org := organizations.Organization{AllowedRedirectURIs: []string{"https://consumer.example.com"}}

	if org.AllowsRedirectURI("http://consumer.example.com/callback") {
		t.Error("expected a different scheme to be rejected even against an origin-only entry")
	}
	if org.AllowsRedirectURI("https://other.example.com/callback") {
		t.Error("expected a different host to be rejected even against an origin-only entry")
	}
}

func TestAllowsRedirectURI_AnEntryWithAnExplicitPathDoesNotMatchADifferentPathUnderTheSameOrigin(t *testing.T) {
	org := organizations.Organization{AllowedRedirectURIs: []string{"https://consumer.example.com/callback"}}

	if org.AllowsRedirectURI("https://consumer.example.com/other-path") {
		t.Error("an entry carrying its own path must match exactly, not any path under the origin")
	}
}

func TestAllowsRedirectURI_FailsClosedWhenAnAllowListEntryFailsToParse(t *testing.T) {
	org := organizations.Organization{AllowedRedirectURIs: []string{"https://consumer.example.com\x7f"}}

	if org.AllowsRedirectURI("https://consumer.example.com/callback") {
		t.Error("an allow-list entry that fails url.Parse must not match any candidate (fail closed)")
	}
}

func TestAllowsRedirectURI_FailsClosedWhenTheCandidateFailsToParse(t *testing.T) {
	org := organizations.Organization{AllowedRedirectURIs: []string{"https://consumer.example.com"}}

	if org.AllowsRedirectURI("https://consumer.example.com/\x7f") {
		t.Error("a candidate redirectUri that fails url.Parse must be rejected, not matched (fail closed)")
	}
}

func TestSetAllowedRedirectURIs_ReplacesTheOrganizationsAllowList(t *testing.T) {
	f := newFacade()
	org, err := f.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := f.SetAllowedRedirectURIs(context.Background(), org.ID, []string{"https://consumer.example.com/callback"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.AllowedRedirectURIs) != 1 || updated.AllowedRedirectURIs[0] != "https://consumer.example.com/callback" {
		t.Errorf("AllowedRedirectURIs = %v, want [%q]", updated.AllowedRedirectURIs, "https://consumer.example.com/callback")
	}

	refetched, err := f.Get(context.Background(), org.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refetched.AllowedRedirectURIs) != 1 {
		t.Errorf("the allow-list update was not persisted: Get() returned %v", refetched.AllowedRedirectURIs)
	}
}

func TestSetAllowedRedirectURIs_TrimsWhitespaceAndDropsBlankEntries(t *testing.T) {
	f := newFacade()
	org, err := f.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := f.SetAllowedRedirectURIs(context.Background(), org.ID, []string{"  https://consumer.example.com/callback  ", "", "   "})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.AllowedRedirectURIs) != 1 || updated.AllowedRedirectURIs[0] != "https://consumer.example.com/callback" {
		t.Errorf("AllowedRedirectURIs = %v, want blank entries dropped and the remaining entry trimmed", updated.AllowedRedirectURIs)
	}
}

func TestSetAllowedRedirectURIs_ReturnsNotFoundForAnUnknownOrgID(t *testing.T) {
	f := newFacade()

	_, err := f.SetAllowedRedirectURIs(context.Background(), organizations.OrgID("org_missing"), []string{"https://consumer.example.com/callback"})

	assertDomainError(t, err, organizations.CodeNotFound, 404)
}
