// Package catalog_test (external test package — no import cycle risk here,
// but kept consistent with the rest of the codebase's facade/domain tests).
// Covers AC1 (a provider definition file loads name, logo, OAuth
// authorize/token endpoints, scopes, tool definitions) and AC2 (an invalid
// definition fails naming the file and the field) using testing/fstest.MapFS
// in place of the real embedded providers/ directory.
package catalog_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"beecon/internal/catalog"
)

const validDefinitionYAML = `
formatVersion: 1
slug: outlook
name: Outlook
logo: https://static.beecon.dev/providers/outlook.png
authScheme: oauth2
oauth:
  authorizeUrl: https://login.microsoftonline.com/common/oauth2/v2.0/authorize
  tokenUrl: https://login.microsoftonline.com/common/oauth2/v2.0/token
  scopes:
    - offline_access
    - Mail.Read
mapping:
  baseUrl: https://graph.microsoft.com
tools:
  - slug: outlook-list-messages
    name: List messages
    description: List messages in the mailbox.
    inputSchema:
      type: object
    outputSchema:
      type: object
    mapping:
      method: GET
      path: /v1.0/me/messages
`

func mapFSWithFile(name, contents string) fstest.MapFS {
	return fstest.MapFS{
		name: {Data: []byte(contents)},
	}
}

// yamlWithoutLineContaining drops every whole line containing substr,
// preserving every other line's own indentation — unlike a raw
// strings.Replace on a partial line match, this keeps the remaining YAML
// well-formed.
func yamlWithoutLineContaining(yaml, substr string) string {
	lines := strings.Split(yaml, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, substr) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func TestLoadProviderDefinitions_LoadsNameLogoOAuthEndpointsScopesAndTools(t *testing.T) {
	defs, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", validDefinitionYAML))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("len(defs) = %d, want 1", len(defs))
	}
	d := defs[0]
	if d.Slug != "outlook" {
		t.Errorf("Slug = %q, want %q", d.Slug, "outlook")
	}
	if d.Name != "Outlook" {
		t.Errorf("Name = %q, want %q", d.Name, "Outlook")
	}
	if d.Logo != "https://static.beecon.dev/providers/outlook.png" {
		t.Errorf("Logo = %q, want the configured logo URL", d.Logo)
	}
	if d.AuthorizeURL != "https://login.microsoftonline.com/common/oauth2/v2.0/authorize" {
		t.Errorf("AuthorizeURL = %q, want the configured authorize URL", d.AuthorizeURL)
	}
	if d.TokenURL != "https://login.microsoftonline.com/common/oauth2/v2.0/token" {
		t.Errorf("TokenURL = %q, want the configured token URL", d.TokenURL)
	}
	wantScopes := []string{"offline_access", "Mail.Read"}
	if len(d.Scopes) != len(wantScopes) {
		t.Fatalf("Scopes = %v, want %v", d.Scopes, wantScopes)
	}
	for i, s := range wantScopes {
		if d.Scopes[i] != s {
			t.Errorf("Scopes[%d] = %q, want %q", i, d.Scopes[i], s)
		}
	}
	if len(d.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(d.Tools))
	}
	tool := d.Tools[0]
	if tool.Slug != "outlook-list-messages" {
		t.Errorf("Tools[0].Slug = %q, want %q", tool.Slug, "outlook-list-messages")
	}
	if tool.Method != "GET" {
		t.Errorf("Tools[0].Method = %q, want %q", tool.Method, "GET")
	}
	if tool.Path != "/v1.0/me/messages" {
		t.Errorf("Tools[0].Path = %q, want %q", tool.Path, "/v1.0/me/messages")
	}
}

func TestLoadProviderDefinitions_DefaultsAuthSchemeToOAuth2WhenOmitted(t *testing.T) {
	yamlWithoutAuthScheme := `
formatVersion: 1
slug: outlook
name: Outlook
logo: https://static.beecon.dev/providers/outlook.png
oauth:
  authorizeUrl: https://example.com/authorize
  tokenUrl: https://example.com/token
  scopes:
    - Mail.Read
mapping:
  baseUrl: https://example.com
`
	defs, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", yamlWithoutAuthScheme))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defs[0].AuthScheme != "oauth2" {
		t.Errorf("AuthScheme = %q, want default %q", defs[0].AuthScheme, "oauth2")
	}
}

