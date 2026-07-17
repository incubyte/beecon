//go:build integration

// Package support: FakeGitHub is a scripted httptest.Server standing in for
// GitHub's OAuth token endpoint, its account-fetch endpoint (GET /user), and
// its REST API's repos/issues endpoints — the upstream calls oauthhttp.Client
// and providerhttp.Client make during the OAuth callback and
// github-list-repos/github-list-issues/github-create-issue execution.
// Crucial-path journeys point a catalog.ProviderDefinition's
// TokenURL/UserInfoURL/BaseURL at this server instead of the real internet.
// Mirrors FakeHubspot's/FakeGoogle's shape (fake_hubspot.go, fake_google.go).
//
// Unlike those fakes, every FakeGitHub endpoint enforces GitHub's real,
// documented requirement (PD84/PD83): a request with no *deliberately set*
// User-Agent header is rejected with HTTP 403. Go's net/http stdlib silently
// fills in its own "Go-http-client/1.1" default whenever calling code never
// sets the header at all, so a literal-emptiness check alone would never
// catch that omission (verified: it never actually fires against a real
// http.Client) — isMissingDeliberateUserAgent below also treats that stdlib
// default as "missing" for that reason. This is deliberate, not incidental —
// it is what makes the activation journey
// (TestGitHubJourney_ActivatesViaTheRealOAuthCallbackBecauseOfTheDefault
// UserAgent) actually prove that connections/driven/oauthhttp's default
// User-Agent (PD83) is what unblocks GitHub: without that default, this fake
// would 403 the token exchange and the account-fetch, and the connection
// would never reach ACTIVE.
package support

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// isMissingDeliberateUserAgent reports whether ua looks like no caller code
// ever set a User-Agent header at all: either literally empty, or Go's own
// net/http stdlib default ("Go-http-client/1.1...") that the standard
// Transport fills in unasked whenever a request carries none. Real GitHub
// requires an application to set its own identifying header; masking a
// forgotten header behind Go's default would let a broken client pass this
// fake's check for the wrong reason, silently defeating the point of it.
func isMissingDeliberateUserAgent(ua string) bool {
	return ua == "" || strings.HasPrefix(ua, "Go-http-client/")
}

// FakeGitHubRepo is one fixture repository FakeGitHub's /user/repos endpoint
// returns.
type FakeGitHubRepo struct {
	ID       int
	Name     string
	FullName string
	Private  bool
	HTMLURL  string
}

// FakeGitHubIssue is one fixture issue FakeGitHub's issues-list endpoint
// returns.
type FakeGitHubIssue struct {
	ID      int
	Number  int
	Title   string
	Body    string
	State   string
	HTMLURL string
}

// FakeGitHubScript configures how FakeGitHub's endpoints respond.
type FakeGitHubScript struct {
	AccessToken string
	// AccountEmail and AccountLogin are GET /user's top-level "email"/"login"
	// fields, matching github.yaml's userInfo mapping (email->email,
	// displayName->login).
	AccountEmail string
	AccountLogin string

	// FailTokenExchange makes the token endpoint return a non-200 status,
	// simulating GitHub rejecting the authorization code.
	FailTokenExchange bool
	// FailUserInfo makes GET /user return 401 after a successful token
	// exchange (a User-Agent-carrying request that GitHub still rejects for
	// an unrelated reason, distinct from the 403-no-User-Agent case below).
	FailUserInfo bool

	// Repos is the fixed set of repositories github-list-repos returns.
	Repos []FakeGitHubRepo
	// Issues is the fixed set of issues github-list-issues returns.
	Issues []FakeGitHubIssue

	// CreateIssueStatus, when non-zero, makes github-create-issue's endpoint
	// return this status (with CreateIssueBody, if set) instead of a
	// successful creation — proves an upstream GitHub error (e.g. 422)
	// surfaces as a tool-level failure.
	CreateIssueStatus int
	CreateIssueBody   string
}

