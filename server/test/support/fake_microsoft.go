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
	// ExpiresIn is the authorization_code grant's expires_in (PD18), left
	// unset (0) when a test doesn't care about token expiry — the connections
	// module then assumes a conservative default TTL rather than treating the
	// token as already expired.
	ExpiresIn int

	// FailTokenExchange makes the token endpoint return 400, simulating the
	// provider rejecting the authorization code (AC9).
	FailTokenExchange bool
	// FailUserInfo makes the user-info endpoint return 401 after a
	// successful token exchange, the callback's other failure mode (AC9).
	FailUserInfo bool

	// RefreshAccessToken and RefreshRefreshToken, when set, are what a
	// refresh_token grant returns instead of AccessToken/RefreshToken (Slice
	// 4, AC7, AC8): RefreshRefreshToken left empty simulates a provider that
	// does not rotate the refresh token — the connections module keeps the
	// one it already has; set it to prove a rotated refresh token replaces
	// the stored one.
	RefreshAccessToken  string
	RefreshRefreshToken string
	// FailRefresh makes a refresh_token grant return 400 invalid_grant,
	// simulating a revoked refresh token (Slice 4, AC9).
	FailRefresh bool
}

// FakeMicrosoft is a running fake Microsoft/Graph server plus the request
// details it observed, so a test can assert on what oauthhttp.Client sent.
type FakeMicrosoft struct {
	TokenURL    string
	UserInfoURL string

	LastTokenForm          url.Values
	LastUserInfoAuthHeader string
	RefreshCallCount       int
}

// NewFakeMicrosoft starts a FakeMicrosoft server scripted per script, and
// registers it for cleanup with t.
func NewFakeMicrosoft(t *testing.T, script FakeMicrosoftScript) *FakeMicrosoft {
	t.Helper()
	fm := &FakeMicrosoft{}

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/v2.0/token", fm.tokenHandler(script))
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

// tokenHandler serves both the authorization_code grant (the OAuth
// callback's token exchange) and the refresh_token grant (Slice 4, PD18),
// distinguished by the form's own grant_type — scripted independently via
// FailTokenExchange/FailRefresh and the Refresh* fields.
func (fm *FakeMicrosoft) tokenHandler(script FakeMicrosoftScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		isRefresh := r.Form.Get("grant_type") == "refresh_token"

		if isRefresh && script.FailRefresh {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		if !isRefresh && script.FailTokenExchange {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		fm.LastTokenForm = r.Form
		accessToken, refreshToken := script.AccessToken, script.RefreshToken
		if isRefresh {
			fm.RefreshCallCount++
			if script.RefreshAccessToken != "" {
				accessToken = script.RefreshAccessToken
			}
			refreshToken = script.RefreshRefreshToken
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  accessToken,
			"refresh_token": refreshToken,
			"expires_in":    script.ExpiresIn,
		})
	}
}
