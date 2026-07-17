package membraneimport

import (
	"strings"
	"testing"
)

// TestConvert_UnknownConnectorEmitsTODOPlaceholdersForOAuthAndBaseURL is
// Slice 3's AC2 for the fallback path: a connector matching no preset alias
// (the Slice 1/2 "test-crm" fixture) still emits a shape-complete oauth
// block and mapping.baseUrl -- explicit TODO placeholders rather than empty
// strings -- so the definition still parses through the real strict loader
// (AC5), even though it is not yet usable.
func TestConvert_UnknownConnectorEmitsTODOPlaceholdersForOAuthAndBaseURL(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-get-record.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	def := loadSingleEmittedDefinition(t, result)
	if def.AuthorizeURL != todoAuthorizeURL {
		t.Errorf("AuthorizeURL = %q, want the TODO placeholder %q", def.AuthorizeURL, todoAuthorizeURL)
	}
	if def.TokenURL != todoTokenURL {
		t.Errorf("TokenURL = %q, want the TODO placeholder %q", def.TokenURL, todoTokenURL)
	}
	if def.UserInfoURL != todoUserInfoURL {
		t.Errorf("UserInfoURL = %q, want the TODO placeholder %q", def.UserInfoURL, todoUserInfoURL)
	}
	if len(def.Scopes) != 1 || def.Scopes[0] != todoScope {
		t.Errorf("Scopes = %v, want a single TODO placeholder scope %q", def.Scopes, todoScope)
	}
	if def.BaseURL != todoBaseURL {
		t.Errorf("BaseURL = %q, want the TODO placeholder %q", def.BaseURL, todoBaseURL)
	}
	if def.AuthScheme != "oauth2" {
		t.Errorf("AuthScheme = %q, want %q (the importer emits none; the loader defaults it)", def.AuthScheme, "oauth2")
	}
}

// TestConvert_UnknownConnectorTODOFieldsListedAsProviderPartialCaveats is
// Slice 3's AC2 report-visibility half: every one of the five TODO
// placeholder fields is named, individually, as a caveat under its own
// "provider"-kind Partial item -- an operator scanning the report sees
// exactly what to fill in, not one generic "needs OAuth" line.
func TestConvert_UnknownConnectorTODOFieldsListedAsProviderPartialCaveats(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-get-record.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	partialSection := reportSection(t, string(result.Report), "Partial")
	for _, want := range []string{
		`provider "test-crm"`, // the provider-kind item's own identity line
		"integration.yaml",    // source: the integration record's own file name
		"oauth.authorizeUrl is a TODO placeholder",
		"oauth.tokenUrl is a TODO placeholder",
		"oauth.userInfoUrl is a TODO placeholder",
		"oauth.scopes is a TODO placeholder",
		"mapping.baseUrl is a TODO placeholder",
	} {
		if !strings.Contains(partialSection, want) {
			t.Errorf("Partial section missing %q:\n%s", want, partialSection)
		}
	}
}
