//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for
// the package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, oauthJourneyFixture,
// openConnectPageAndGetState, doJSONRequest, listTools,
// listTriggerDefinitions/triggerDefinitionsPageDTO
// (trigger_definitions_journey_integration_test.go),
// executionResultWithCursorDTO, executeHubspotTool (hubspot_journey_
// integration_test.go — a provider-agnostic tool-execute-with-cursor helper
// despite its name) — same package). This file tells the Providers strand's
// Slice 4 story end to end against the real composition root: GitHub arrives
// purely as a definition file (github.yaml, no provider-specific Go code)
// declaring three tools that each carry their own literal
// User-Agent/Accept headers (PD84) and no trigger; github-list-issues
// URL-escapes {input.owner}/{input.repo} into the issues path;
// github-create-issue posts a JSON body and an upstream 422 surfaces as a
// tool-level failure; github-list-repos forwards its
// visibility/per_page/page query mapping; and the real OAuth callback now
// activates a GitHub connection with captured account identity because
// connections/driven/oauthhttp's default User-Agent (PD83) unblocks the
// account-fetch GET https://api.github.com/user, which FakeGitHub —
// mirroring the real GitHub API — rejects with 403 when no User-Agent header
// is present.
package crucial_path

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"beecon/internal/app"
	"beecon/internal/catalog"
	"beecon/test/support"
)

// githubDefinitionAgainst is GitHub's real github.yaml shape, re-expressed as
// a catalog.ProviderDefinition pointed at fg instead of the real internet:
// the OAuth block's email->email/displayName->login userInfo mapping, and
// the three tools' declared mappings — each carrying the literal
// User-Agent/Accept headers github.yaml declares per tool (PD84).
func githubDefinitionAgainst(fg *support.FakeGitHub) []catalog.ProviderDefinition {
	githubHeaders := map[string]string{
		"User-Agent": "Beecon",
		"Accept":     "application/vnd.github+json",
	}
	return []catalog.ProviderDefinition{
		{
			Slug:         "github",
			Name:         "GitHub",
			Logo:         "https://static.beecon.dev/providers/github.png",
			AuthScheme:   "oauth2",
			BaseURL:      fg.BaseURL,
			AuthorizeURL: "https://fake-github.example.com/login/oauth/authorize",
			TokenURL:     fg.TokenURL,
			UserInfoURL:  fg.UserInfoURL,
			Scopes:       []string{"repo", "read:user"},
			UserInfo:     catalog.UserInfoMapping{EmailField: "email", DisplayNameField: "login"},
			Tools: []catalog.ProviderTool{
				{
					Slug:        "github-list-repos",
					Name:        "List repositories",
					Description: "List repositories owned by (or accessible to) the authenticated user, page-paginated.",
					Method:      "GET",
					Path:        "/user/repos",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"visibility": map[string]any{"type": "string"},
							"perPage":    map[string]any{"type": "integer"},
							"page":       map[string]any{"type": "integer"},
						},
					},
					OutputSchema: map[string]any{"type": "array"},
					Mapping: catalog.Mapping{
						Query: map[string]string{
							"visibility": "{input.visibility}",
							"per_page":   "{input.perPage}",
							"page":       "{input.page}",
						},
						Header: githubHeaders,
					},
				},
				{
					Slug:        "github-list-issues",
					Name:        "List issues",
					Description: "List issues in a repository, page-paginated.",
					Method:      "GET",
					Path:        "/repos/{input.owner}/{input.repo}/issues",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"owner":   map[string]any{"type": "string"},
							"repo":    map[string]any{"type": "string"},
							"state":   map[string]any{"type": "string"},
							"perPage": map[string]any{"type": "integer"},
						},
						"required": []any{"owner", "repo"},
					},
					OutputSchema: map[string]any{"type": "array"},
					Mapping: catalog.Mapping{
						Query: map[string]string{
							"state":    "{input.state}",
							"per_page": "{input.perPage}",
						},
						Header: githubHeaders,
					},
				},
				{
					Slug:        "github-create-issue",
					Name:        "Create issue",
					Description: "Create a new issue in a repository.",
					Method:      "POST",
					Path:        "/repos/{input.owner}/{input.repo}/issues",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"owner": map[string]any{"type": "string"},
							"repo":  map[string]any{"type": "string"},
							"title": map[string]any{"type": "string"},
							"body":  map[string]any{"type": "string"},
						},
						"required": []any{"owner", "repo", "title"},
					},
					OutputSchema: map[string]any{"type": "object"},
					Mapping: catalog.Mapping{
						Body: map[string]string{
							"title": "{input.title}",
							"body":  "{input.body}",
						},
						Header: githubHeaders,
					},
				},
			},
		},
	}
}

