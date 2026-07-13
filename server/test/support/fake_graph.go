//go:build integration

// Package support: FakeGraph is a scripted httptest.Server standing in for
// Microsoft Graph's GET /v1.0/me/messages — the one upstream call
// providerhttp.Client makes during tool execution. Crucial-path journeys
// point the outlook-list-messages tool definition's call URL at this server
// instead of the real internet, scripted to happy/401/429/5xx outcomes.
package support

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// FakeGraphScript configures how FakeGraph's messages endpoints respond.
type FakeGraphScript struct {
	// StatusCode is the HTTP status FakeGraph returns; zero defaults to 200.
	StatusCode int
	// Body is the raw response body FakeGraph returns; empty defaults to a
	// minimal messages payload when StatusCode is 200 (or unset).
	Body string

	// RateLimitedAttempts is the number of leading calls to
	// GET /v1.0/me/messages that respond as Graph's normalized rate limit
	// (PD21, Slice 6) — an HTTP 429 carrying Graph's nested
	// error.innerError.code throttle shape — before falling through to
	// StatusCode/Body. 0 (default) never rate-limits.
	RateLimitedAttempts int
	// RateLimitRetryAfter is the Retry-After header value sent on each
	// rate-limited attempt; empty sends no header at all, exercising
	// retry.go's jittered-backoff fallback instead.
	RateLimitRetryAfter string
}

// FakeGraph is a running fake Microsoft Graph server plus the request
// details it observed, so a test can assert on what providerhttp.Client
// sent (e.g. that the Authorization header carried the connection's
// decrypted access token, and that top/skip/select/filter arguments arrived
// as query parameters).
type FakeGraph struct {
	// BaseURL is this server's Microsoft-Graph-shaped base
	// (".../v1.0") — set catalog.ProviderDefinition.BaseURL to this to
	// exercise the finalized format's baseUrl+path joining and
	// {input.messageId} path templating (outlook-get-message) against this
	// fake instead of the real internet.
	BaseURL string
	// MessagesURL is the full outlook-list-messages call URL, preserved for
	// definitions built Phase-1-style (a full URL in Path, no BaseURL).
	MessagesURL string

	LastAuthorizationHeader string
	LastQuery               map[string][]string
	LastMessageIDPath       string

	// MessagesCallCount counts every call to GET /v1.0/me/messages,
	// including rate-limited ones, so a retry journey can assert exactly how
	// many attempts the platform-side retry loop made.
	MessagesCallCount int
}

// NewFakeGraph starts a FakeGraph server scripted per script, and registers
// it for cleanup with t. Alongside GET /v1.0/me/messages (outlook-list-
// messages), it serves GET /v1.0/me/messages/{messageId} (outlook-get-
// message, Slice 1's path-parameter templating proof) with the same script.
func NewFakeGraph(t *testing.T, script FakeGraphScript) *FakeGraph {
	t.Helper()
	fg := &FakeGraph{}

	respond := func(w http.ResponseWriter, defaultBody func() any) {
		status := script.StatusCode
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if script.Body != "" {
			_, _ = w.Write([]byte(script.Body))
			return
		}
		_ = json.NewEncoder(w).Encode(defaultBody())
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/me/messages", func(w http.ResponseWriter, r *http.Request) {
		fg.LastAuthorizationHeader = r.Header.Get("Authorization")
		fg.LastQuery = r.URL.Query()
		fg.MessagesCallCount++
		if fg.MessagesCallCount <= script.RateLimitedAttempts {
			respondGraphRateLimited(w, script.RateLimitRetryAfter)
			return
		}
		respond(w, func() any {
			return map[string]any{
				"value": []map[string]string{
					{"id": "msg-1", "subject": "Hello"},
				},
			}
		})
	})
	mux.HandleFunc("/v1.0/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		fg.LastAuthorizationHeader = r.Header.Get("Authorization")
		fg.LastQuery = r.URL.Query()
		fg.LastMessageIDPath = strings.TrimPrefix(r.URL.Path, "/v1.0/me/messages/")
		respond(w, func() any {
			return map[string]any{
				"id":      fg.LastMessageIDPath,
				"subject": "Hello",
			}
		})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	fg.BaseURL = server.URL + "/v1.0"
	fg.MessagesURL = server.URL + "/v1.0/me/messages"
	return fg
}

// respondGraphRateLimited writes Graph's normalized rate-limit shape (PD21):
// an HTTP 429 carrying the nested error.innerError.code throttle code, with
// Retry-After set when retryAfter is non-empty.
func respondGraphRateLimited(w http.ResponseWriter, retryAfter string) {
	if retryAfter != "" {
		w.Header().Set("Retry-After", retryAfter)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"error":{"code":"TooManyRequests","innerError":{"code":"activityLimitReached"}}}`))
}
