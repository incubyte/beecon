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
	"sync"
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

// FakeGraphMessage is one message FakeGraph's mailFolders/{folderId}/messages
// route (the outlook-message-received poll mapping's own call, Phase 3 Slice
// 4) serves for one folder. ReceivedDateTime is an RFC3339 string, matching
// outlook.yaml's recordTimestampPath/{watermark} convention exactly.
type FakeGraphMessage struct {
	ID               string
	Subject          string
	From             string
	ReceivedDateTime string
	BodyPreview      string
	FolderID         string
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

	// mu guards the mail-folder polling state below (Phase 3 Slice 4): the
	// crucial_path polling journey appends a new message mid-test, from the
	// same goroutine that then calls PollOnce — a mutex keeps that safe
	// without forcing every other (single-goroutine) FakeGraph field to pay
	// for locking too.
	mu                           sync.Mutex
	messagesByFolder             map[string][]FakeGraphMessage
	failMailFolderPollsRemaining int

	// LastMailFolderID and MailFolderMessagesCallCount observe the poll
	// mapping's own call — folder-scoped, per instance (Slice 4's AC8: two
	// instances on different folders must be independently observable).
	LastMailFolderID            string
	MailFolderMessagesCallCount int
}

// AddMessage appends a new message to folderId's mailbox, observable by the
// very next poll (Phase 3 Slice 4's crucial_path journey: "a new Outlook
// message arrives" mid-test).
func (fg *FakeGraph) AddMessage(folderID string, msg FakeGraphMessage) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	if fg.messagesByFolder == nil {
		fg.messagesByFolder = map[string][]FakeGraphMessage{}
	}
	msg.FolderID = folderID
	fg.messagesByFolder[folderID] = append(fg.messagesByFolder[folderID], msg)
}

// FailNextMailFolderPoll makes the next call (and only the next call) to the
// mailFolders/{folderId}/messages route return 500, regardless of folder —
// the crucial_path polling journey's "a failing poll... writes a log entry
// and does not stop the schedule" proof (PD34).
func (fg *FakeGraph) FailNextMailFolderPoll() {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	fg.failMailFolderPollsRemaining++
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
	mux.HandleFunc("/v1.0/me/mailFolders/", fg.mailFolderMessagesHandler)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	fg.BaseURL = server.URL + "/v1.0"
	fg.MessagesURL = server.URL + "/v1.0/me/messages"
	return fg
}

// mailFolderMessagesHandler serves GET /v1.0/me/mailFolders/{folderId}/messages
// (the outlook-message-received poll mapping's own call, PD35): every
// currently-held message in that folder, unfiltered — production's own
// watermark decision (triggers.ApplyWatermark) is what actually selects
// which of these are new, exactly as it does against the real Graph API's
// own $filter, so this fake does not need to interpret $filter/$orderby
// itself to be a faithful stand-in.
func (fg *FakeGraph) mailFolderMessagesHandler(w http.ResponseWriter, r *http.Request) {
	fg.mu.Lock()
	folderID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1.0/me/mailFolders/"), "/messages")
	fg.LastAuthorizationHeader = r.Header.Get("Authorization")
	fg.LastQuery = r.URL.Query()
	fg.LastMailFolderID = folderID
	fg.MailFolderMessagesCallCount++
	messages := append([]FakeGraphMessage(nil), fg.messagesByFolder[folderID]...)
	shouldFail := fg.failMailFolderPollsRemaining > 0
	if shouldFail {
		fg.failMailFolderPollsRemaining--
	}
	fg.mu.Unlock()

	if shouldFail {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	value := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		value = append(value, map[string]any{
			"id":               m.ID,
			"subject":          m.Subject,
			"from":             map[string]any{"emailAddress": map[string]any{"address": m.From}},
			"receivedDateTime": m.ReceivedDateTime,
			"bodyPreview":      m.BodyPreview,
			"parentFolderId":   m.FolderID,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
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