// newGithubJourneyFixture is newOAuthJourneyFixture
// (oauth_handshake_journey_integration_test.go), re-pointed at the "github"
// providerSlug — mirrors newGmailJourneyFixture/newSlackJourneyFixture.
func newGithubJourneyFixture(t *testing.T, wired *app.Wired) oauthJourneyFixture {
	t.Helper()
	adminAuth := "Bearer " + support.AdminAPIKey
	const allowedRedirectURI = "https://consumer.example.com/github-callback"

	var org organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Acme GitHub"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}

	var integration integrationSummaryDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"github","clientId":"github-client-id","clientSecret":"github-client-secret"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create integration status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &integration); err != nil {
		t.Fatalf("decode integration: %v", err)
	}

	w = doJSONRequest(t, wired.Router, http.MethodPatch, "/api/v1/organizations/"+org.ID, adminAuth,
		`{"allowedRedirectUris":["`+allowedRedirectURI+`"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("set allow-list status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var orgKey issuedKeyDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &orgKey); err != nil {
		t.Fatalf("decode key: %v", err)
	}
	orgAuth := "Bearer " + orgKey.Key

	var user userDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", orgAuth, `{"name":"Ada Lovelace"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode user: %v", err)
	}

	return oauthJourneyFixture{
		orgAuth:            orgAuth,
		userID:             user.ID,
		integrationID:      integration.ID,
		allowedRedirectURI: allowedRedirectURI,
	}
}

// activateGithubConnectionViaCallback drives the real OAuth handshake —
// initiate, open the connect page, and the callback with a fake
// authorization code — through live HTTP requests against the real
// composition root, and returns the resulting connection's stable id.
// Mirrors activateSlackConnectionViaCallback/activateGmailConnection.
func activateGithubConnectionViaCallback(t *testing.T, wired *app.Wired, fixture oauthJourneyFixture) initiatedConnectionDTO {
	t.Helper()
	initiated := fixture.initiate(t, wired)
	state := openConnectPageAndGetState(t, wired, initiated)

	w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")
	if w.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (handshake must complete); body=%s", w.Code, http.StatusFound, w.Body.String())
	}
	return initiated
}

// TestGitHubJourney_DefinitionLoadsAtBootWithThreeToolsAndZeroTriggers is the
// boot-load AC: booted against the real embedded providers/ directory (not a
// fake), the catalog lists all three GitHub tools under provider slug
// "github", each with a non-empty input and output schema, and
// trigger-definitions surfaces exactly zero triggers for it (PD84: GitHub
// ships no trigger in this strand) — proving GitHub arrived purely as a
// definition file (mirrors
// TestSlackJourney_DefinitionLoadsAtBootWithNoUserInfoAndZeroTriggers).
func TestGitHubJourney_DefinitionLoadsAtBootWithThreeToolsAndZeroTriggers(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	var org organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Acme"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}
	var orgKey issuedKeyDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &orgKey); err != nil {
		t.Fatalf("decode key: %v", err)
	}
	orgAuth := "Bearer " + orgKey.Key

	t.Run("tools list surfaces all three GitHub tools with non-empty schemas", func(t *testing.T) {
		status, page := listTools(t, wired, orgAuth, "?providerSlug=github")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		wantSlugs := map[string]bool{"github-list-repos": false, "github-list-issues": false, "github-create-issue": false}
		for _, item := range page.Items {
			if _, declared := wantSlugs[item.Slug]; declared {
				wantSlugs[item.Slug] = true
			}
			if item.Provider.Slug != "github" {
				t.Errorf("item %q provider.slug = %q, want %q", item.Slug, item.Provider.Slug, "github")
			}
			if len(item.InputSchema) == 0 || len(item.OutputSchema) == 0 {
				t.Errorf("item %q has an empty input/output schema", item.Slug)
			}
		}
		for slug, found := range wantSlugs {
			if !found {
				t.Errorf("tools list %+v is missing %q", page.Items, slug)
			}
		}
	})

	t.Run("trigger-definitions surfaces zero GitHub triggers", func(t *testing.T) {
		status, page := listTriggerDefinitions(t, wired, orgAuth, "?providerSlug=github")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if len(page.Items) != 0 {
			t.Errorf("items = %+v, want zero triggers (GitHub declares none, PD84)", page.Items)
		}
	})
}

