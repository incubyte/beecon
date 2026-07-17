package membraneimport

import (
	"reflect"
	"strings"
	"testing"

	"beecon/internal/catalog"
)

// Real preset values below are copied verbatim from the shipped Beecon
// provider definitions the Slice 3 preset table claims to mirror --
// server/internal/catalog/providers/{outlook,hubspot,gmail,google-calendar}.yaml
// -- so a failure here means the preset table (preset.go) has drifted from
// the provider files it is supposed to match.
const (
	realOutlookAuthorizeURL = "https://login.microsoftonline.com/common/oauth2/v2.0/authorize"
	realOutlookTokenURL     = "https://login.microsoftonline.com/common/oauth2/v2.0/token"
	realOutlookUserInfoURL  = "https://graph.microsoft.com/v1.0/me"
	realOutlookBaseURL      = "https://graph.microsoft.com/v1.0"

	realHubspotAuthorizeURL = "https://app.hubspot.com/oauth/authorize"
	realHubspotTokenURL     = "https://api.hubapi.com/oauth/v1/token"
	realHubspotUserInfoURL  = "https://api.hubapi.com/oauth/v1/access-tokens/{accessToken}"
	realHubspotBaseURL      = "https://api.hubapi.com"

	// realGoogleAuthorizeURL/TokenURL/UserInfoURL are verbatim identical in
	// both gmail.yaml and google-calendar.yaml (each file's own comment calls
	// this out as their "shared Google OAuth block") -- only scopes and
	// baseUrl differ per Google product, which is why the generic "google"
	// preset match below only asserts the OpenID-subset scopes (see the
	// comment on realGoogleOpenIDScopes) and leaves baseUrl as a TODO rather
	// than guessing between gmail.googleapis.com and
	// www.googleapis.com/calendar/v3.
	realGoogleAuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	realGoogleTokenURL     = "https://oauth2.googleapis.com/token"
	realGoogleUserInfoURL  = "https://www.googleapis.com/oauth2/v3/userinfo"
)

var (
	realOutlookScopes = []string{"offline_access", "Mail.Read", "User.Read"}
	realHubspotScopes = []string{"crm.objects.contacts.read", "crm.objects.contacts.write", "files"}
	// realGoogleOpenIDScopes is the OpenID Connect subset gmail.yaml and
	// google-calendar.yaml declare identically; each file additionally
	// declares its own product-specific scope (gmail.readonly/gmail.send,
	// calendar.events respectively) that a generic "google" connector match
	// (as opposed to a Gmail- or Calendar-specific one) cannot know to add.
	realGoogleOpenIDScopes = []string{"openid", "email", "profile"}
)