func TestLoadProviderDefinitions_LoadsMultipleFilesInSortedFilenameOrder(t *testing.T) {
	fsys := fstest.MapFS{
		"z-provider.yaml": {Data: []byte(strings.Replace(validDefinitionYAML, "slug: outlook", "slug: z-provider", 1))},
		"a-provider.yaml": {Data: []byte(strings.Replace(validDefinitionYAML, "slug: outlook", "slug: a-provider", 1))},
	}

	defs, err := catalog.LoadProviderDefinitions(fsys)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("len(defs) = %d, want 2", len(defs))
	}
	if defs[0].Slug != "a-provider" || defs[1].Slug != "z-provider" {
		t.Errorf("load order = [%q, %q], want sorted-by-filename order [a-provider, z-provider]", defs[0].Slug, defs[1].Slug)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenSlugIsMissing(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML, "slug: outlook\n", "", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	if err == nil {
		t.Fatal("expected an error for a missing slug field, got nil")
	}
	wantMessage := `invalid provider definition outlook.yaml: field "slug" must not be empty`
	if err.Error() != wantMessage {
		t.Errorf("error = %q, want %q", err.Error(), wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenNameIsMissing(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML, "name: Outlook\n", "", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "name" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenLogoIsMissing(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML, "logo: https://static.beecon.dev/providers/outlook.png\n", "", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "logo" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenAuthorizeURLIsMissing(t *testing.T) {
	invalid := yamlWithoutLineContaining(validDefinitionYAML, "authorizeUrl:")

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "oauth.authorizeUrl" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenTokenURLIsMissing(t *testing.T) {
	invalid := yamlWithoutLineContaining(validDefinitionYAML, "tokenUrl:")

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "oauth.tokenUrl" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsWhenNoScopesAreDeclared(t *testing.T) {
	invalid := `
formatVersion: 1
slug: outlook
name: Outlook
logo: https://static.beecon.dev/providers/outlook.png
oauth:
  authorizeUrl: https://example.com/authorize
  tokenUrl: https://example.com/token
  scopes: []
mapping:
  baseUrl: https://example.com
`
	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "oauth.scopes" must declare at least one scope`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenAToolIsMissingItsSlug(t *testing.T) {
	// Blanking the value (rather than dropping the whole line) keeps the
	// tool's "- " YAML sequence-item marker intact.
	invalid := strings.Replace(validDefinitionYAML, "slug: outlook-list-messages", "slug: ''", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "tools[0].slug" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsOnUnparsableYAML(t *testing.T) {
	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", "slug: [unterminated"))

	if err == nil {
		t.Fatal("expected a parse error for malformed YAML, got nil")
	}
}

// findDefinitionBySlug locates one loaded ProviderDefinition by slug —
// DefaultProviderDefinitions() loads every embedded provider file (Outlook
// and, since Slice 2, Hubspot), so tests that care about one provider's own
// shape look it up by slug rather than assuming it is the only one loaded.
func findDefinitionBySlug(defs []catalog.ProviderDefinition, slug string) (catalog.ProviderDefinition, bool) {
	for _, d := range defs {
		if d.Slug == slug {
			return d, true
		}
	}
	return catalog.ProviderDefinition{}, false
}

func TestDefaultProviderDefinitions_LoadsTheEmbeddedOutlookDefinition(t *testing.T) {
	defs, err := catalog.DefaultProviderDefinitions()

	if err != nil {
		t.Fatalf("unexpected error loading the embedded provider definitions: %v", err)
	}
	outlook, ok := findDefinitionBySlug(defs, "outlook")
	if !ok {
		t.Fatalf("defs = %+v, want it to include the bundled Outlook definition", defs)
	}
	if outlook.Slug != "outlook" {
		t.Errorf("Slug = %q, want %q", outlook.Slug, "outlook")
	}
	if outlook.Name != "Outlook" {
		t.Errorf("Name = %q, want %q", outlook.Name, "Outlook")
	}
	if outlook.AuthorizeURL == "" || outlook.TokenURL == "" {
		t.Error("AuthorizeURL/TokenURL must not be empty for the bundled Outlook definition")
	}
	foundListMessages := false
	for _, tool := range outlook.Tools {
		if tool.Slug == "outlook-list-messages" {
			foundListMessages = true
		}
	}
	if !foundListMessages {
		t.Errorf("Tools = %+v, want it to include the outlook-list-messages tool (PD8)", outlook.Tools)
	}
}

// TestDefaultProviderDefinitions_LoadsTheEmbeddedHubspotDefinition is AC1: the
// second provider arrives purely as a definition file — no provider-specific
// Go code was added to parse credentialStyle/userInfo generically (PD13,
// PD16) or to make hubspot-list-contacts/hubspot-create-contact executable.
func TestDefaultProviderDefinitions_LoadsTheEmbeddedHubspotDefinition(t *testing.T) {
	defs, err := catalog.DefaultProviderDefinitions()

	if err != nil {
		t.Fatalf("unexpected error loading the embedded provider definitions: %v", err)
	}
	hubspot, ok := findDefinitionBySlug(defs, "hubspot")
	if !ok {
		t.Fatalf("defs = %+v, want it to include the bundled Hubspot definition", defs)
	}
	if hubspot.Name != "Hubspot" {
		t.Errorf("Name = %q, want %q", hubspot.Name, "Hubspot")
	}
	if hubspot.AuthorizeURL == "" || hubspot.TokenURL == "" {
		t.Error("AuthorizeURL/TokenURL must not be empty for the bundled Hubspot definition")
	}
	if hubspot.CredentialStyle != catalog.CredentialStyleFormBody {
		t.Errorf("CredentialStyle = %q, want %q (declared explicitly in hubspot.yaml)", hubspot.CredentialStyle, catalog.CredentialStyleFormBody)
	}
	if hubspot.UserInfo.EmailField != "user" {
		t.Errorf("UserInfo.EmailField = %q, want %q", hubspot.UserInfo.EmailField, "user")
	}
	if hubspot.UserInfo.DisplayNameField != "hub_domain" {
		t.Errorf("UserInfo.DisplayNameField = %q, want %q", hubspot.UserInfo.DisplayNameField, "hub_domain")
	}

	wantTools := map[string]bool{"hubspot-list-contacts": false, "hubspot-create-contact": false, "hubspot-upload-file": false}
	for _, tool := range hubspot.Tools {
		if _, declared := wantTools[tool.Slug]; declared {
			wantTools[tool.Slug] = true
		}
		if tool.Slug == "hubspot-list-contacts" {
			if tool.Mapping.Pagination == nil {
				t.Fatal("hubspot-list-contacts declares no Mapping.Pagination, want one (PD15b)")
			}
			if tool.Mapping.Pagination.PageSizeParam != "limit" || tool.Mapping.Pagination.CursorParam != "after" {
				t.Errorf("Pagination = %+v, want PageSizeParam=limit, CursorParam=after", tool.Mapping.Pagination)
			}
			if tool.Mapping.Pagination.NextCursorPath != "paging.next.after" {
				t.Errorf("NextCursorPath = %q, want %q", tool.Mapping.Pagination.NextCursorPath, "paging.next.after")
			}
		}
		if tool.Slug == "hubspot-create-contact" {
			if tool.Mapping.Body["properties.email"] != "{input.email}" {
				t.Errorf(`Mapping.Body["properties.email"] = %q, want %q`, tool.Mapping.Body["properties.email"], "{input.email}")
			}
		}
	}
	for slug, found := range wantTools {
		if !found {
			t.Errorf("Tools = %+v, want it to include %q", hubspot.Tools, slug)
		}
	}
}