// TestGitHubJourney_ListIssuesSubstitutesOwnerAndRepoAndSendsTheDeclaredHeaders
// is AC2/AC3: {input.owner}/{input.repo} are URL-escaped into
// /repos/{owner}/{repo}/issues, the state/per_page query mapping reaches
// GitHub, the tool call returns the issues array as Data, and — the load-
// bearing header assertion — FakeGitHub observed the tool's own literal
// User-Agent: Beecon and Accept: application/vnd.github+json headers (GitHub
// rejects requests carrying neither).
func TestGitHubJourney_ListIssuesSubstitutesOwnerAndRepoAndSendsTheDeclaredHeaders(t *testing.T) {
	fakeGitHub := support.NewFakeGitHub(t, support.FakeGitHubScript{
		AccessToken: "gho_faketoken", AccountEmail: "ada@example.com", AccountLogin: "ada",
		Issues: []support.FakeGitHubIssue{
			{ID: 1, Number: 1, Title: "First bug", Body: "It crashes", State: "open", HTMLURL: "https://github.com/octo/widgets/issues/1"},
			{ID: 2, Number: 2, Title: "Second bug", Body: "It also crashes", State: "open", HTMLURL: "https://github.com/octo/widgets/issues/2"},
		},
	})
	wired := support.BootAppWithProviderDefinitions(t, githubDefinitionAgainst(fakeGitHub))
	fixture := newGithubJourneyFixture(t, wired)
	initiated := activateGithubConnectionViaCallback(t, wired, fixture)

	const owner = "octo cat"
	const repo = "widgets/next"

	status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "github-list-issues", fixture.userID, initiated.ID,
		`{"owner":"`+owner+`","repo":"`+repo+`","state":"open","perPage":10}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if !dto.Successful {
		t.Fatalf("successful = false, want true; error = %+v", dto.Error)
	}
	issues, ok := dto.Data.([]any)
	if !ok {
		t.Fatalf("data = %T, want a top-level JSON array of issues", dto.Data)
	}
	if len(issues) != 2 {
		t.Fatalf("len(issues) = %d, want 2", len(issues))
	}

	t.Run("owner and repo were URL-escaped and substituted into the issues path", func(t *testing.T) {
		if fakeGitHub.IssuesCallCount != 1 {
			t.Fatalf("IssuesCallCount = %d, want 1", fakeGitHub.IssuesCallCount)
		}
		wantPath := owner + "/" + repo
		if fakeGitHub.LastIssuesOwnerRepoPath != wantPath {
			t.Errorf("LastIssuesOwnerRepoPath = %q, want %q (decoded off the wire)", fakeGitHub.LastIssuesOwnerRepoPath, wantPath)
		}
	})

	t.Run("state and per_page query parameters reached GitHub", func(t *testing.T) {
		if got := fakeGitHub.LastIssuesQuery.Get("state"); got != "open" {
			t.Errorf("state = %q, want %q", got, "open")
		}
		if got := fakeGitHub.LastIssuesQuery.Get("per_page"); got != "10" {
			t.Errorf("per_page = %q, want %q", got, "10")
		}
	})

	t.Run("GitHub observed the tool's own literal User-Agent and Accept headers", func(t *testing.T) {
		if fakeGitHub.LastIssuesUserAgent != "Beecon" {
			t.Errorf("User-Agent = %q, want %q", fakeGitHub.LastIssuesUserAgent, "Beecon")
		}
		if fakeGitHub.LastIssuesAcceptHeader != "application/vnd.github+json" {
			t.Errorf("Accept = %q, want %q", fakeGitHub.LastIssuesAcceptHeader, "application/vnd.github+json")
		}
	})
}

// TestGitHubJourney_CreateIssuePostsAJSONBodyAndAnUpstream422SurfacesAsAToolLevelFailure
// is AC4/AC6: github-create-issue's body mapping builds {"title":...,
// "body":...} under the declared literal headers, a genuine creation
// succeeds, and a scripted upstream 422 (GitHub's real validation-failure
// status) surfaces as a tool-level failure carrying the provider's status
// and message, not a platform HTTP error.
func TestGitHubJourney_CreateIssuePostsAJSONBodyAndAnUpstream422SurfacesAsAToolLevelFailure(t *testing.T) {
	t.Run("a successful creation posts the mapped JSON body under the declared headers", func(t *testing.T) {
		fakeGitHub := support.NewFakeGitHub(t, support.FakeGitHubScript{AccessToken: "gho_faketoken", AccountEmail: "ada@example.com", AccountLogin: "ada"})
		wired := support.BootAppWithProviderDefinitions(t, githubDefinitionAgainst(fakeGitHub))
		fixture := newGithubJourneyFixture(t, wired)
		initiated := activateGithubConnectionViaCallback(t, wired, fixture)

		status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "github-create-issue", fixture.userID, initiated.ID,
			`{"owner":"octo","repo":"widgets","title":"Something broke","body":"Steps to reproduce..."}`)

		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if !dto.Successful {
			t.Fatalf("successful = false, want true; error = %+v", dto.Error)
		}
		data, ok := dto.Data.(map[string]any)
		if !ok {
			t.Fatalf("data = %T, want the created-issue object", dto.Data)
		}
		if data["title"] != "Something broke" {
			t.Errorf("data.title = %v, want %q", data["title"], "Something broke")
		}
		if fakeGitHub.LastCreateIssueBody == nil {
			t.Fatal("GitHub received no create-issue body")
		}
		if fakeGitHub.LastCreateIssueBody["title"] != "Something broke" {
			t.Errorf(`body["title"] = %v, want %q`, fakeGitHub.LastCreateIssueBody["title"], "Something broke")
		}
		if fakeGitHub.LastCreateIssueBody["body"] != "Steps to reproduce..." {
			t.Errorf(`body["body"] = %v, want %q`, fakeGitHub.LastCreateIssueBody["body"], "Steps to reproduce...")
		}
		if fakeGitHub.LastCreateIssueUserAgent != "Beecon" {
			t.Errorf("User-Agent = %q, want %q", fakeGitHub.LastCreateIssueUserAgent, "Beecon")
		}
		if fakeGitHub.LastCreateIssueAcceptHeader != "application/vnd.github+json" {
			t.Errorf("Accept = %q, want %q", fakeGitHub.LastCreateIssueAcceptHeader, "application/vnd.github+json")
		}
	})

	t.Run("an upstream 422 surfaces as a tool-level failure carrying GitHub's status and message", func(t *testing.T) {
		fakeGitHub := support.NewFakeGitHub(t, support.FakeGitHubScript{
			AccessToken: "gho_faketoken", AccountEmail: "ada@example.com", AccountLogin: "ada",
			CreateIssueStatus: http.StatusUnprocessableEntity,
			CreateIssueBody:   `{"message":"Validation Failed","errors":[{"code":"missing_field","field":"title"}]}`,
		})
		wired := support.BootAppWithProviderDefinitions(t, githubDefinitionAgainst(fakeGitHub))
		fixture := newGithubJourneyFixture(t, wired)
		initiated := activateGithubConnectionViaCallback(t, wired, fixture)

		status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "github-create-issue", fixture.userID, initiated.ID,
			`{"owner":"octo","repo":"widgets","title":""}`)

		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d (an upstream error is a tool-level failure, not an HTTP error)", status, http.StatusOK)
		}
		if dto.Successful {
			t.Error("successful = true, want false for an upstream 422 response")
		}
		if dto.Data != nil {
			t.Errorf("data = %v, want nil for a failed execution", dto.Data)
		}
		if dto.Error == nil {
			t.Fatal("error is nil, want the provider's status and message")
		}
		if !strings.Contains(dto.Error.Message, "422") {
			t.Errorf("error.message = %q, want it to surface the provider's status code", dto.Error.Message)
		}
		if !strings.Contains(dto.Error.Message, "Validation Failed") {
			t.Errorf("error.message = %q, want it to surface the provider's response body", dto.Error.Message)
		}
	})
}

// TestGitHubJourney_ListReposForwardsVisibilityPerPageAndPageQueryParameters
// is AC5: github-list-repos maps visibility/perPage/page onto GitHub's own
// visibility/per_page/page query parameters and returns the repos array as
// Data (GitHub paginates /user/repos by page number, not an opaque cursor —
// no Mapping.Pagination is declared for this tool).
func TestGitHubJourney_ListReposForwardsVisibilityPerPageAndPageQueryParameters(t *testing.T) {
	fakeGitHub := support.NewFakeGitHub(t, support.FakeGitHubScript{
		AccessToken: "gho_faketoken", AccountEmail: "ada@example.com", AccountLogin: "ada",
		Repos: []support.FakeGitHubRepo{
			{ID: 1, Name: "widgets", FullName: "octo/widgets", Private: false, HTMLURL: "https://github.com/octo/widgets"},
			{ID: 2, Name: "gadgets", FullName: "octo/gadgets", Private: true, HTMLURL: "https://github.com/octo/gadgets"},
		},
	})
	wired := support.BootAppWithProviderDefinitions(t, githubDefinitionAgainst(fakeGitHub))
	fixture := newGithubJourneyFixture(t, wired)
	initiated := activateGithubConnectionViaCallback(t, wired, fixture)

	status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "github-list-repos", fixture.userID, initiated.ID,
		`{"visibility":"private","perPage":25,"page":2}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if !dto.Successful {
		t.Fatalf("successful = false, want true; error = %+v", dto.Error)
	}
	repos, ok := dto.Data.([]any)
	if !ok {
		t.Fatalf("data = %T, want a top-level JSON array of repos", dto.Data)
	}
	if len(repos) != 2 {
		t.Fatalf("len(repos) = %d, want 2", len(repos))
	}

	if got := fakeGitHub.LastReposQuery.Get("visibility"); got != "private" {
		t.Errorf("visibility = %q, want %q", got, "private")
	}
	if got := fakeGitHub.LastReposQuery.Get("per_page"); got != "25" {
		t.Errorf("per_page = %q, want %q", got, "25")
	}
	if got := fakeGitHub.LastReposQuery.Get("page"); got != "2" {
		t.Errorf("page = %q, want %q", got, "2")
	}
}

