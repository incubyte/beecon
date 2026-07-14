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
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
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

	// RateLimitedAttempts is the number of leading calls to
	// /crm/v3/objects/contacts (list or create) that respond as Hubspot's
	// normalized rate limit (PD21, Slice 6) — an HTTP 429 carrying Hubspot's
	// RATE_LIMITS error category — before falling through to the script's
	// normal list/create behavior. 0 (default) never rate-limits.
	RateLimitedAttempts int
	// RateLimitRetryAfter is the Retry-After header value sent on each
	// rate-limited attempt; empty sends no header at all, exercising
	// retry.go's jittered-backoff fallback instead.
	RateLimitRetryAfter string

	// RefreshAccessToken and RefreshRefreshToken, when set, are what a
	// refresh_token grant returns instead of AccessToken/RefreshToken (Phase
	// 3 Slice 5, PD36): RefreshRefreshToken left empty simulates a provider
	// that does not rotate the refresh token.
	RefreshAccessToken  string
	RefreshRefreshToken string
	// FailRefresh makes a refresh_token grant return 400 invalid_grant,
	// simulating a revoked refresh token.
	FailRefresh bool
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

	// ContactsCallCount counts every call to /crm/v3/objects/contacts
	// (list or create), including rate-limited ones, so a retry journey can
	// assert exactly how many attempts the platform-side retry loop made.
	ContactsCallCount int

	// LastFileUpload is the most recent multipart file part FakeHubspot's
	// /files/v3/files endpoint received (PD22, Slice 7, AC4) — the field
	// name, filename, content type, and the raw bytes, so a test can assert
	// Beecon actually streamed the stored file's own bytes/filename/mime
	// rather than something synthesized.
	LastFileUpload *FakeHubspotUpload
	// FilesCallCount counts every call to /files/v3/files, so a test can
	// assert the fake provider was never called at all (AC5: an unknown or
	// cross-organization file_ id must short-circuit before any provider
	// call).
	FilesCallCount int

	// mu guards the contacts-search polling state below (Phase 3 Slice 4):
	// the crucial_path polling journey appends a new contact mid-test, from
	// the same goroutine that then calls PollOnce — mirrors FakeGraph's own
	// mu for the identical reason.
	mu                        sync.Mutex
	searchContacts            []FakeHubspotSearchContact
	failNextSearchesRemaining int

	// LastSearchBody is the most recent decoded JSON body posted to
	// /crm/v3/objects/contacts/search (the hubspot-contact-created poll
	// mapping's own call, PD35) — its filterGroups/sorts shape, so a test can
	// assert the {watermark} templating actually reached the request body.
	LastSearchBody map[string]any
	// SearchCallCount counts every call to /crm/v3/objects/contacts/search.
	SearchCallCount int

	// RefreshCallCount counts every refresh_token grant (Phase 3 Slice 5,
	// PD36), independent of the initial authorization_code exchange.
	RefreshCallCount int

	// refreshMu guards refreshOutcomes/userInfoProbeStatuses below — the
	// token-self-heal journey's concurrent scenarios call into this fake
	// from more than one goroutine at once (mirrors FakeMicrosoft's own mu).
	refreshMu             sync.Mutex
	refreshOutcomes       []FakeHubspotRefreshOutcome
	userInfoProbeStatuses []int
}

// FakeHubspotRefreshOutcome mirrors FakeMicrosoft's RefreshOutcome (Phase 3
// Slice 5): one scripted refresh_token grant response, consumed FIFO.
type FakeHubspotRefreshOutcome struct {
	AccessToken  string
	RefreshToken string

	InvalidGrant bool
	ServerError  bool
	NetworkDrop  bool
}

// QueueRefreshOutcomes appends outcomes to the FIFO queue future
// refresh_token grant calls consume from, one per call, before falling back
// to the script's static FailRefresh/RefreshAccessToken/RefreshRefreshToken
// fields once the queue is empty.
func (fh *FakeHubspot) QueueRefreshOutcomes(outcomes ...FakeHubspotRefreshOutcome) {
	fh.refreshMu.Lock()
	defer fh.refreshMu.Unlock()
	fh.refreshOutcomes = append(fh.refreshOutcomes, outcomes...)
}

