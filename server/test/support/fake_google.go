//go:build integration

// Package support: FakeGoogle is a scripted httptest.Server standing in for
// Google's OAuth token endpoint, its OpenID userinfo endpoint, Gmail's
// messages endpoints, and Google Calendar's events endpoint (shared by the
// Providers strand's Slice 2, PD78: Gmail and Google Calendar reuse the same
// OAuth block, so one fake server stands in for both) — the upstream calls
// oauthhttp.Client and providerhttp.Client make during the OAuth callback and
// gmail-list-messages/gmail-get-message/gmail-send-message/gcal-list-events/
// gcal-create-event/gcal-event-updated execution. Crucial-path journeys point
// a catalog.ProviderDefinition's TokenURL/UserInfoURL/BaseURL at this server
// instead of the real internet. Mirrors FakeHubspot's/FakeGraph's shape
// (fake_hubspot.go, fake_graph.go).
package support

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// FakeGoogleMessage is one fixture message FakeGoogle's messages-list
// endpoint pages over.
type FakeGoogleMessage struct {
	ID       string
	ThreadID string
}

// FakeGoogleScript configures how FakeGoogle's endpoints respond.
type FakeGoogleScript struct {
	AccessToken  string
	RefreshToken string
	// AccountEmail and AccountName are the OpenID userinfo endpoint's
	// top-level "email"/"name" fields (matching gmail.yaml's userInfo
	// mapping).
	AccountEmail string
	AccountName  string

	// FailTokenExchange makes the token endpoint return 400, simulating the
	// provider rejecting the authorization code.
	FailTokenExchange bool
	// FailUserInfo makes the userinfo endpoint return 401 after a
	// successful token exchange.
	FailUserInfo bool

	// Messages is the fixed set of messages gmail-list-messages pages over,
	// in page order.
	Messages []FakeGoogleMessage

	// GetMessageStatus, when non-zero, makes gmail-get-message's endpoint
	// return this status (with GetMessageBody, if set) instead of a
	// successful fetch.
	GetMessageStatus int
	GetMessageBody   string

	// SendStatus, when non-zero, makes gmail-send-message's endpoint return
	// this status (with SendBody, if set) instead of a successful send —
	// proves an upstream error surfaces as a tool-level failure.
	SendStatus int
	SendBody   string

	// Events is the fixed set of events gcal-list-events pages over and
	// gcal-event-updated polls, in list order (Providers strand Slice 2).
	Events []FakeGoogleEvent

	// CreateEventStatus, when non-zero, makes gcal-create-event's endpoint
	// return this status (with CreateEventBody, if set) instead of a
	// successful creation — proves an upstream Calendar error surfaces as a
	// tool-level failure, mirroring SendStatus/SendBody above.
	CreateEventStatus int
	CreateEventBody   string
}

// FakeGoogleEvent is one fixture event FakeGoogle's Calendar events endpoint
// (/calendars/{calendarId}/events) pages over for gcal-list-events and polls
// for gcal-event-updated. Updated is an RFC3339 string, matching
// google-calendar.yaml's recordTimestampPath/{watermark} convention exactly
// (mirrors FakeGraph's FakeGraphMessage/FakeHubspot's FakeHubspotSearchContact).
type FakeGoogleEvent struct {
	ID            string
	Summary       string
	Status        string
	StartDateTime string
	EndDateTime   string
	Updated       string
}

