// Package connectweb_test exercises the connect-page/callback HTML handlers
// through an actual chi router, backed by the driven/memory connections
// facade and hand-written fakes for the narrow cross-module reader ports —
// the same shape connections/driving/httpapi/handler_test.go uses. Covers
// Slice 4's AC1-AC9 at the HTTP-rendering layer.
package connectweb_test

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	memory "beecon/internal/connections/driven/memory"
	"beecon/internal/connectweb"
	"beecon/internal/organizations"
)

type fakeOrgReader struct {
	orgs map[organizations.OrgID]organizations.Organization
}

func (f fakeOrgReader) Get(_ context.Context, id organizations.OrgID) (organizations.Organization, error) {
	org, ok := f.orgs[id]
	if !ok {
		return organizations.Organization{}, organizations.ErrNotFound()
	}
	return org, nil
}

type fakeUserReader struct {
	users map[organizations.UserID]organizations.User
}

func (f fakeUserReader) GetUser(_ context.Context, org organizations.OrgID, id organizations.UserID) (organizations.User, error) {
	user, ok := f.users[id]
	if !ok || user.OrgID != org {
		return organizations.User{}, organizations.ErrUserNotFound()
	}
	return user, nil
}

type fakeIntegrationReader struct {
	integrations map[catalog.IntegrationID]catalog.Integration
}

func (f fakeIntegrationReader) GetIntegration(_ context.Context, id catalog.IntegrationID) (catalog.Integration, error) {
	integration, ok := f.integrations[id]
	if !ok {
		return catalog.Integration{}, catalog.ErrIntegrationNotFound()
	}
	return integration, nil
}

type fakeProviderReader struct {
	definitions map[string]catalog.ProviderDefinition
}

func (f fakeProviderReader) GetProviderDefinition(_ context.Context, slug string) (catalog.ProviderDefinition, error) {
	definition, ok := f.definitions[slug]
	if !ok {
		return catalog.ProviderDefinition{}, catalog.ErrUnknownProvider(slug)
	}
	return definition, nil
}

type fakeOAuthClient struct {
	exchangeResult connections.TokenExchangeResult
	exchangeErr    error
	accountResult  connections.AccountInfo
	accountErr     error
}

func (f *fakeOAuthClient) ExchangeCode(_ context.Context, _ connections.TokenExchangeRequest) (connections.TokenExchangeResult, error) {
	if f.exchangeErr != nil {
		return connections.TokenExchangeResult{}, f.exchangeErr
	}
	return f.exchangeResult, nil
}

func (f *fakeOAuthClient) FetchAccount(_ context.Context, _ string, _ string) (connections.AccountInfo, error) {
	if f.accountErr != nil {
		return connections.AccountInfo{}, f.accountErr
	}
	return f.accountResult, nil
}

type failingExchangeError struct{}

func (failingExchangeError) Error() string { return "provider rejected the exchange" }

const (
	testOrg             = organizations.OrgID("org_1")
	testUser            = organizations.UserID("user_1")
	testIntegration     = catalog.IntegrationID("intg_1")
	testProviderSlug    = "outlook"
	allowedRedirect     = "https://consumer.example.com/callback"
	testProviderName    = "Outlook"
	testProviderLogoURL = "https://static.beecon.dev/providers/outlook.png"
)

