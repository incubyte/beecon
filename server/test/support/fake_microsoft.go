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
	"sync"
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

// RefreshOutcome describes one scripted refresh_token grant response,
// consumed FIFO from FakeMicrosoft's queue (Phase 3 Slice 5, PD36/FD3) —
// letting a single crucial_path journey drive a scheduled refresh through
// success/rotated/invalid_grant/network-drop/5xx outcomes in sequence,
// something the static FailRefresh/RefreshAccessToken fields (a fixed
// behavior for the server's whole lifetime) cannot express. Exactly one
// field group is meaningful per outcome:
//   - success (the zero value of the failure flags): AccessToken/
//     RefreshToken/ExpiresIn are returned; an empty RefreshToken means the
//     provider did not rotate it.
//   - InvalidGrant: the token endpoint returns 400 {"error":"invalid_grant"}
//     — connections.RefreshDenied, the only permanent refusal (FD3).
//   - ServerError: the token endpoint returns 500 with no OAuth-shaped error
//     body — a transient failure (permanentRefreshErrorCode can't parse it).
//   - NetworkDrop: the connection is hijacked and closed with no response at
//     all — a transient network-level failure.
type RefreshOutcome struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int

	InvalidGrant bool
	ServerError  bool
	NetworkDrop  bool
}

// FakeMicrosoft is a running fake Microsoft/Graph server plus the request
// details it observed, so a test can assert on what oauthhttp.Client sent.
// mu guards every mutable field below, including the two scripted-outcome
// queues (QueueRefreshOutcomes/QueueUserInfoProbeStatuses): the token-
// self-heal journey's concurrent-execute-and-scheduled-refresh scenario
// calls into this fake from more than one goroutine at once.
type FakeMicrosoft struct {
	TokenURL    string
	UserInfoURL string

	mu                     sync.Mutex
	LastTokenForm          url.Values
	LastUserInfoAuthHeader string
	RefreshCallCount       int

	refreshOutcomes       []RefreshOutcome
	userInfoProbeStatuses []int
}

// QueueRefreshOutcomes appends outcomes to the FIFO queue future
// refresh_token grant calls consume from, one per call, before falling back
// to the script's static FailRefresh/RefreshAccessToken/RefreshRefreshToken
// fields once the queue is empty.
func (fm *FakeMicrosoft) QueueRefreshOutcomes(outcomes ...RefreshOutcome) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.refreshOutcomes = append(fm.refreshOutcomes, outcomes...)
}

// QueueUserInfoProbeStatuses appends statuses (200 or 401) to the FIFO queue
// future GET /v1.0/me calls consume from, one per call — the reconciliation
// probe's own scripting (PD37), independent of the OAuth callback's own
// FailUserInfo static field. Falls back to the script's static FailUserInfo
// once the queue is empty.
func (fm *FakeMicrosoft) QueueUserInfoProbeStatuses(statuses ...int) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.userInfoProbeStatuses = append(fm.userInfoProbeStatuses, statuses...)
}

func (fm *FakeMicrosoft) nextRefreshOutcome() (RefreshOutcome, bool) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	if len(fm.refreshOutcomes) == 0 {
		return RefreshOutcome{}, false
	}
	next := fm.refreshOutcomes[0]
	fm.refreshOutcomes = fm.refreshOutcomes[1:]
	return next, true
}

func (fm *FakeMicrosoft) nextUserInfoProbeStatus() (int, bool) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	if len(fm.userInfoProbeStatuses) == 0 {
		return 0, false
	}
	next := fm.userInfoProbeStatuses[0]
	fm.userInfoProbeStatuses = fm.userInfoProbeStatuses[1:]
	return next, true
}

func (fm *FakeMicrosoft) recordRefreshCall(form url.Values) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.LastTokenForm = form
	fm.RefreshCallCount++
}

func (fm *FakeMicrosoft) recordUserInfoAuthHeader(header string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.LastUserInfoAuthHeader = header
}

// NewFakeMicrosoft starts a FakeMicrosoft server scripted per script, and
// registers it for cleanup with t.
func NewFakeMicrosoft(t *testing.T, script FakeMicrosoftScript) *FakeMicrosoft {
	t.Helper()
	fm := &FakeMicrosoft{}

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/v2.0/token", fm.tokenHandler(script))
	mux.HandleFunc("/v1.0/me", fm.userInfoHandler(script))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	fm.TokenURL = server.URL + "/oauth2/v2.0/token"
	fm.UserInfoURL = server.URL + "/v1.0/me"
	return fm
}

// userInfoHandler serves GET /v1.0/me: first consuming
// QueueUserInfoProbeStatuses' queue (the reconciliation probe's own
// scripting, PD37) when non-empty, otherwise the static FailUserInfo
// behavior every OAuth-callback test already relies on.
func (fm *FakeMicrosoft) userInfoHandler(script FakeMicrosoftScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if status, ok := fm.nextUserInfoProbeStatus(); ok {
			if status != http.StatusOK {
				w.WriteHeader(status)
				return
			}
		} else if script.FailUserInfo {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fm.recordUserInfoAuthHeader(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"mail":        script.AccountEmail,
			"displayName": script.AccountDisplayName,
		})
	}
}

// tokenHandler serves both the authorization_code grant (the OAuth
// callback's token exchange) and the refresh_token grant (Slice 4/5, PD18/
// PD36), distinguished by the form's own grant_type. A refresh_token grant
// first consumes QueueRefreshOutcomes' queue when non-empty (Phase 3 Slice
// 5's success/rotated/invalid_grant/network-drop/5xx scripting); once that
// queue is empty, it falls back to the static FailRefresh/RefreshAccessToken/
// RefreshRefreshToken fields every Slice 4 test already relies on. An
// authorization_code grant is unaffected by either queue.
func (fm *FakeMicrosoft) tokenHandler(script FakeMicrosoftScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		isRefresh := r.Form.Get("grant_type") == "refresh_token"

		if isRefresh {
			if outcome, ok := fm.nextRefreshOutcome(); ok {
				fm.serveScriptedRefreshOutcome(w, r, outcome)
				return
			}
			if script.FailRefresh {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
		}
		if !isRefresh && script.FailTokenExchange {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		accessToken, refreshToken := script.AccessToken, script.RefreshToken
		if isRefresh {
			fm.recordRefreshCall(r.Form)
			if script.RefreshAccessToken != "" {
				accessToken = script.RefreshAccessToken
			}
			refreshToken = script.RefreshRefreshToken
		} else {
			fm.mu.Lock()
			fm.LastTokenForm = r.Form
			fm.mu.Unlock()
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  accessToken,
			"refresh_token": refreshToken,
			"expires_in":    script.ExpiresIn,
		})
	}
}

// serveScriptedRefreshOutcome answers one refresh_token grant per outcome's
// own shape (RefreshOutcome's doc comment): NetworkDrop hijacks and closes
// the connection with no response at all; ServerError writes a bare 500;
// InvalidGrant writes the OAuth-shaped permanent refusal; otherwise a normal
// success body carrying outcome's own tokens.
func (fm *FakeMicrosoft) serveScriptedRefreshOutcome(w http.ResponseWriter, r *http.Request, outcome RefreshOutcome) {
	fm.recordRefreshCall(r.Form)

	if outcome.NetworkDrop {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = conn.Close()
		return
	}
	if outcome.ServerError {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if outcome.InvalidGrant {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  outcome.AccessToken,
		"refresh_token": outcome.RefreshToken,
		"expires_in":    outcome.ExpiresIn,
	})
}