// FakeGitHub is a running fake GitHub server plus the request details it
// observed, so a test can assert on what Beecon sent.
type FakeGitHub struct {
	// TokenURL is FakeGitHub's OAuth token endpoint.
	TokenURL string
	// UserInfoURL is FakeGitHub's account-fetch endpoint (GET /user).
	UserInfoURL string
	// BaseURL is FakeGitHub's REST API base — set a
	// catalog.ProviderDefinition's BaseURL to this to exercise
	// github-list-repos/github-list-issues/github-create-issue against it.
	BaseURL string

	LastTokenForm url.Values
	// LastTokenUserAgent is the User-Agent header FakeGitHub observed on the
	// token-exchange request — empty, or Go's own stdlib default, means no
	// caller code deliberately set one, which this fake treats as a 403
	// (mirrors GitHub's real API, isMissingDeliberateUserAgent).
	LastTokenUserAgent string

	// LastUserInfoUserAgent and LastUserInfoAuthorizationHeader are what
	// FakeGitHub observed on the GET /user account-fetch request.
	LastUserInfoUserAgent           string
	LastUserInfoAuthorizationHeader string

	LastReposQuery        url.Values
	LastReposUserAgent    string
	LastReposAcceptHeader string
	ReposCallCount        int

	// LastIssuesOwnerRepoPath is the decoded "{owner}/{repo}" FakeGitHub
	// observed as its own path segment, so a test can assert
	// {input.owner}/{input.repo} were substituted and URL-escaped correctly.
	LastIssuesOwnerRepoPath string
	LastIssuesQuery         url.Values
	LastIssuesUserAgent     string
	LastIssuesAcceptHeader  string
	IssuesCallCount         int

	LastCreateIssueOwnerRepoPath string
	LastCreateIssueBody         map[string]any
	LastCreateIssueUserAgent    string
	LastCreateIssueAcceptHeader string
	CreateIssueCallCount        int
}

// NewFakeGitHub starts a FakeGitHub server scripted per script, and registers
// it for cleanup with t.
func NewFakeGitHub(t *testing.T, script FakeGitHubScript) *FakeGitHub {
	t.Helper()
	fg := &FakeGitHub{}

	mux := http.NewServeMux()
	mux.HandleFunc("/login/oauth/access_token", fg.tokenHandler(script))
	mux.HandleFunc("/user", fg.userHandler(script))
	mux.HandleFunc("/user/repos", fg.listReposHandler(script))
	mux.HandleFunc("/repos/", fg.repoIssuesHandler(script))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	fg.TokenURL = server.URL + "/login/oauth/access_token"
	fg.UserInfoURL = server.URL + "/user"
	fg.BaseURL = server.URL
	return fg
}

// tokenHandler serves the authorization_code grant (the OAuth callback's
// token exchange). GitHub's OAuth Apps issue non-expiring tokens, so this
// never returns a refresh_token (the definition's documented deviation).
func (fg *FakeGitHub) tokenHandler(script FakeGitHubScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fg.LastTokenUserAgent = r.Header.Get("User-Agent")
		if isMissingDeliberateUserAgent(fg.LastTokenUserAgent) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
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
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": script.AccessToken})
	}
}

// userHandler serves GET /user (the account-fetch that activates a
// connection with captured identity): a request carrying no deliberately set
// User-Agent header is rejected 403 exactly as the real GitHub API does —
// this is the endpoint PD83's default oauthhttp User-Agent exists to
// unblock.
func (fg *FakeGitHub) userHandler(script FakeGitHubScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fg.LastUserInfoUserAgent = r.Header.Get("User-Agent")
		fg.LastUserInfoAuthorizationHeader = r.Header.Get("Authorization")
		if isMissingDeliberateUserAgent(fg.LastUserInfoUserAgent) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if script.FailUserInfo {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"email": script.AccountEmail,
			"login": script.AccountLogin,
		})
	}
}