// FakeGoogle is a running fake Google/Gmail server plus the request details
// it observed, so a test can assert on what Beecon sent.
type FakeGoogle struct {
	// TokenURL is FakeGoogle's OAuth token endpoint.
	TokenURL string
	// UserInfoURL is FakeGoogle's OpenID userinfo endpoint (fixed, unlike
	// Hubspot's {accessToken}-templated one) — set a
	// catalog.ProviderDefinition's UserInfoURL to this.
	UserInfoURL string
	// BaseURL is FakeGoogle's Gmail API base — set a
	// catalog.ProviderDefinition's BaseURL to this to exercise
	// gmail-list-messages/gmail-get-message/gmail-send-message against it.
	BaseURL string

	LastTokenForm                   url.Values
	LastUserInfoAuthorizationHeader string

	LastMessagesQuery url.Values
	MessagesCallCount int

	// LastGetMessageIDPath is the messageId FakeGoogle observed as its own
	// path segment, decoded off the wire — so a test can assert a
	// slash/question-mark-carrying messageId arrived correctly
	// URL-escaped and round-tripped intact.
	LastGetMessageIDPath string
	LastGetMessageQuery  url.Values
	GetMessageCallCount  int

	// LastSendBody is the most recent decoded JSON body posted to
	// /users/me/messages/send, so a test can assert the caller-supplied
	// raw value reached Gmail unchanged.
	LastSendBody  map[string]any
	SendCallCount int

	// mu guards the Calendar events state below (Providers strand Slice 2):
	// the crucial_path poll journey appends a new event mid-test, from the
	// same goroutine that then calls PollOnce — mirrors FakeGraph's/
	// FakeHubspot's own mu for the identical reason.
	mu     sync.Mutex
	events []FakeGoogleEvent

	// LastEventsCalendarID and LastEventsQuery are gcal-list-events'/
	// gcal-event-updated's own observed calendarId path segment and query
	// parameters — so a test can assert the inputSchema/configSchema default
	// ("primary") was applied when the caller/instance config omitted it, and
	// that updatedMin/orderBy/singleEvents arrived as declared.
	LastEventsCalendarID string
	LastEventsQuery      url.Values
	EventsCallCount      int

	// LastCreateEventCalendarID and LastCreateEventBody are gcal-create-event's
	// own observed calendarId path segment and decoded JSON body, so a test
	// can assert the dotted start.dateTime/end.dateTime keys built the nested
	// object Calendar's events.insert requires.
	LastCreateEventCalendarID string
	LastCreateEventBody       map[string]any
	CreateEventCallCount      int
}

// AddEvent appends a new event, observable by the very next gcal-list-events
// call or gcal-event-updated poll (mirrors FakeGraph.AddMessage/
// FakeHubspot.AddSearchContact's identical "a new record appears" precedent).
func (fg *FakeGoogle) AddEvent(event FakeGoogleEvent) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	fg.events = append(fg.events, event)
}

// NewFakeGoogle starts a FakeGoogle server scripted per script, and
// registers it for cleanup with t.
func NewFakeGoogle(t *testing.T, script FakeGoogleScript) *FakeGoogle {
	t.Helper()
	fg := &FakeGoogle{events: append([]FakeGoogleEvent(nil), script.Events...)}

	mux := http.NewServeMux()
	mux.HandleFunc("/token", fg.tokenHandler(script))
	mux.HandleFunc("/userinfo", fg.userInfoHandler(script))
	mux.HandleFunc("/users/me/messages", fg.listMessagesHandler(script.Messages))
	mux.HandleFunc("/users/me/messages/", fg.messagesSubtreeHandler(script))
	mux.HandleFunc("/calendars/", fg.calendarEventsHandler(script))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	fg.TokenURL = server.URL + "/token"
	fg.UserInfoURL = server.URL + "/userinfo"
	fg.BaseURL = server.URL
	return fg
}

// tokenHandler serves the authorization_code grant (the OAuth callback's
// token exchange) — mirrors FakeHubspot's tokenHandler minus the
// refresh_token branch (Gmail's own definition declares no refresh path in
// this strand).
func (fg *FakeGoogle) tokenHandler(script FakeGoogleScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if script.FailTokenExchange {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fg.LastTokenForm = r.Form
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token":  script.AccessToken,
			"refresh_token": script.RefreshToken,
		})
	}
}

