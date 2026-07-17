// Package catalog_test (external test package — no import cycle risk here,
// but kept consistent with the rest of the codebase's facade/domain tests).
// Covers AC1 (a provider definition file loads name, logo, OAuth
// authorize/token endpoints, scopes, tool definitions) and AC2 (an invalid
// definition fails naming the file and the field) using testing/fstest.MapFS
// in place of the real embedded providers/ directory.
package catalog_test

import (
	"slices"
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
		// hubspot-upload-file (Slice 7, PD22): pins the YAML parse actually
		// carries its file-typed input through to Mapping.FileInputs — a
		// parsing regression here would silently drop file support from the
		// shipped definition without failing any other assertion.
		if tool.Slug == "hubspot-upload-file" {
			if !slices.Equal(tool.Mapping.FileInputs, []string{"file"}) {
				t.Errorf("Mapping.FileInputs = %v, want %v", tool.Mapping.FileInputs, []string{"file"})
			}
		}
	}
	for slug, found := range wantTools {
		if !found {
			t.Errorf("Tools = %+v, want it to include %q", hubspot.Tools, slug)
		}
	}
}

// TestDefaultProviderDefinitions_LoadsTheEmbeddedGmailDefinition is the
// Providers strand's Gmail slice's boot-load AC: gmail.yaml parses under the
// real strict loader with no error and boot-loads purely as a definition
// file — no provider-specific Go code was added to make its OAuth block or
// its three tools' mappings (pagination, path templating, JSON body)
// parseable.
func TestDefaultProviderDefinitions_LoadsTheEmbeddedGmailDefinition(t *testing.T) {
	defs, err := catalog.DefaultProviderDefinitions()

	if err != nil {
		t.Fatalf("unexpected error loading the embedded provider definitions: %v", err)
	}
	gmail, ok := findDefinitionBySlug(defs, "gmail")
	if !ok {
		t.Fatalf("defs = %+v, want it to include the bundled Gmail definition", defs)
	}
	if gmail.Name != "Gmail" {
		t.Errorf("Name = %q, want %q", gmail.Name, "Gmail")
	}
	if gmail.AuthorizeURL != "https://accounts.google.com/o/oauth2/v2/auth" {
		t.Errorf("AuthorizeURL = %q, want the Google authorize endpoint", gmail.AuthorizeURL)
	}
	if gmail.TokenURL != "https://oauth2.googleapis.com/token" {
		t.Errorf("TokenURL = %q, want the Google token endpoint", gmail.TokenURL)
	}
	if gmail.UserInfoURL != "https://www.googleapis.com/oauth2/v3/userinfo" {
		t.Errorf("UserInfoURL = %q, want the Google OpenID userinfo endpoint", gmail.UserInfoURL)
	}
	wantScopes := []string{"openid", "email", "profile", "https://www.googleapis.com/auth/gmail.readonly", "https://www.googleapis.com/auth/gmail.send"}
	for _, scope := range wantScopes {
		if !slices.Contains(gmail.Scopes, scope) {
			t.Errorf("Scopes = %v, want it to include %q", gmail.Scopes, scope)
		}
	}
	if gmail.UserInfo.EmailField != "email" {
		t.Errorf("UserInfo.EmailField = %q, want %q", gmail.UserInfo.EmailField, "email")
	}
	if gmail.UserInfo.DisplayNameField != "name" {
		t.Errorf("UserInfo.DisplayNameField = %q, want %q", gmail.UserInfo.DisplayNameField, "name")
	}
	if gmail.BaseURL != "https://gmail.googleapis.com/gmail/v1" {
		t.Errorf("BaseURL = %q, want the Gmail API base", gmail.BaseURL)
	}
	if len(gmail.Triggers) != 0 {
		t.Errorf("Triggers = %+v, want an empty list (Gmail defers its trigger, PD80)", gmail.Triggers)
	}

	wantTools := map[string]bool{"gmail-list-messages": false, "gmail-get-message": false, "gmail-send-message": false}
	for _, tool := range gmail.Tools {
		if _, declared := wantTools[tool.Slug]; declared {
			wantTools[tool.Slug] = true
		}
		if len(tool.InputSchema) == 0 || len(tool.OutputSchema) == 0 {
			t.Errorf("tool %q has an empty input/output schema", tool.Slug)
		}
		if tool.Slug == "gmail-list-messages" {
			if tool.Mapping.Pagination == nil {
				t.Fatal("gmail-list-messages declares no Mapping.Pagination, want one (PD15b)")
			}
			if tool.Mapping.Pagination.PageSizeParam != "maxResults" || tool.Mapping.Pagination.CursorParam != "pageToken" {
				t.Errorf("Pagination = %+v, want PageSizeParam=maxResults, CursorParam=pageToken", tool.Mapping.Pagination)
			}
			if tool.Mapping.Pagination.NextCursorPath != "nextPageToken" {
				t.Errorf("NextCursorPath = %q, want %q", tool.Mapping.Pagination.NextCursorPath, "nextPageToken")
			}
		}
		if tool.Slug == "gmail-get-message" {
			if tool.Path != "/users/me/messages/{input.messageId}" {
				t.Errorf("Path = %q, want the {input.messageId} path token", tool.Path)
			}
		}
		if tool.Slug == "gmail-send-message" {
			if tool.Mapping.Body["raw"] != "{input.raw}" {
				t.Errorf(`Mapping.Body["raw"] = %q, want %q`, tool.Mapping.Body["raw"], "{input.raw}")
			}
		}
	}
	for slug, found := range wantTools {
		if !found {
			t.Errorf("Tools = %+v, want it to include %q", gmail.Tools, slug)
		}
	}
}

