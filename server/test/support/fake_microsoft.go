//go:build integration

// Package support: FakeMicrosoft is a scripted httptest.Server standing in
// for Microsoft's OAuth token endpoint and Graph's GET /v1.0/me — the two
// upstream calls oauthhttp.Client makes during the OAuth callback. Crucial-
// path journeys point a catalog.ProviderDefinition's TokenURL/UserInfoURL at
// this server instead of the real internet, scripted to happy/exchange-
// failure/user-info-failure outcomes.
package support

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// FakeMicrosoftScript configures how FakeMicrosoft's token and user-info
// endpoints respond.
type FakeMicrosoftScript struct {
	AccessToken        string
	RefreshToken       string
	AccountEmail       string
	AccountDisplayName string

	// FailTokenExchange makes the token endpoint return 400, simulating the
	// provider rejecting the authorization code (AC9).
	FailTokenExchange bool
	// FailUserInfo makes the user-info endpoint return 401 after a
	// successful token exchange, the callback's other failure mode (AC9).
	FailUserInfo bool
}

// FakeMicrosoft is a running fake Microsoft/Graph server plus the request
// details it observed, so a test can assert on what oauthhttp.Client sent.
type FakeMicrosoft struct {
	TokenURL    string
	UserInfoURL string

	LastTokenForm          url.Values
	LastUserInfoAuthHeader string
}

// NewFakeMicrosoft starts a FakeMicrosoft server scripted per script, and
// registers it for cleanup with t.
func NewFakeMicrosoft(t *testing.T, script FakeMicrosoftScript) *FakeMicrosoft {
	t.Helper()
	fm := &FakeMicrosoft{}

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		if script.FailTokenExchange {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fm.LastTokenForm = r.Form
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token":  script.AccessToken,
			"refresh_token": script.RefreshToken,
		})
	})
	mux.HandleFunc("/v1.0/me", func(w http.ResponseWriter, r *http.Request) {
		if script.FailUserInfo {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fm.LastUserInfoAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"mail":        script.AccountEmail,
			"displayName": script.AccountDisplayName,
		})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	fm.TokenURL = server.URL + "/oauth2/v2.0/token"
	fm.UserInfoURL = server.URL + "/v1.0/me"
	return fm
}