// TestGitHubJourney_ActivatesViaTheRealOAuthCallbackBecauseOfTheDefaultUserAgent
// is the activation AC this slice's one code change (PD83) exists for: FakeGitHub
// rejects any token-exchange or GET /user request carrying no User-Agent
// header with 403 (mirroring GitHub's real API), exactly as a bare
// oauthhttp.Client would have failed before the default was added. Driving
// the real initiate -> connect-page -> callback handshake against this fake
// still reaches ACTIVE with captured account identity, proving
// connections/driven/oauthhttp's default User-Agent (PD83) is what makes
// this succeed — this test would fail if that default were removed.
func TestGitHubJourney_ActivatesViaTheRealOAuthCallbackBecauseOfTheDefaultUserAgent(t *testing.T) {
	fakeGitHub := support.NewFakeGitHub(t, support.FakeGitHubScript{
		AccessToken: "gho_faketoken", AccountEmail: "ada@example.com", AccountLogin: "ada",
	})
	wired := support.BootAppWithProviderDefinitions(t, githubDefinitionAgainst(fakeGitHub))
	fixture := newGithubJourneyFixture(t, wired)
	initiated := fixture.initiate(t, wired)
	state := openConnectPageAndGetState(t, wired, initiated)

	w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")

	if w.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want %d; body=%s", w.Code, http.StatusFound, w.Body.String())
	}
	parsed, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if got := parsed.Query().Get("status"); got != "success" {
		t.Errorf("status = %q, want %q", got, "success")
	}

	t.Run("the connection reaches ACTIVE with captured account identity", func(t *testing.T) {
		got := fixture.getConnection(t, wired, initiated.ID)
		if got.Status != "ACTIVE" {
			t.Fatalf("status = %q, want %q", got.Status, "ACTIVE")
		}
		if got.Account == nil {
			t.Fatal("account = nil, want captured email/login")
		}
		if got.Account.Email != "ada@example.com" {
			t.Errorf("account.email = %q, want %q", got.Account.Email, "ada@example.com")
		}
		if got.Account.DisplayName != "ada" {
			t.Errorf("account.displayName = %q, want %q (GitHub's login field, PD84)", got.Account.DisplayName, "ada")
		}
	})

	t.Run("FakeGitHub observed a non-empty User-Agent on the token exchange and the account-fetch — the default this slice ships", func(t *testing.T) {
		if fakeGitHub.LastTokenUserAgent == "" {
			t.Error("LastTokenUserAgent is empty — the token exchange would have been 403'd without oauthhttp's default User-Agent (PD83)")
		}
		if fakeGitHub.LastUserInfoUserAgent == "" {
			t.Error("LastUserInfoUserAgent is empty — GET /user would have been 403'd without oauthhttp's default User-Agent (PD83)")
		}
	})
}