// TestDefaultProviderDefinitions_LoadsTheEmbeddedGoogleCalendarDefinition is
// the Providers strand's Google Calendar slice's boot-load AC:
// google-calendar.yaml parses under the real strict loader and boot-loads
// purely as a definition file — reusing gmail.yaml's shared Google OAuth
// block (PD78) and adding the strand's one poll trigger (PD80) with no
// provider-specific Go code.
func TestDefaultProviderDefinitions_LoadsTheEmbeddedGoogleCalendarDefinition(t *testing.T) {
	defs, err := catalog.DefaultProviderDefinitions()

	if err != nil {
		t.Fatalf("unexpected error loading the embedded provider definitions: %v", err)
	}
	gcal, ok := findDefinitionBySlug(defs, "google-calendar")
	if !ok {
		t.Fatalf("defs = %+v, want it to include the bundled Google Calendar definition", defs)
	}
	if gcal.Name != "Google Calendar" {
		t.Errorf("Name = %q, want %q", gcal.Name, "Google Calendar")
	}
	if gcal.AuthorizeURL != "https://accounts.google.com/o/oauth2/v2/auth" {
		t.Errorf("AuthorizeURL = %q, want the Google authorize endpoint (shared with gmail.yaml, PD78)", gcal.AuthorizeURL)
	}
	if gcal.TokenURL != "https://oauth2.googleapis.com/token" {
		t.Errorf("TokenURL = %q, want the Google token endpoint (shared with gmail.yaml, PD78)", gcal.TokenURL)
	}
	if gcal.UserInfoURL != "https://www.googleapis.com/oauth2/v3/userinfo" {
		t.Errorf("UserInfoURL = %q, want the Google OpenID userinfo endpoint (shared with gmail.yaml, PD78)", gcal.UserInfoURL)
	}
	if !slices.Contains(gcal.Scopes, "https://www.googleapis.com/auth/calendar.events") {
		t.Errorf("Scopes = %v, want it to include the calendar.events scope", gcal.Scopes)
	}
	if gcal.UserInfo.EmailField != "email" {
		t.Errorf("UserInfo.EmailField = %q, want %q", gcal.UserInfo.EmailField, "email")
	}
	if gcal.UserInfo.DisplayNameField != "name" {
		t.Errorf("UserInfo.DisplayNameField = %q, want %q", gcal.UserInfo.DisplayNameField, "name")
	}
	if gcal.BaseURL != "https://www.googleapis.com/calendar/v3" {
		t.Errorf("BaseURL = %q, want the Calendar API base", gcal.BaseURL)
	}

	wantTools := map[string]bool{"gcal-list-events": false, "gcal-create-event": false}
	for _, tool := range gcal.Tools {
		if _, declared := wantTools[tool.Slug]; declared {
			wantTools[tool.Slug] = true
		}
		if len(tool.InputSchema) == 0 || len(tool.OutputSchema) == 0 {
			t.Errorf("tool %q has an empty input/output schema", tool.Slug)
		}
		if tool.Slug == "gcal-list-events" {
			if tool.Mapping.Pagination == nil {
				t.Fatal("gcal-list-events declares no Mapping.Pagination, want one (PD15b)")
			}
			if tool.Mapping.Pagination.PageSizeParam != "maxResults" || tool.Mapping.Pagination.CursorParam != "pageToken" {
				t.Errorf("Pagination = %+v, want PageSizeParam=maxResults, CursorParam=pageToken", tool.Mapping.Pagination)
			}
			properties, _ := tool.InputSchema["properties"].(map[string]any)
			calendarID, _ := properties["calendarId"].(map[string]any)
			if calendarID["default"] != "primary" {
				t.Errorf("inputSchema.properties.calendarId.default = %v, want %q (engine-gaps Gap C)", calendarID["default"], "primary")
			}
		}
		if tool.Slug == "gcal-create-event" {
			if tool.Mapping.Body["start.dateTime"] != "{input.startDateTime}" {
				t.Errorf(`Mapping.Body["start.dateTime"] = %q, want %q`, tool.Mapping.Body["start.dateTime"], "{input.startDateTime}")
			}
			if tool.Mapping.Body["end.dateTime"] != "{input.endDateTime}" {
				t.Errorf(`Mapping.Body["end.dateTime"] = %q, want %q`, tool.Mapping.Body["end.dateTime"], "{input.endDateTime}")
			}
		}
	}
	for slug, found := range wantTools {
		if !found {
			t.Errorf("Tools = %+v, want it to include %q", gcal.Tools, slug)
		}
	}

	if len(gcal.Triggers) != 1 {
		t.Fatalf("Triggers = %+v, want exactly 1 (gcal-event-updated, PD80)", gcal.Triggers)
	}
	trigger := gcal.Triggers[0]
	if trigger.Slug != "gcal-event-updated" {
		t.Errorf("Triggers[0].Slug = %q, want %q", trigger.Slug, "gcal-event-updated")
	}
	if trigger.Ingestion != "poll" {
		t.Errorf("Ingestion = %q, want %q", trigger.Ingestion, "poll")
	}
	if len(trigger.ConfigSchema) == 0 {
		t.Error("ConfigSchema must not be empty")
	}
	if len(trigger.PayloadSchema) == 0 {
		t.Error("PayloadSchema must not be empty")
	}
	if trigger.Poll.Path != "/calendars/{config.calendarId}/events" {
		t.Errorf("Poll.Path = %q, want the config.calendarId-templated events path", trigger.Poll.Path)
	}
	if trigger.Poll.Query["updatedMin"] != "{watermark}" {
		t.Errorf(`Poll.Query["updatedMin"] = %q, want %q`, trigger.Poll.Query["updatedMin"], "{watermark}")
	}
	if trigger.Poll.Query["orderBy"] != "updated" {
		t.Errorf(`Poll.Query["orderBy"] = %q, want the literal %q`, trigger.Poll.Query["orderBy"], "updated")
	}
	if trigger.Poll.Query["singleEvents"] != "true" {
		t.Errorf(`Poll.Query["singleEvents"] = %q, want the literal %q`, trigger.Poll.Query["singleEvents"], "true")
	}
	if trigger.Poll.RecordsPath != "items" {
		t.Errorf("Poll.RecordsPath = %q, want %q", trigger.Poll.RecordsPath, "items")
	}
	if trigger.Poll.RecordIDPath != "id" {
		t.Errorf("Poll.RecordIDPath = %q, want %q", trigger.Poll.RecordIDPath, "id")
	}
	if trigger.Poll.RecordTimestampPath != "updated" {
		t.Errorf("Poll.RecordTimestampPath = %q, want %q", trigger.Poll.RecordTimestampPath, "updated")
	}
}