// listReposHandler serves GET /user/repos (github-list-repos): returns
// script.Repos as a top-level JSON array (GitHub's real shape) and records
// the visibility/per_page/page query parameters and the literal
// User-Agent/Accept headers the tool's own mapping declares.
func (fg *FakeGitHub) listReposHandler(script FakeGitHubScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fg.ReposCallCount++
		fg.LastReposQuery = r.URL.Query()
		fg.LastReposUserAgent = r.Header.Get("User-Agent")
		fg.LastReposAcceptHeader = r.Header.Get("Accept")
		if isMissingDeliberateUserAgent(fg.LastReposUserAgent) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		repos := make([]map[string]any, 0, len(script.Repos))
		for _, repo := range script.Repos {
			repos = append(repos, map[string]any{
				"id":        repo.ID,
				"name":      repo.Name,
				"full_name": repo.FullName,
				"private":   repo.Private,
				"html_url":  repo.HTMLURL,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(repos)
	}
}

// repoIssuesHandler serves everything under /repos/: GET
// {owner}/{repo}/issues (github-list-issues) and POST {owner}/{repo}/issues
// (github-create-issue) — distinguished by method, mirroring how the real
// GitHub API serves both list and create under one URL.
func (fg *FakeGitHub) repoIssuesHandler(script FakeGitHubScript) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/issues") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		ownerRepo := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/"), "/issues")
		switch r.Method {
		case http.MethodGet:
			fg.handleListIssues(w, r, ownerRepo, script)
		case http.MethodPost:
			fg.handleCreateIssue(w, r, ownerRepo, script)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

// handleListIssues serves GET /repos/{owner}/{repo}/issues
// (github-list-issues): returns script.Issues as a top-level JSON array,
// recording the decoded owner/repo path segment and the state/per_page query
// parameters plus the literal User-Agent/Accept headers.
func (fg *FakeGitHub) handleListIssues(w http.ResponseWriter, r *http.Request, ownerRepo string, script FakeGitHubScript) {
	fg.IssuesCallCount++
	fg.LastIssuesOwnerRepoPath = ownerRepo
	fg.LastIssuesQuery = r.URL.Query()
	fg.LastIssuesUserAgent = r.Header.Get("User-Agent")
	fg.LastIssuesAcceptHeader = r.Header.Get("Accept")
	if isMissingDeliberateUserAgent(fg.LastIssuesUserAgent) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	issues := make([]map[string]any, 0, len(script.Issues))
	for _, issue := range script.Issues {
		issues = append(issues, map[string]any{
			"id":       issue.ID,
			"number":   issue.Number,
			"title":    issue.Title,
			"body":     issue.Body,
			"state":    issue.State,
			"html_url": issue.HTMLURL,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(issues)
}

// handleCreateIssue serves POST /repos/{owner}/{repo}/issues
// (github-create-issue): echoes back the decoded title/body it received, or
// returns a scripted error status/body proving an upstream rejection (e.g.
// HTTP 422) surfaces as a tool-level failure.
func (fg *FakeGitHub) handleCreateIssue(w http.ResponseWriter, r *http.Request, ownerRepo string, script FakeGitHubScript) {
	fg.CreateIssueCallCount++
	fg.LastCreateIssueOwnerRepoPath = ownerRepo
	fg.LastCreateIssueUserAgent = r.Header.Get("User-Agent")
	fg.LastCreateIssueAcceptHeader = r.Header.Get("Accept")
	if isMissingDeliberateUserAgent(fg.LastCreateIssueUserAgent) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if script.CreateIssueStatus != 0 {
		w.WriteHeader(script.CreateIssueStatus)
		if script.CreateIssueBody != "" {
			_, _ = w.Write([]byte(script.CreateIssueBody))
		}
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	fg.LastCreateIssueBody = body
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":       1,
		"number":   42,
		"title":    body["title"],
		"body":     body["body"],
		"state":    "open",
		"html_url": "https://github.com/" + ownerRepo + "/issues/42",
	})
}
