//go:build integration

// Package support: FakeHubspot is a scripted httptest.Server standing in for
// Hubspot's OAuth token endpoint, its token-metadata endpoint, and its CRM
// contacts endpoints — the upstream calls oauthhttp.Client and
// providerhttp.Client make during the OAuth callback and hubspot-list-
// contacts/hubspot-create-contact execution. Crucial-path journeys point a
// catalog.ProviderDefinition's TokenURL/UserInfoURL/BaseURL at this server
// instead of the real internet. Mirrors FakeMicrosoft's/FakeGraph's shape
// (fake_microsoft.go, fake_graph.go). There is no dedicated deny-consent
// endpoint here, same as FakeMicrosoft: a denied consent is exercised purely
// through the OAuth callback's own "error" query parameter — Beecon never
// calls the provider at all in that path.
package support

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// FakeHubspotContact is one fixture contact FakeHubspot's contacts-list
// endpoint pages over.
type FakeHubspotContact struct {
	ID         string
	Properties map[string]string
}

// FakeHubspotScript configures how FakeHubspot's endpoints respond.
type FakeHubspotScript struct {
	AccessToken  string
	RefreshToken string
	// AccountEmail and HubDomain are the token-metadata endpoint's "user" and
	// "hub_domain" fields respectively (PD16).
	AccountEmail string
	HubDomain    string

	// FailTokenExchange makes the token endpoint return 400, simulating the
	// provider rejecting the authorization code.
	FailTokenExchange bool
	// FailUserInfo makes the token-metadata endpoint return 401 after a
	// successful token exchange.
	FailUserInfo bool

	// Contacts is the fixed set of contacts hubspot-list-contacts pages over,
	// in page order.
	Contacts []FakeHubspotContact

	// CreateStatus, when non-zero, makes the create-contact endpoint return
	// this status (with CreateBody, if set) instead of a successful
	// creation — proves upstream errors surface as tool-level failures.
	CreateStatus int
	CreateBody   string
}

// FakeHubspot is a running fake Hubspot server plus the request details it
// observed, so a test can assert on what Beecon sent.
type FakeHubspot struct {
	// TokenURL is FakeHubspot's OAuth token endpoint
	// (.../oauth/v1/token).
	TokenURL string
	// UserInfoURLTemplate is FakeHubspot's token-metadata endpoint with an
	// {accessToken} placeholder, matching the real Hubspot API's
	// GET /oauth/v1/access-tokens/{token} shape (PD16) — set a
	// catalog.ProviderDefinition's UserInfoURL to this.
	UserInfoURLTemplate string
	// BaseURL is FakeHubspot's API base — set a catalog.ProviderDefinition's
	// BaseURL to this to exercise hubspot-list-contacts/hubspot-create-contact
	// against it.
	BaseURL string

	LastTokenForm         url.Values
	LastContactsQuery     url.Values
	LastCreateContactBody map[string]any
}

// NewFakeHubspot starts a FakeHubspot server scripted per script, and
// registers it for cleanup with t.
func NewFakeHubspot(t *testing.T, script FakeHubspotScript) *FakeHubspot {
	t.Helper()
	fh := &FakeHubspot{}

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/v1/token", fh.tokenHandler(script))
	mux.HandleFunc("/oauth/v1/access-tokens/", fh.userInfoHandler(script))
	mux.HandleFunc("/crm/v3/objects/contacts", fh.contactsHandler(script))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	fh.TokenURL = server.URL + "/oauth/v1/token"
	fh.UserInfoURLTemplate = server.URL + "/oauth/v1/access-tokens/{accessToken}"
	fh.BaseURL = server.URL
	return fh
}

func (fh *FakeHubspot) tokenHandler(script FakeHubspotScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if script.FailTokenExchange {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fh.LastTokenForm = r.Form
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token":  script.AccessToken,
			"refresh_token": script.RefreshToken,
		})
	}
}

func (fh *FakeHubspot) userInfoHandler(script FakeHubspotScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if script.FailUserInfo {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"user":       script.AccountEmail,
			"hub_domain": script.HubDomain,
		})
	}
}

func (fh *FakeHubspot) contactsHandler(script FakeHubspotScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			fh.LastContactsQuery = r.URL.Query()
			respondPagedContacts(w, script.Contacts, r.URL.Query())
		case http.MethodPost:
			fh.handleCreateContact(w, r, script)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func (fh *FakeHubspot) handleCreateContact(w http.ResponseWriter, r *http.Request, script FakeHubspotScript) {
	if script.CreateStatus != 0 {
		w.WriteHeader(script.CreateStatus)
		if script.CreateBody != "" {
			_, _ = w.Write([]byte(script.CreateBody))
		}
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	fh.LastCreateContactBody = body
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":         "contact-created-1",
		"properties": body["properties"],
	})
}

// respondPagedContacts serves one page of contacts starting at the "after"
// query parameter (an index into contacts, defaulting to 0) sized by the
// "limit" query parameter (defaulting to len(contacts)), carrying
// paging.next.after when a further page remains — the shape
// hubspot-list-contacts' declared pagination (limit/after ->
// paging.next.after) expects.
func respondPagedContacts(w http.ResponseWriter, contacts []FakeHubspotContact, query url.Values) {
	after := parseIntDefault(query.Get("after"), 0)
	limit := parseIntDefault(query.Get("limit"), len(contacts))
	if after < 0 {
		after = 0
	}
	if after > len(contacts) {
		after = len(contacts)
	}
	end := after + limit
	hasMore := end < len(contacts)
	if end > len(contacts) {
		end = len(contacts)
	}

	results := make([]map[string]any, 0, end-after)
	for _, contact := range contacts[after:end] {
		properties := make(map[string]any, len(contact.Properties))
		for k, v := range contact.Properties {
			properties[k] = v
		}
		results = append(results, map[string]any{"id": contact.ID, "properties": properties})
	}

	body := map[string]any{"results": results}
	if hasMore {
		body["paging"] = map[string]any{"next": map[string]any{"after": strconv.Itoa(end)}}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func parseIntDefault(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}