// TestDefaultProviderDefinitions_LoadsTheEmbeddedSlackDefinition is the
// Providers strand's Slice 3 boot-load AC: slack.yaml parses under the real
// strict loader and boot-loads purely as a definition file — no
// provider-specific Go code was added to make its two tools' mappings
// (JSON body, cursor pagination) parseable. It also pins slack.yaml's two
// deliberate deviations at the definition level: userInfoUrl/userInfo are
// entirely omitted (the format allows it — validateDefinitionFileV1 only
// requires oauth.authorizeUrl/tokenUrl) and Slack declares no trigger, so
// both UserInfoURL/UserInfo and Triggers must come back empty rather than
// failing to load or silently defaulting to some other provider's shape.
func TestDefaultProviderDefinitions_LoadsTheEmbeddedSlackDefinition(t *testing.T) {
	defs, err := catalog.DefaultProviderDefinitions()

	if err != nil {
		t.Fatalf("unexpected error loading the embedded provider definitions: %v", err)
	}
	slack, ok := findDefinitionBySlug(defs, "slack")
	if !ok {
		t.Fatalf("defs = %+v, want it to include the bundled Slack definition", defs)
	}
	if slack.Name != "Slack" {
		t.Errorf("Name = %q, want %q", slack.Name, "Slack")
	}
	if slack.AuthorizeURL != "https://slack.com/oauth/v2/authorize" {
		t.Errorf("AuthorizeURL = %q, want the Slack OAuth v2 authorize endpoint", slack.AuthorizeURL)
	}
	if slack.TokenURL != "https://slack.com/api/oauth.v2.access" {
		t.Errorf("TokenURL = %q, want the Slack OAuth v2 token endpoint", slack.TokenURL)
	}
	if slack.BaseURL != "https://slack.com/api" {
		t.Errorf("BaseURL = %q, want the Slack Web API base", slack.BaseURL)
	}
	for _, scope := range []string{"chat:write", "channels:read"} {
		if !slices.Contains(slack.Scopes, scope) {
			t.Errorf("Scopes = %v, want it to include %q", slack.Scopes, scope)
		}
	}
	if slack.UserInfoURL != "" {
		t.Errorf("UserInfoURL = %q, want empty — slack.yaml deliberately omits userInfoUrl (PD77 deviation)", slack.UserInfoURL)
	}
	if slack.UserInfo != (catalog.UserInfoMapping{}) {
		t.Errorf("UserInfo = %+v, want the zero value — slack.yaml declares no userInfo block", slack.UserInfo)
	}
	if len(slack.Triggers) != 0 {
		t.Errorf("Triggers = %+v, want an empty list — Slack ships no trigger in this strand (PD81 deviation)", slack.Triggers)
	}

	wantTools := map[string]bool{"slack-post-message": false, "slack-list-channels": false}
	for _, tool := range slack.Tools {
		if _, declared := wantTools[tool.Slug]; declared {
			wantTools[tool.Slug] = true
		}
		if len(tool.InputSchema) == 0 || len(tool.OutputSchema) == 0 {
			t.Errorf("tool %q has an empty input/output schema", tool.Slug)
		}
		if tool.Slug == "slack-post-message" {
			if tool.Method != "POST" || tool.Path != "/chat.postMessage" {
				t.Errorf("Method/Path = %s %s, want POST /chat.postMessage", tool.Method, tool.Path)
			}
			if tool.Mapping.Body["channel"] != "{input.channel}" {
				t.Errorf(`Mapping.Body["channel"] = %q, want %q`, tool.Mapping.Body["channel"], "{input.channel}")
			}
			if tool.Mapping.Body["text"] != "{input.text}" {
				t.Errorf(`Mapping.Body["text"] = %q, want %q`, tool.Mapping.Body["text"], "{input.text}")
			}
		}
		if tool.Slug == "slack-list-channels" {
			if tool.Mapping.Pagination == nil {
				t.Fatal("slack-list-channels declares no Mapping.Pagination, want one (PD15b)")
			}
			if tool.Mapping.Pagination.PageSizeParam != "limit" || tool.Mapping.Pagination.CursorParam != "cursor" {
				t.Errorf("Pagination = %+v, want PageSizeParam=limit, CursorParam=cursor", tool.Mapping.Pagination)
			}
			if tool.Mapping.Pagination.NextCursorPath != "response_metadata.next_cursor" {
				t.Errorf("NextCursorPath = %q, want %q", tool.Mapping.Pagination.NextCursorPath, "response_metadata.next_cursor")
			}
		}
	}
	for slug, found := range wantTools {
		if !found {
			t.Errorf("Tools = %+v, want it to include %q", slack.Tools, slug)
		}
	}
}