// QueueUserInfoProbeStatuses appends statuses (200 or 401) to the FIFO queue
// future token-metadata probe calls consume from, one per call — the
// reconciliation probe's own scripting (PD37), independent of the OAuth
// callback's own FailUserInfo static field.
func (fh *FakeHubspot) QueueUserInfoProbeStatuses(statuses ...int) {
	fh.refreshMu.Lock()
	defer fh.refreshMu.Unlock()
	fh.userInfoProbeStatuses = append(fh.userInfoProbeStatuses, statuses...)
}

func (fh *FakeHubspot) nextRefreshOutcome() (FakeHubspotRefreshOutcome, bool) {
	fh.refreshMu.Lock()
	defer fh.refreshMu.Unlock()
	if len(fh.refreshOutcomes) == 0 {
		return FakeHubspotRefreshOutcome{}, false
	}
	next := fh.refreshOutcomes[0]
	fh.refreshOutcomes = fh.refreshOutcomes[1:]
	return next, true
}

func (fh *FakeHubspot) nextUserInfoProbeStatus() (int, bool) {
	fh.refreshMu.Lock()
	defer fh.refreshMu.Unlock()
	if len(fh.userInfoProbeStatuses) == 0 {
		return 0, false
	}
	next := fh.userInfoProbeStatuses[0]
	fh.userInfoProbeStatuses = fh.userInfoProbeStatuses[1:]
	return next, true
}

func (fh *FakeHubspot) recordRefreshCall() {
	fh.refreshMu.Lock()
	defer fh.refreshMu.Unlock()
	fh.RefreshCallCount++
}

// FakeHubspotSearchContact is one contact FakeHubspot's contacts-search
// route (hubspot-contact-created's poll mapping, PD35) serves. CreatedAt is
// an RFC3339 string carried at the result's own top level — matching
// hubspot.yaml's recordTimestampPath ("createdAt"), distinct from the
// "createdate" property real Hubspot also carries inside Properties.
type FakeHubspotSearchContact struct {
	ID         string
	Properties map[string]string
	CreatedAt  string
}

// AddSearchContact appends a new contact observable by the very next poll
// (Phase 3 Slice 4's crucial_path journey: "a new contact appears" mid-test).
func (fh *FakeHubspot) AddSearchContact(contact FakeHubspotSearchContact) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.searchContacts = append(fh.searchContacts, contact)
}

// FailNextSearch makes the next call (and only the next call) to
// /crm/v3/objects/contacts/search return 500 — the same "one scripted
// failure" shape as FakeGraph.FailNextMailFolderPoll.
func (fh *FakeHubspot) FailNextSearch() {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.failNextSearchesRemaining++
}

// FakeHubspotUpload is one multipart file part FakeHubspot's /files/v3/files
// endpoint accepted (PD22, Slice 7).
type FakeHubspotUpload struct {
	FieldName string
	FileName  string
	MimeType  string
	Content   []byte
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
	mux.HandleFunc("/crm/v3/objects/contacts/search", fh.contactsSearchHandler())
	mux.HandleFunc("/files/v3/files", fh.filesHandler(script))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	fh.TokenURL = server.URL + "/oauth/v1/token"
	fh.UserInfoURLTemplate = server.URL + "/oauth/v1/access-tokens/{accessToken}"
	fh.BaseURL = server.URL
	return fh
}

// tokenHandler serves both the authorization_code grant (the OAuth
// callback's token exchange) and the refresh_token grant (Phase 3 Slice 5,
// PD36), distinguished by the form's own grant_type — mirroring
// FakeMicrosoft's own tokenHandler. A refresh_token grant first consumes
// QueueRefreshOutcomes' queue when non-empty; once empty, it falls back to
// the static FailRefresh/RefreshAccessToken/RefreshRefreshToken fields.
func (fh *FakeHubspot) tokenHandler(script FakeHubspotScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		isRefresh := r.Form.Get("grant_type") == "refresh_token"

		if isRefresh {
			if outcome, ok := fh.nextRefreshOutcome(); ok {
				fh.serveScriptedRefreshOutcome(w, outcome)
				return
			}
			if script.FailRefresh {
				fh.recordRefreshCall()
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
		} else if script.FailTokenExchange {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		fh.LastTokenForm = r.Form
		accessToken, refreshToken := script.AccessToken, script.RefreshToken
		if isRefresh {
			fh.recordRefreshCall()
			if script.RefreshAccessToken != "" {
				accessToken = script.RefreshAccessToken
			}
			refreshToken = script.RefreshRefreshToken
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token":  accessToken,
			"refresh_token": refreshToken,
		})
	}
}

