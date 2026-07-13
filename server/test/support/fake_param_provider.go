//go:build integration

// Package support: FakeParamProvider is a scripted httptest.Server standing
// in for a fictional OAuth provider whose definition declares expectedParams
// (Slice 3, AC1, AC8) — proving {params.x} templating end to end in a
// provider's OAuth URLs and a tool's mapping, since neither of Beecon's two
// real providers (Outlook, Hubspot) needs pre-auth params. The fixture is
// modeled as a region-scoped API (e.g. Zendesk/Freshdesk-style subdomains):
// every endpoint lives under /{region}/..., where {region} is the value
// collected for the definition's "region" expected param, and its one tool
// also carries a secret "apiKey" expected param forwarded as a header —
// proving both a non-secret and a secret expected param reach a live call.
package support

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"beecon/internal/catalog"
)

// FakeParamProviderScript configures how FakeParamProvider's endpoints
// respond.
type FakeParamProviderScript struct {
	AccessToken  string
	RefreshToken string
	AccountEmail string
	AccountName  string

	// WidgetName is what the fake tool endpoint returns for the requested
	// widget id, proving a tool call actually reached the provider rather
	// than merely rendering a request.
	WidgetName string
}

// FakeParamProvider is a running fake region-scoped OAuth+tool provider plus
// the request details it observed, so a test can assert the collected param
// values actually reached the provider (not just that Beecon parsed the
// {params.x} tokens).
type FakeParamProvider struct {
	// BaseURL is the fake server's own base, with no region segment — a
	// FakeParamProviderDefinition builds every region-scoped URL from it.
	BaseURL string

	LastTokenRegion        string
	LastTokenForm          url.Values
	LastUserInfoAuthHeader string
	LastWidgetRegion       string
	LastWidgetAPIKeyHeader string
	LastWidgetAuthHeader   string
}

// NewFakeParamProvider starts a FakeParamProvider server scripted per script,
// and registers it for cleanup with t.
func NewFakeParamProvider(t *testing.T, script FakeParamProviderScript) *FakeParamProvider {
	t.Helper()
	fp := &FakeParamProvider{}

	mux := http.NewServeMux()
	mux.HandleFunc("/", fp.dispatch(script))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	fp.BaseURL = server.URL
	return fp
}

// dispatch routes every request by its /{region}/... path shape: nothing
// here is a real provider's actual API, so a single handler parsing the path
// is enough to stand in for the token endpoint, the user-info endpoint, and
// the one fake tool.
func (fp *FakeParamProvider) dispatch(script FakeParamProviderScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(segments) < 2 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		region := segments[0]
		switch {
		case len(segments) == 3 && segments[1] == "oauth" && segments[2] == "token":
			fp.handleToken(w, r, region, script)
		case len(segments) == 3 && segments[1] == "user" && segments[2] == "me":
			fp.handleUserInfo(w, r, region, script)
		case len(segments) == 3 && segments[1] == "widgets":
			fp.handleWidget(w, r, region, segments[2], script)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (fp *FakeParamProvider) handleToken(w http.ResponseWriter, r *http.Request, region string, script FakeParamProviderScript) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	fp.LastTokenRegion = region
	fp.LastTokenForm = r.Form
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"access_token":  script.AccessToken,
		"refresh_token": script.RefreshToken,
	})
}

func (fp *FakeParamProvider) handleUserInfo(w http.ResponseWriter, r *http.Request, _ string, script FakeParamProviderScript) {
	fp.LastUserInfoAuthHeader = r.Header.Get("Authorization")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"email": script.AccountEmail,
		"name":  script.AccountName,
	})
}

func (fp *FakeParamProvider) handleWidget(w http.ResponseWriter, r *http.Request, region, widgetID string, script FakeParamProviderScript) {
	fp.LastWidgetRegion = region
	fp.LastWidgetAPIKeyHeader = r.Header.Get("X-Api-Key")
	fp.LastWidgetAuthHeader = r.Header.Get("Authorization")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":   widgetID,
		"name": script.WidgetName,
	})
}

// FakeParamProviderDefinition returns the test-fixture catalog.ProviderDefinition
// pointed at fp: it declares one required, non-secret expected param
// ("region") and one required, secret expected param ("apiKey", AC5), and
// templates both via {params.x} — "region" into every OAuth URL and the
// fake tool's path (AC8), "apiKey" into the fake tool's header mapping —
// proving collected values are usable in both OAuth URLs and tool mappings
// end to end, not just parsed.
func FakeParamProviderDefinition(fp *FakeParamProvider) catalog.ProviderDefinition {
	return catalog.ProviderDefinition{
		Slug:         "fake-param-provider",
		Name:         "Fake Param Provider",
		Logo:         "https://static.beecon.dev/providers/fake-param-provider.png",
		AuthScheme:   "oauth2",
		BaseURL:      fp.BaseURL,
		AuthorizeURL: fp.BaseURL + "/{params.region}/oauth/authorize",
		TokenURL:     fp.BaseURL + "/{params.region}/oauth/token",
		UserInfoURL:  fp.BaseURL + "/{params.region}/user/me",
		Scopes:       []string{"widgets.read"},
		UserInfo:     catalog.UserInfoMapping{EmailField: "email", DisplayNameField: "name"},
		ExpectedParams: []catalog.ExpectedParam{
			{Name: "region", DisplayName: "Region", Description: "Your account's region, e.g. eu or us.", Required: true, Secret: false},
			{Name: "apiKey", DisplayName: "API Key", Description: "Your account's API key.", Required: true, Secret: true},
		},
		Tools: []catalog.ProviderTool{
			{
				Slug:        "fake-param-provider-get-widget",
				Name:        "Get widget",
				Description: "Fetch one widget by id — proves {params.x} templating reaches a tool's path and header mapping.",
				Method:      "GET",
				Path:        "/{params.region}/widgets/{input.widgetId}",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"widgetId": map[string]any{"type": "string"},
					},
					"required": []any{"widgetId"},
				},
				OutputSchema: map[string]any{"type": "object"},
				Mapping: catalog.Mapping{
					Header: map[string]string{"X-Api-Key": "{params.apiKey}"},
				},
			},
		},
	}
}
