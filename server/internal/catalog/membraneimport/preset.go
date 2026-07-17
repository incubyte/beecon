package membraneimport

import "strings"

// providerPreset is one entry of the Slice 3 known-provider preset table:
// the real OAuth authorize/token/userInfo URLs, scopes, and API base URL a
// recognized Membrane connector maps onto. Every value below is copied from
// the provider's own shipped Beecon definition under
// server/internal/catalog/providers/ (outlook.yaml, hubspot.yaml,
// gmail.yaml/google-calendar.yaml's shared Google OAuth block) — never
// invented — so a preset-filled definition matches what a human would have
// hand-authored.
type providerPreset struct {
	// aliases are lowercase substrings this preset matches against a
	// connector's key/connectorUuid/logoUri (never its free-text name, per
	// the spec's AC3 — a renamed integration must still match its preset).
	aliases      []string
	authorizeURL string
	tokenURL     string
	userInfoURL  string
	scopes       []string
	// baseURL is the provider's single, correct API base — left empty for a
	// preset whose OAuth block is real and shared but whose API base
	// genuinely varies by product (the google preset below). An empty
	// baseURL is this table's sentinel: resolveOAuthAndMapping routes it
	// through the TODO baseUrl path (plus that field's own caveat) instead
	// of emitting a plausible-looking but potentially wrong value.
	baseURL string
}

// knownProviderPresets is the three providers the spec names: microsoft
// (Outlook/Graph), hubspot, and google (Gmail/Calendar's shared Google OAuth
// block). Checked in order; the first alias match wins.
var knownProviderPresets = []providerPreset{
	{
		aliases:      []string{"microsoft", "outlook", "office365"},
		authorizeURL: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		tokenURL:     "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		userInfoURL:  "https://graph.microsoft.com/v1.0/me",
		scopes:       []string{"offline_access", "Mail.Read", "User.Read"},
		baseURL:      "https://graph.microsoft.com/v1.0",
	},
	{
		aliases:      []string{"hubspot"},
		authorizeURL: "https://app.hubspot.com/oauth/authorize",
		tokenURL:     "https://api.hubapi.com/oauth/v1/token",
		userInfoURL:  "https://api.hubapi.com/oauth/v1/access-tokens/{accessToken}",
		scopes:       []string{"crm.objects.contacts.read", "crm.objects.contacts.write", "files"},
		baseURL:      "https://api.hubapi.com",
	},
	{
		aliases:      []string{"google", "gmail", "gsuite"},
		authorizeURL: "https://accounts.google.com/o/oauth2/v2/auth",
		tokenURL:     "https://oauth2.googleapis.com/token",
		userInfoURL:  "https://www.googleapis.com/oauth2/v3/userinfo",
		scopes:       []string{"openid", "email", "profile"},
		// baseURL is deliberately omitted: unlike Microsoft/HubSpot, Google's
		// API base genuinely varies by product (Gmail is
		// https://gmail.googleapis.com/gmail/v1, Calendar is
		// https://www.googleapis.com/calendar/v3, ...). No single value is
		// correct for a generic "google"/"gmail"/"gsuite" match, so this
		// preset leaves baseURL as the zero value and lets
		// resolveOAuthAndMapping TODO it instead of guessing.
	},
}

// matchProviderPreset finds the known-provider preset for one Membrane
// integration record's connector identity: its key, connectorUuid, and
// logoUri joined and lowercased. It deliberately never looks at the
// integration's free-text "name" field (AC3), so an installation that
// renamed its integration's display name still matches the same preset.
// Returns the zero value and false when no preset recognizes the connector.
func matchProviderPreset(integrationFields map[string]any) (providerPreset, bool) {
	identity := strings.ToLower(strings.Join([]string{
		stringAt(integrationFields, "key"),
		stringAt(integrationFields, "connectorUuid"),
		stringAt(integrationFields, "logoUri"),
	}, " "))

	for _, preset := range knownProviderPresets {
		for _, alias := range preset.aliases {
			if strings.Contains(identity, alias) {
				return preset, true
			}
		}
	}
	return providerPreset{}, false
}

// resolveOAuthAndMapping fills a provider definition's oauth block and
// mapping.baseUrl from the known-provider preset table when the
// integration's connector identity matches one (AC1: real values, no
// TODOs). A matched preset whose baseURL is empty (the google preset: a
// real, shared OAuth block but a product-specific API base) still TODOs
// just mapping.baseUrl, with only that field's caveat. An unrecognized
// connector falls back to TODO placeholders for the whole block (AC2). The
// returned caveats name every placeholder field for the report's Partial
// section — empty when a preset filled everything, since nothing was left
// for a human to fill.
func resolveOAuthAndMapping(integrationFields map[string]any) (outputOAuthV1, outputProviderMappingV1, []string) {
	preset, ok := matchProviderPreset(integrationFields)
	if !ok {
		oauth := outputOAuthV1{
			AuthorizeURL: todoAuthorizeURL,
			TokenURL:     todoTokenURL,
			UserInfoURL:  todoUserInfoURL,
			Scopes:       []string{todoScope},
		}
		return oauth, outputProviderMappingV1{BaseURL: todoBaseURL}, todoOAuthCaveats()
	}

	oauth := outputOAuthV1{
		AuthorizeURL: preset.authorizeURL,
		TokenURL:     preset.tokenURL,
		UserInfoURL:  preset.userInfoURL,
		Scopes:       append([]string(nil), preset.scopes...),
	}
	if preset.baseURL == "" {
		return oauth, outputProviderMappingV1{BaseURL: todoBaseURL}, []string{todoBaseURLCaveat}
	}
	return oauth, outputProviderMappingV1{BaseURL: preset.baseURL}, nil
}

// The todo*Caveat constants each name one specific OAuth/baseUrl field left
// as a TODO placeholder — for the report's Partial section — rather than one
// generic "needs OAuth" note. todoOAuthCaveats composes all five for the
// AC2 unrecognized-connector fallback; a preset with a product-specific API
// base (the google preset) uses todoBaseURLCaveat alone, since its other
// four fields are real, known values.
const (
	todoAuthorizeURLCaveat = "oauth.authorizeUrl is a TODO placeholder — confirm against the provider's own developer docs"
	todoTokenURLCaveat     = "oauth.tokenUrl is a TODO placeholder — confirm against the provider's own developer docs"
	todoUserInfoURLCaveat  = "oauth.userInfoUrl is a TODO placeholder — confirm against the provider's own developer docs"
	todoScopesCaveat       = "oauth.scopes is a TODO placeholder — confirm against the provider's own developer docs"
	todoBaseURLCaveat      = "mapping.baseUrl is a TODO placeholder — confirm against the provider's own developer docs"
)

func todoOAuthCaveats() []string {
	return []string{
		todoAuthorizeURLCaveat,
		todoTokenURLCaveat,
		todoUserInfoURLCaveat,
		todoScopesCaveat,
		todoBaseURLCaveat,
	}
}
