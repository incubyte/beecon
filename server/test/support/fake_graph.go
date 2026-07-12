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
	"testing"
)

// FakeGraphScript configures how FakeGraph's messages endpoint responds.
type FakeGraphScript struct {
	// StatusCode is the HTTP status FakeGraph returns; zero defaults to 200.
	StatusCode int
	// Body is the raw response body FakeGraph returns; empty defaults to a
	// minimal messages payload when StatusCode is 200 (or unset).
	Body string
}

// FakeGraph is a running fake Microsoft Graph server plus the request
// details it observed, so a test can assert on what providerhttp.Client
// sent (e.g. that the Authorization header carried the connection's
// decrypted access token, and that top/skip/select/filter arguments arrived
// as query parameters).
type FakeGraph struct {
	MessagesURL string

	LastAuthorizationHeader string
	LastQuery               map[string][]string
}

// NewFakeGraph starts a FakeGraph server scripted per script, and registers
// it for cleanup with t.
func NewFakeGraph(t *testing.T, script FakeGraphScript) *FakeGraph {
	t.Helper()
	fg := &FakeGraph{}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/me/messages", func(w http.ResponseWriter, r *http.Request) {
		fg.LastAuthorizationHeader = r.Header.Get("Authorization")
		fg.LastQuery = r.URL.Query()

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
		_ = json.NewEncoder(w).Encode(map[string]any{
			"value": []map[string]string{
				{"id": "msg-1", "subject": "Hello"},
			},
		})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	fg.MessagesURL = server.URL + "/v1.0/me/messages"
	return fg
}
