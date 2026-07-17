//go:build integration

// Package support: FakeSlack is a scripted httptest.Server standing in for
// Slack's OAuth v2 token endpoint and its Web API (chat.postMessage,
// conversations.list) — the upstream calls oauthhttp.Client and
// providerhttp.Client make during the OAuth callback and
// slack-post-message/slack-list-channels execution. Crucial-path journeys
// point a catalog.ProviderDefinition's TokenURL/BaseURL at this server
// instead of the real internet. Mirrors FakeHubspot's/FakeGoogle's shape
// (fake_hubspot.go, fake_google.go). Unlike those fakes, FakeSlack serves no
// user-info/token-metadata endpoint at all — slack.yaml deliberately omits
// userInfoUrl (the documented deviation: Slack's account identity lives
// behind a separate, differently-shaped users.identity call the finalized
// format's flat userInfo mapping cannot express).
package support

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// FakeSlackChannel is one fixture channel FakeSlack's conversations.list
// endpoint pages over.
type FakeSlackChannel struct {
	ID        string
	Name      string
	IsChannel bool
	IsPrivate bool
}

// FakeSlackScript configures how FakeSlack's endpoints respond.
type FakeSlackScript struct {
	AccessToken string

	// FailTokenExchange makes the token endpoint return a non-200 status,
	// simulating the provider rejecting the authorization code.
	FailTokenExchange bool

	// Channels is the fixed set of channels slack-list-channels pages over,
	// in page order.
	Channels []FakeSlackChannel

	// PostMessageError, when non-empty, makes chat.postMessage respond HTTP
	// 200 (Slack's real API never returns a non-2xx status, PD77's
	// documented deviation) with {"ok":false,"error":PostMessageError}
	// instead of a successful post.
	PostMessageError string
}

// FakeSlack is a running fake Slack server plus the request details it
// observed, so a test can assert on what Beecon sent.
type FakeSlack struct {
	// TokenURL is FakeSlack's OAuth v2 token endpoint (.../oauth.v2.access).
	TokenURL string
	// BaseURL is FakeSlack's Web API base — set a catalog.ProviderDefinition's
	// BaseURL to this to exercise slack-post-message/slack-list-channels
	// against it.
	BaseURL string

	LastTokenForm url.Values

	// LastPostMessageAuthorizationHeader is the bearer header
	// chat.postMessage observed, so a test can assert the connection's
	// access token reached Slack as a bearer credential.
	LastPostMessageAuthorizationHeader string
	// LastPostMessageBody is the most recent decoded JSON body posted to
	// chat.postMessage, so a test can assert the channel/text mapping built
	// the JSON body Slack's Web API expects (not form-encoded).
	LastPostMessageBody  map[string]any
	PostMessageCallCount int

	LastChannelsQuery url.Values
	ChannelsCallCount int
}

// NewFakeSlack starts a FakeSlack server scripted per script, and registers
// it for cleanup with t.
func NewFakeSlack(t *testing.T, script FakeSlackScript) *FakeSlack {
	t.Helper()
	fs := &FakeSlack{}

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth.v2.access", fs.tokenHandler(script))
	mux.HandleFunc("/chat.postMessage", fs.postMessageHandler(script))
	mux.HandleFunc("/conversations.list", fs.conversationsListHandler(script))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	fs.TokenURL = server.URL + "/oauth.v2.access"
	fs.BaseURL = server.URL
	return fs
}

// tokenHandler serves the authorization_code grant (the OAuth callback's
// token exchange) — mirrors FakeGoogle's tokenHandler; Slack bot tokens
// typically carry no refresh_token (the definition's other documented
// deviation), so this never returns one.
func (fs *FakeSlack) tokenHandler(script FakeSlackScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if script.FailTokenExchange {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fs.LastTokenForm = r.Form
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":           true,
			"access_token": script.AccessToken,
		})
	}
}

// postMessageHandler serves POST /chat.postMessage (slack-post-message):
// decodes the bearer-authenticated JSON body, and either echoes back a
// genuine {ok:true,channel,ts} success or, when script.PostMessageError is
// set, Slack's own documented failure shape — HTTP 200 carrying
// {ok:false,error} (PD77's deviation, not a tool-level HTTP failure).
func (fs *FakeSlack) postMessageHandler(script FakeSlackScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fs.PostMessageCallCount++
		fs.LastPostMessageAuthorizationHeader = r.Header.Get("Authorization")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fs.LastPostMessageBody = body

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if script.PostMessageError != "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": script.PostMessageError,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"channel": body["channel"],
			"ts":      "1720000000.000100",
		})
	}
}

// conversationsListHandler serves GET /conversations.list
// (slack-list-channels): pages script.Channels by the canonical
// pageSize/cursor convention mapped onto Slack's own limit/cursor query
// parameters, carrying response_metadata.next_cursor when a further page
// remains — mirrors respondPagedContacts/respondPagedGmailMessages for
// Slack's own shape.
func (fs *FakeSlack) conversationsListHandler(script FakeSlackScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fs.ChannelsCallCount++
		fs.LastChannelsQuery = r.URL.Query()

		offset := parseIntDefault(r.URL.Query().Get("cursor"), 0)
		limit := parseIntDefault(r.URL.Query().Get("limit"), len(script.Channels))
		if offset < 0 {
			offset = 0
		}
		if offset > len(script.Channels) {
			offset = len(script.Channels)
		}
		end := offset + limit
		hasMore := end < len(script.Channels)
		if end > len(script.Channels) {
			end = len(script.Channels)
		}

		channels := make([]map[string]any, 0, end-offset)
		for _, c := range script.Channels[offset:end] {
			channels = append(channels, map[string]any{
				"id":         c.ID,
				"name":       c.Name,
				"is_channel": c.IsChannel,
				"is_private": c.IsPrivate,
			})
		}
		responseMetadata := map[string]any{"next_cursor": ""}
		if hasMore {
			responseMetadata["next_cursor"] = strconv.Itoa(end)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":                true,
			"channels":          channels,
			"response_metadata": responseMetadata,
		})
	}
}