// userInfoHandler serves GET /userinfo (Google's OpenID userinfo endpoint,
// bearer-authenticated): top-level "email"/"name" fields, matching
// gmail.yaml's userInfo mapping.
func (fg *FakeGoogle) userInfoHandler(script FakeGoogleScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fg.LastUserInfoAuthorizationHeader = r.Header.Get("Authorization")
		if script.FailUserInfo {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"email": script.AccountEmail,
			"name":  script.AccountName,
		})
	}
}

// listMessagesHandler serves GET /users/me/messages (gmail-list-messages):
// pages script.Messages by the canonical pageSize/cursor convention mapped
// onto Gmail's own maxResults/pageToken query parameters, carrying
// nextPageToken when a further page remains.
func (fg *FakeGoogle) listMessagesHandler(messages []FakeGoogleMessage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fg.MessagesCallCount++
		fg.LastMessagesQuery = r.URL.Query()
		respondPagedGmailMessages(w, messages, r.URL.Query())
	}
}

// messagesSubtreeHandler serves everything under /users/me/messages/: the
// literal /users/me/messages/send route (gmail-send-message, POST) and
// every other path segment as a gmail-get-message messageId (GET) — mirrors
// how the real Gmail API distinguishes the two by path shape.
func (fg *FakeGoogle) messagesSubtreeHandler(script FakeGoogleScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/me/messages/send" && r.Method == http.MethodPost {
			fg.handleSendMessage(w, r, script)
			return
		}
		fg.handleGetMessage(w, r, script)
	}
}

func (fg *FakeGoogle) handleGetMessage(w http.ResponseWriter, r *http.Request, script FakeGoogleScript) {
	fg.GetMessageCallCount++
	fg.LastGetMessageIDPath = strings.TrimPrefix(r.URL.Path, "/users/me/messages/")
	fg.LastGetMessageQuery = r.URL.Query()
	if script.GetMessageStatus != 0 {
		w.WriteHeader(script.GetMessageStatus)
		if script.GetMessageBody != "" {
			_, _ = w.Write([]byte(script.GetMessageBody))
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":       fg.LastGetMessageIDPath,
		"threadId": "thread-1",
		"snippet":  "hello",
	})
}

func (fg *FakeGoogle) handleSendMessage(w http.ResponseWriter, r *http.Request, script FakeGoogleScript) {
	fg.SendCallCount++
	if script.SendStatus != 0 {
		w.WriteHeader(script.SendStatus)
		if script.SendBody != "" {
			_, _ = w.Write([]byte(script.SendBody))
		}
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	fg.LastSendBody = body
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":       "sent-message-1",
		"threadId": "thread-1",
	})
}