// serveScriptedRefreshOutcome mirrors FakeMicrosoft's own
// serveScriptedRefreshOutcome for FakeHubspotRefreshOutcome's shape.
func (fh *FakeHubspot) serveScriptedRefreshOutcome(w http.ResponseWriter, outcome FakeHubspotRefreshOutcome) {
	fh.recordRefreshCall()

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
	_ = json.NewEncoder(w).Encode(map[string]string{
		"access_token":  outcome.AccessToken,
		"refresh_token": outcome.RefreshToken,
	})
}

// userInfoHandler serves GET /oauth/v1/access-tokens/{token}: first
// consuming QueueUserInfoProbeStatuses' queue (the reconciliation probe's
// own scripting, PD37) when non-empty, otherwise the static FailUserInfo
// behavior every OAuth-callback test already relies on.
func (fh *FakeHubspot) userInfoHandler(script FakeHubspotScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if status, ok := fh.nextUserInfoProbeStatus(); ok {
			if status != http.StatusOK {
				w.WriteHeader(status)
				return
			}
		} else if script.FailUserInfo {
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
		fh.ContactsCallCount++
		if fh.ContactsCallCount <= script.RateLimitedAttempts {
			respondHubspotRateLimited(w, script.RateLimitRetryAfter)
			return
		}
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

// respondHubspotRateLimited writes Hubspot's normalized rate-limit shape
// (PD21): an HTTP 429 carrying the RATE_LIMITS error category, with
// Retry-After set when retryAfter is non-empty.
func respondHubspotRateLimited(w http.ResponseWriter, retryAfter string) {
	if retryAfter != "" {
		w.Header().Set("Retry-After", retryAfter)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"category":"RATE_LIMITS","message":"You have reached your secondly limit."}`))
}

// contactsSearchHandler serves POST /crm/v3/objects/contacts/search (the
// hubspot-contact-created poll mapping's own call, PD35): every
// currently-held search contact, unfiltered — production's own watermark
// decision (triggers.ApplyWatermark) is what actually selects which of these
// are new, exactly as it does against the real Hubspot search endpoint's own
// createdate filter, so this fake does not need to interpret the posted
// filterGroups itself to be a faithful stand-in (mirrors FakeGraph's
// mailFolderMessagesHandler's identical reasoning).
func (fh *FakeHubspot) contactsSearchHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fh.mu.Lock()
		fh.SearchCallCount++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fh.LastSearchBody = body
		shouldFail := fh.failNextSearchesRemaining > 0
		if shouldFail {
			fh.failNextSearchesRemaining--
		}
		contacts := append([]FakeHubspotSearchContact(nil), fh.searchContacts...)
		fh.mu.Unlock()

		if shouldFail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		results := make([]map[string]any, 0, len(contacts))
		for _, c := range contacts {
			properties := make(map[string]any, len(c.Properties))
			for k, v := range c.Properties {
				properties[k] = v
			}
			results = append(results, map[string]any{
				"id":         c.ID,
				"createdAt":  c.CreatedAt,
				"properties": properties,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
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

// filesHandler serves /files/v3/files (PD22, Slice 7, AC4): accepts one
// multipart file part and echoes back Hubspot's own file-record shape
// ({id, name, url}) so hubspot-upload-file's execution can prove it returns
// the provider's record, not Beecon's own file metadata.
func (fh *FakeHubspot) filesHandler(_ FakeHubspotScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fh.FilesCallCount++
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		reader, err := r.MultipartReader()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			if part.FileName() == "" {
				_ = part.Close()
				continue
			}
			content, _ := io.ReadAll(part)
			fh.LastFileUpload = &FakeHubspotUpload{
				FieldName: part.FormName(),
				FileName:  part.FileName(),
				MimeType:  part.Header.Get("Content-Type"),
				Content:   content,
			}
			_ = part.Close()
		}
		if fh.LastFileUpload == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "hubspot-file-1",
			"name": fh.LastFileUpload.FileName,
			"url":  "https://api.hubapi.com/filemanager/api/v3/files/hubspot-file-1/signed-url-redirect",
		})
	}
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