// TestDefaultProviderDefinitions_LoadsTheEmbeddedGitHubDefinition is the
// Providers strand's Slice 4 boot-load AC: github.yaml parses under the real
// strict loader and boot-loads purely as a definition file — no
// provider-specific Go code was added to make its three tools' mappings
// (query pagination, path templating, JSON body, and literal per-tool
// headers) parseable. It also pins github.yaml's own OAuth block (including
// the email->email/displayName->login userInfo mapping) and its deliberate
// omission of any trigger (PD84 deviation).
func TestDefaultProviderDefinitions_LoadsTheEmbeddedGitHubDefinition(t *testing.T) {
	defs, err := catalog.DefaultProviderDefinitions()

	if err != nil {
		t.Fatalf("unexpected error loading the embedded provider definitions: %v", err)
	}
	github, ok := findDefinitionBySlug(defs, "github")
	if !ok {
		t.Fatalf("defs = %+v, want it to include the bundled GitHub definition", defs)
	}
	if github.Name != "GitHub" {
		t.Errorf("Name = %q, want %q", github.Name, "GitHub")
	}
	if github.AuthorizeURL != "https://github.com/login/oauth/authorize" {
		t.Errorf("AuthorizeURL = %q, want the GitHub OAuth authorize endpoint", github.AuthorizeURL)
	}
	if github.TokenURL != "https://github.com/login/oauth/access_token" {
		t.Errorf("TokenURL = %q, want the GitHub OAuth token endpoint", github.TokenURL)
	}
	if github.UserInfoURL != "https://api.github.com/user" {
		t.Errorf("UserInfoURL = %q, want the GitHub account-fetch endpoint", github.UserInfoURL)
	}
	for _, scope := range []string{"repo", "read:user"} {
		if !slices.Contains(github.Scopes, scope) {
			t.Errorf("Scopes = %v, want it to include %q", github.Scopes, scope)
		}
	}
	if github.UserInfo.EmailField != "email" {
		t.Errorf("UserInfo.EmailField = %q, want %q", github.UserInfo.EmailField, "email")
	}
	if github.UserInfo.DisplayNameField != "login" {
		t.Errorf("UserInfo.DisplayNameField = %q, want %q", github.UserInfo.DisplayNameField, "login")
	}
	if github.BaseURL != "https://api.github.com" {
		t.Errorf("BaseURL = %q, want the GitHub API base", github.BaseURL)
	}
	if len(github.Triggers) != 0 {
		t.Errorf("Triggers = %+v, want an empty list — GitHub ships no trigger in this strand (PD84 deviation)", github.Triggers)
	}

	wantTools := map[string]bool{"github-list-repos": false, "github-list-issues": false, "github-create-issue": false}
	for _, tool := range github.Tools {
		if _, declared := wantTools[tool.Slug]; declared {
			wantTools[tool.Slug] = true
		}
		if len(tool.InputSchema) == 0 || len(tool.OutputSchema) == 0 {
			t.Errorf("tool %q has an empty input/output schema", tool.Slug)
		}
		if tool.Mapping.Header["User-Agent"] != "Beecon" {
			t.Errorf("tool %q Mapping.Header[User-Agent] = %q, want %q (PD84)", tool.Slug, tool.Mapping.Header["User-Agent"], "Beecon")
		}
		if tool.Mapping.Header["Accept"] != "application/vnd.github+json" {
			t.Errorf("tool %q Mapping.Header[Accept] = %q, want %q", tool.Slug, tool.Mapping.Header["Accept"], "application/vnd.github+json")
		}
		if tool.Slug == "github-list-issues" {
			if tool.Path != "/repos/{input.owner}/{input.repo}/issues" {
				t.Errorf("Path = %q, want the {input.owner}/{input.repo}-templated path", tool.Path)
			}
		}
		if tool.Slug == "github-create-issue" {
			if tool.Mapping.Body["title"] != "{input.title}" {
				t.Errorf(`Mapping.Body["title"] = %q, want %q`, tool.Mapping.Body["title"], "{input.title}")
			}
			if tool.Mapping.Body["body"] != "{input.body}" {
				t.Errorf(`Mapping.Body["body"] = %q, want %q`, tool.Mapping.Body["body"], "{input.body}")
			}
		}
	}
	for slug, found := range wantTools {
		if !found {
			t.Errorf("Tools = %+v, want it to include %q", github.Tools, slug)
		}
	}
}