// calendarEventsHandler serves everything under /calendars/: GET
// {calendarId}/events (gcal-list-events and gcal-event-updated's poll, both
// hitting the identical path — distinguished only by which query parameters
// they send) and POST {calendarId}/events (gcal-create-event) — mirrors how
// the real Calendar API serves both list and insert under one URL.
func (fg *FakeGoogle) calendarEventsHandler(script FakeGoogleScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		calendarID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/calendars/"), "/events")
		switch r.Method {
		case http.MethodGet:
			fg.handleListEvents(w, r, calendarID)
		case http.MethodPost:
			fg.handleCreateEvent(w, r, calendarID, script)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

// handleListEvents serves GET /calendars/{calendarId}/events for both
// gcal-list-events (paged by maxResults/pageToken) and gcal-event-updated's
// poll mapping (which sends no maxResults/pageToken, so the default page —
// every currently-held event, unfiltered — is returned; production's own
// per-instance watermark decision is what actually selects which are new,
// exactly as it does against the real Calendar API's own updatedMin filter,
// mirroring FakeGraph's mailFolderMessagesHandler/FakeHubspot's
// contactsSearchHandler identical reasoning).
func (fg *FakeGoogle) handleListEvents(w http.ResponseWriter, r *http.Request, calendarID string) {
	fg.mu.Lock()
	fg.EventsCallCount++
	fg.LastEventsCalendarID = calendarID
	fg.LastEventsQuery = r.URL.Query()
	events := append([]FakeGoogleEvent(nil), fg.events...)
	fg.mu.Unlock()

	respondPagedGoogleEvents(w, events, r.URL.Query())
}

// handleCreateEvent serves POST /calendars/{calendarId}/events
// (gcal-create-event): echoes back the nested {start:{dateTime},
// end:{dateTime}} object it received so a test can assert the dotted body
// mapping built the correct shape, or returns a scripted error status/body
// proving an upstream rejection surfaces as a tool-level failure.
func (fg *FakeGoogle) handleCreateEvent(w http.ResponseWriter, r *http.Request, calendarID string, script FakeGoogleScript) {
	fg.mu.Lock()
	fg.CreateEventCallCount++
	fg.LastCreateEventCalendarID = calendarID
	fg.mu.Unlock()

	if script.CreateEventStatus != 0 {
		w.WriteHeader(script.CreateEventStatus)
		if script.CreateEventBody != "" {
			_, _ = w.Write([]byte(script.CreateEventBody))
		}
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	fg.mu.Lock()
	fg.LastCreateEventBody = body
	fg.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":      "event-created-1",
		"status":  "confirmed",
		"summary": body["summary"],
		"start":   body["start"],
		"end":     body["end"],
	})
}

// respondPagedGoogleEvents serves one page of events starting at the
// "pageToken" query parameter (an opaque index into events, defaulting to 0)
// sized by the "maxResults" query parameter (defaulting to len(events)),
// carrying nextPageToken when a further page remains, each event's
// start/end nested as {dateTime:...} the way the real Calendar API shapes
// them — mirrors respondPagedGmailMessages/respondPagedContacts for
// Calendar's own event shape.
func respondPagedGoogleEvents(w http.ResponseWriter, events []FakeGoogleEvent, query url.Values) {
	offset := parseIntDefault(query.Get("pageToken"), 0)
	limit := parseIntDefault(query.Get("maxResults"), len(events))
	if offset < 0 {
		offset = 0
	}
	if offset > len(events) {
		offset = len(events)
	}
	end := offset + limit
	hasMore := end < len(events)
	if end > len(events) {
		end = len(events)
	}

	items := make([]map[string]any, 0, end-offset)
	for _, e := range events[offset:end] {
		items = append(items, map[string]any{
			"id":      e.ID,
			"status":  e.Status,
			"summary": e.Summary,
			"start":   map[string]any{"dateTime": e.StartDateTime},
			"end":     map[string]any{"dateTime": e.EndDateTime},
			"updated": e.Updated,
		})
	}
	body := map[string]any{"items": items}
	if hasMore {
		body["nextPageToken"] = strconv.Itoa(end)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// respondPagedGmailMessages serves one page of messages starting at the
// "pageToken" query parameter (an opaque index into messages, defaulting to
// 0) sized by the "maxResults" query parameter (defaulting to
// len(messages)), carrying nextPageToken when a further page remains —
// mirrors FakeHubspot's respondPagedContacts for Gmail's own shape.
func respondPagedGmailMessages(w http.ResponseWriter, messages []FakeGoogleMessage, query url.Values) {
	offset := parseIntDefault(query.Get("pageToken"), 0)
	limit := parseIntDefault(query.Get("maxResults"), len(messages))
	if offset < 0 {
		offset = 0
	}
	if offset > len(messages) {
		offset = len(messages)
	}
	end := offset + limit
	hasMore := end < len(messages)
	if end > len(messages) {
		end = len(messages)
	}

	results := make([]map[string]any, 0, end-offset)
	for _, m := range messages[offset:end] {
		results = append(results, map[string]any{"id": m.ID, "threadId": m.ThreadID})
	}

	body := map[string]any{"messages": results, "resultSizeEstimate": len(messages)}
	if hasMore {
		body["nextPageToken"] = strconv.Itoa(end)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