func testProviderDefinition() catalog.ProviderDefinition {
	return catalog.ProviderDefinition{
		Slug:         testProviderSlug,
		Name:         testProviderName,
		Logo:         testProviderLogoURL,
		AuthScheme:   "oauth2",
		AuthorizeURL: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		TokenURL:     "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		UserInfoURL:  "https://graph.microsoft.com/v1.0/me",
		Scopes:       []string{"offline_access", "Mail.Read"},
	}
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

// testFixture wires connectweb's handlers behind an actual chi router,
// backed by the driven/memory connections facade.
type testFixture struct {
	router chi.Router
	facade *connections.Facade
	clock  *clock
}

func newTestFixture(t *testing.T, oauthClient connections.OAuthClient) *testFixture {
	t.Helper()
	orgs := map[organizations.OrgID]organizations.Organization{
		testOrg: {ID: testOrg, Name: "Acme", AllowedRedirectURIs: []string{allowedRedirect}},
	}
	c := &clock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	facade := memory.NewFacadeWithOverrides(memory.Overrides{
		Organizations: fakeOrgReader{orgs: orgs},
		Users:         fakeUserReader{users: map[organizations.UserID]organizations.User{testUser: {ID: testUser, OrgID: testOrg, Name: "Ada"}}},
		Integrations:  fakeIntegrationReader{integrations: map[catalog.IntegrationID]catalog.Integration{testIntegration: {ID: testIntegration, ProviderSlug: testProviderSlug, ClientID: "cid", ClientSecret: "csecret"}}},
		Providers:     fakeProviderReader{definitions: map[string]catalog.ProviderDefinition{testProviderSlug: testProviderDefinition()}},
		OAuthClient:   oauthClient,
		Now:           c.Now,
	})
	handler, err := connectweb.NewHandler(facade)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	r := chi.NewRouter()
	r.Get("/connect/{token}", handler.ConnectPage)
	r.Get("/connect/oauth/callback", handler.Callback)
	return &testFixture{router: r, facade: facade, clock: c}
}

func (f *testFixture) initiate(t *testing.T) connections.InitiatedConnection {
	t.Helper()
	initiated, err := f.facade.Initiate(context.Background(), testOrg, testUser, testIntegration, allowedRedirect)
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	return initiated
}

func (f *testFixture) getConnectPage(token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/connect/"+token, nil)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func (f *testFixture) getCallback(query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/connect/oauth/callback?"+query, nil)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

var hrefPattern = regexp.MustCompile(`href="([^"]+)"`)

// extractHref pulls the first href attribute value out of an HTML body and
// unescapes HTML entities (html/template escapes "&" to "&amp;" and "+" to
// "&#43;" inside attribute values) — a browser's HTML parser does this
// transparently before treating the value as a URL; a raw regex match does
// not.
func extractHref(t *testing.T, body string) string {
	t.Helper()
	match := hrefPattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("no href found in body: %s", body)
	}
	return html.UnescapeString(match[1])
}

func queryParam(t *testing.T, rawURL, key string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL %q: %v", rawURL, err)
	}
	return parsed.Query().Get(key)
}

// --- GET /connect/{token} (AC1, AC2, AC3) ---

func TestConnectPage_ValidLinkRendersProviderNameLogoAndAConnectAction(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})
	initiated := f.initiate(t)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")

	w := f.getConnectPage(token)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, testProviderName) {
		t.Errorf("body does not contain the provider name %q: %s", testProviderName, body)
	}
	if !strings.Contains(body, testProviderLogoURL) {
		t.Errorf("body does not contain the provider logo URL %q: %s", testProviderLogoURL, body)
	}
	authorizeURL := extractHref(t, body)
	if queryParam(t, authorizeURL, "client_id") != "cid" {
		t.Errorf("Connect action's href %q does not carry the Integration's client_id", authorizeURL)
	}
}

func TestConnectPage_InvalidTokenShowsErrorPageAndNeverForwardsToTheProvider(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})

	w := f.getConnectPage("token-that-does-not-exist")

	if w.Code == http.StatusOK {
		t.Fatalf("status = %d, want a non-200 error status; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "login.microsoftonline.com") {
		t.Errorf("error page body %s must never contain a provider URL", body)
	}
	if strings.Contains(body, "Connect") && strings.Contains(body, "href=") {
		t.Errorf("error page body %s must not carry a Connect action", body)
	}
}

func TestConnectPage_ExpiredLinkShowsErrorPageAndNeverForwardsToTheProvider(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})
	initiated := f.initiate(t)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")
	f.clock.now = f.clock.now.Add(connections.ConnectLinkTTL + time.Minute)

	w := f.getConnectPage(token)

	if w.Code == http.StatusOK {
		t.Fatalf("status = %d, want a non-200 error status; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "login.microsoftonline.com") {
		t.Errorf("expired-link error page must never contain a provider URL: %s", w.Body.String())
	}
}

func TestConnectPage_AlreadyCompletedLinkShowsErrorPageAndNeverForwardsToTheProvider(t *testing.T) {
	client := &fakeOAuthClient{
		exchangeResult: connections.TokenExchangeResult{AccessToken: "at", RefreshToken: "rt"},
		accountResult:  connections.AccountInfo{Email: "ada@example.com", DisplayName: "Ada"},
	}
	f := newTestFixture(t, client)
	initiated := f.initiate(t)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")

	openPage := f.getConnectPage(token)
	authorizeURL := extractHref(t, openPage.Body.String())
	state := queryParam(t, authorizeURL, "state")
	callback := f.getCallback("code=auth-code&state=" + state)
	if callback.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (redirect on activation); body=%s", callback.Code, http.StatusFound, callback.Body.String())
	}

	w := f.getConnectPage(token)

	if w.Code == http.StatusOK {
		t.Fatalf("status = %d, want a non-200 error status for an already-completed link; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "login.microsoftonline.com") {
		t.Errorf("already-completed error page must never contain a provider URL: %s", w.Body.String())
	}
}