// TestConvert_MicrosoftConnectorFillsRealOutlookOAuthAndBaseURL is Slice 3's
// AC1 for the Microsoft/Outlook preset: a connector matched via its key
// ("2-outlook-mail-connector" contains "outlook") emits the real known
// Outlook/Graph OAuth endpoints and baseUrl -- no TODO placeholders -- and
// the definition still parses through the real strict loader.
func TestConvert_MicrosoftConnectorFillsRealOutlookOAuthAndBaseURL(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration-microsoft.yaml"),
		loadTestdataFile(t, "action-microsoft-list-messages.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	def := loadSingleEmittedDefinition(t, result)
	assertPresetOAuthFilled(t, def, realOutlookAuthorizeURL, realOutlookTokenURL, realOutlookUserInfoURL, realOutlookScopes, realOutlookBaseURL)
	assertNoOAuthTODOCaveats(t, result)
}

// TestConvert_HubspotConnectorFillsRealHubspotOAuthAndBaseURL is Slice 3's
// AC1 for the HubSpot preset, fed the real Membrane sample
// (temp/outlook-integration.yaml, reproduced verbatim as
// testdata/integration-hubspot.yaml): despite the sample's misleading
// filename, its actual key ("1-hubspot-all-in-one") matches the hubspot
// preset and the emitted definition carries the real HubSpot OAuth
// endpoints and baseUrl.
func TestConvert_HubspotConnectorFillsRealHubspotOAuthAndBaseURL(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration-hubspot.yaml"),
		loadTestdataFile(t, "action-hubspot-get-contact.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	def := loadSingleEmittedDefinition(t, result)
	assertPresetOAuthFilled(t, def, realHubspotAuthorizeURL, realHubspotTokenURL, realHubspotUserInfoURL, realHubspotScopes, realHubspotBaseURL)
	assertNoOAuthTODOCaveats(t, result)
}

// TestConvert_GoogleConnectorFillsRealGoogleOAuthButTODOsBaseURL is Slice
// 3's AC1 for the Google preset, corrected: Google's OAuth
// authorize/token/userInfo endpoints and OpenID scopes are real, shared,
// and correct across every Google product (identical in gmail.yaml and
// google-calendar.yaml), so a connector matched via its key
// ("3-gmail-connector" contains "gmail") still emits those real values, not
// TODOs. But Google's API base genuinely varies by product -- Gmail is
// https://gmail.googleapis.com/gmail/v1, Calendar is
// https://www.googleapis.com/calendar/v3 -- so no single baseUrl is correct
// for a generic Google match. Emitting a plausible-looking prefix like
// "https://www.googleapis.com" silently would be wrong for Gmail and worse
// than an honest TODO, so the preset instead leaves mapping.baseUrl as the
// TODO placeholder with its own caveat in the Partial section.
func TestConvert_GoogleConnectorFillsRealGoogleOAuthButTODOsBaseURL(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration-google.yaml"),
		loadTestdataFile(t, "action-google-list-messages.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	def := loadSingleEmittedDefinition(t, result)
	if def.AuthorizeURL != realGoogleAuthorizeURL {
		t.Errorf("AuthorizeURL = %q, want the real Google preset value %q", def.AuthorizeURL, realGoogleAuthorizeURL)
	}
	if def.TokenURL != realGoogleTokenURL {
		t.Errorf("TokenURL = %q, want the real Google preset value %q", def.TokenURL, realGoogleTokenURL)
	}
	if def.UserInfoURL != realGoogleUserInfoURL {
		t.Errorf("UserInfoURL = %q, want the real Google preset value %q", def.UserInfoURL, realGoogleUserInfoURL)
	}
	if !reflect.DeepEqual(def.Scopes, realGoogleOpenIDScopes) {
		t.Errorf("Scopes = %v, want the real Google preset value %v", def.Scopes, realGoogleOpenIDScopes)
	}
	if def.AuthScheme != "oauth2" {
		t.Errorf("AuthScheme = %q, want %q (the importer emits none; the loader defaults it)", def.AuthScheme, "oauth2")
	}

	if def.BaseURL != todoBaseURL {
		t.Errorf("BaseURL = %q, want the TODO placeholder %q -- Google's API base is product-specific, so no single preset value is correct", def.BaseURL, todoBaseURL)
	}

	partialSection := reportSection(t, string(result.Report), "Partial")
	if !strings.Contains(partialSection, todoBaseURLCaveat) {
		t.Errorf("Partial section missing the baseUrl TODO caveat for the google preset's product-specific API base:\n%s", partialSection)
	}
	for _, caveat := range []string{todoAuthorizeURLCaveat, todoTokenURLCaveat, todoUserInfoURLCaveat, todoScopesCaveat} {
		if strings.Contains(partialSection, caveat) {
			t.Errorf("Partial section unexpectedly names TODO caveat %q -- the google preset's OAuth endpoints/scopes are real, known values:\n%s", caveat, partialSection)
		}
	}
}

// TestConvert_RenamedIntegrationNameStillMatchesPresetByKey is Slice 3's
// AC3: an integration whose free-text "name" names no known provider at all
// ("My Custom Business App") still matches the HubSpot preset because its
// "key" carries the alias ("hubspot-crm-sync-connector") -- proving the
// match never reads "name".
func TestConvert_RenamedIntegrationNameStillMatchesPresetByKey(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration-renamed-connector.yaml"),
		loadTestdataFile(t, "action-renamed-connector-get-contact.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	def := loadSingleEmittedDefinition(t, result)
	if def.Name != "My Custom Business App" {
		t.Fatalf("Name = %q, want the fixture's misleading display name preserved, %q", def.Name, "My Custom Business App")
	}
	assertPresetOAuthFilled(t, def, realHubspotAuthorizeURL, realHubspotTokenURL, realHubspotUserInfoURL, realHubspotScopes, realHubspotBaseURL)
	assertNoOAuthTODOCaveats(t, result)
}

// assertPresetOAuthFilled asserts one emitted, loaded definition carries the
// exact known-provider preset values (never a TODO placeholder) and that the
// loader's authScheme default (oauth2, since the importer emits none) held.
func assertPresetOAuthFilled(t *testing.T, def catalog.ProviderDefinition, wantAuthorize, wantToken, wantUserInfo string, wantScopes []string, wantBaseURL string) {
	t.Helper()
	if def.AuthorizeURL != wantAuthorize {
		t.Errorf("AuthorizeURL = %q, want %q", def.AuthorizeURL, wantAuthorize)
	}
	if def.TokenURL != wantToken {
		t.Errorf("TokenURL = %q, want %q", def.TokenURL, wantToken)
	}
	if def.UserInfoURL != wantUserInfo {
		t.Errorf("UserInfoURL = %q, want %q", def.UserInfoURL, wantUserInfo)
	}
	if !reflect.DeepEqual(def.Scopes, wantScopes) {
		t.Errorf("Scopes = %v, want %v", def.Scopes, wantScopes)
	}
	if def.BaseURL != wantBaseURL {
		t.Errorf("BaseURL = %q, want %q", def.BaseURL, wantBaseURL)
	}
	if def.AuthScheme != "oauth2" {
		t.Errorf("AuthScheme = %q, want %q (the importer emits none; the loader defaults it)", def.AuthScheme, "oauth2")
	}
	for label, got := range map[string]string{
		"AuthorizeURL": def.AuthorizeURL,
		"TokenURL":     def.TokenURL,
		"UserInfoURL":  def.UserInfoURL,
		"BaseURL":      def.BaseURL,
	} {
		if strings.Contains(got, "TODO") {
			t.Errorf("%s = %q still contains a TODO placeholder for a preset-matched connector", label, got)
		}
	}
}

// assertNoOAuthTODOCaveats asserts a preset-matched run's Partial section
// carries none of the five Slice 3 TODO-fallback caveat lines -- a matched
// connector leaves nothing OAuth/baseUrl-related for a human to fill in.
// (The Partial section's standing disclosure note also mentions "TODO
// placeholder" generically, so this checks the specific per-field caveat
// text rather than that phrase alone.)
func assertNoOAuthTODOCaveats(t *testing.T, result Result) {
	t.Helper()
	partialSection := reportSection(t, string(result.Report), "Partial")
	for _, caveat := range todoOAuthCaveats() {
		if strings.Contains(partialSection, caveat) {
			t.Errorf("Partial section unexpectedly names TODO caveat %q for a preset-matched connector:\n%s", caveat, partialSection)
		}
	}
}