// --- GET /connect/oauth/callback (AC4, AC7, AC8, AC9) ---

func TestCallback_HappyPathRedirectsToConsumerRedirectURIWithConnectionIDAndSuccessStatus(t *testing.T) {
	client := &fakeOAuthClient{
		exchangeResult: connections.TokenExchangeResult{AccessToken: "raw-access-token", RefreshToken: "raw-refresh-token"},
		accountResult:  connections.AccountInfo{Email: "ada@example.com", DisplayName: "Ada Lovelace"},
	}
	f := newTestFixture(t, client)
	initiated := f.initiate(t)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")
	openPage := f.getConnectPage(token)
	state := queryParam(t, extractHref(t, openPage.Body.String()), "state")

	w := f.getCallback("code=auth-code&state=" + state)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusFound, w.Body.String())
	}
	location := w.Header().Get("Location")
	if got := queryParam(t, location, "connectionId"); got != string(initiated.Connection.ID) {
		t.Errorf("Location connectionId = %q, want the stable id %q", got, initiated.Connection.ID)
	}
	if got := queryParam(t, location, "status"); got != "success" {
		t.Errorf("Location status = %q, want %q", got, "success")
	}
	if strings.Contains(location, "raw-access-token") || strings.Contains(location, "raw-refresh-token") {
		t.Errorf("redirect Location %q must never contain a raw token", location)
	}
}

func TestCallback_MissingStateShowsErrorPage(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})

	w := f.getCallback("code=auth-code")

	if w.Code == http.StatusFound {
		t.Fatalf("status = %d, want an error status, not a redirect; body=%s", w.Code, w.Body.String())
	}
}

func TestCallback_UnknownStateShowsErrorPage(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})

	w := f.getCallback("code=auth-code&state=nobody-minted-this")

	if w.Code == http.StatusFound {
		t.Fatalf("status = %d, want an error status, not a redirect; body=%s", w.Code, w.Body.String())
	}
}

func TestCallback_AlreadyUsedStateShowsErrorPageOnTheSecondAttempt(t *testing.T) {
	client := &fakeOAuthClient{
		exchangeResult: connections.TokenExchangeResult{AccessToken: "at", RefreshToken: "rt"},
		accountResult:  connections.AccountInfo{Email: "ada@example.com", DisplayName: "Ada"},
	}
	f := newTestFixture(t, client)
	initiated := f.initiate(t)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")
	openPage := f.getConnectPage(token)
	state := queryParam(t, extractHref(t, openPage.Body.String()), "state")
	first := f.getCallback("code=auth-code&state=" + state)
	if first.Code != http.StatusFound {
		t.Fatalf("first callback status = %d, want %d; body=%s", first.Code, http.StatusFound, first.Body.String())
	}

	second := f.getCallback("code=auth-code&state=" + state)

	if second.Code == http.StatusFound {
		t.Fatalf("second callback with an already-used state status = %d, want an error status", second.Code)
	}
}

func TestCallback_ConsentDenialRedirectsToConsumerRedirectURIWithErrorStatus(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})
	initiated := f.initiate(t)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")
	openPage := f.getConnectPage(token)
	state := queryParam(t, extractHref(t, openPage.Body.String()), "state")

	w := f.getCallback("state=" + state + "&error=access_denied")

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect back to consumer even on denial); body=%s", w.Code, http.StatusFound, w.Body.String())
	}
	location := w.Header().Get("Location")
	if got := queryParam(t, location, "status"); got != "error" {
		t.Errorf("Location status = %q, want %q", got, "error")
	}
}

func TestCallback_TokenExchangeFailureShowsErrorPage(t *testing.T) {
	client := &fakeOAuthClient{exchangeErr: failingExchangeError{}}
	f := newTestFixture(t, client)
	initiated := f.initiate(t)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")
	openPage := f.getConnectPage(token)
	state := queryParam(t, extractHref(t, openPage.Body.String()), "state")

	w := f.getCallback("code=auth-code&state=" + state)

	if w.Code == http.StatusFound {
		t.Fatalf("status = %d, want an error status, not a redirect; body=%s", w.Code, w.Body.String())
	}
}
