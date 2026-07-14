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

func (f *fakeOAuthClient) FetchAccount(_ context.Context, _ connections.AccountFetchRequest) (connections.AccountInfo, error) {
	if f.accountErr != nil {
		return connections.AccountInfo{}, f.accountErr
	}
	return f.accountResult, nil
}

// RefreshGrant is unused by this file's connect-page/callback rendering
// tests (Slice 4); it satisfies connections.OAuthClient with a harmless
// default.
func (f *fakeOAuthClient) RefreshGrant(_ context.Context, _ connections.RefreshGrantRequest) (connections.TokenExchangeResult, error) {
	return f.exchangeResult, f.exchangeErr
}

// FetchUserInfo is unused by this file's connect-page/callback rendering
// tests (Slice 5); it satisfies connections.OAuthClient with a harmless
// default.
func (f *fakeOAuthClient) FetchUserInfo(_ context.Context, _, _ string) error {
	return nil
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

	// testParamsIntegration/testParamsProviderSlug are a second Integration
	// whose provider declares expected pre-auth params (Slice 3), registered
	// alongside the plain Outlook fixture in every testFixture so tests can
	// exercise either flow from the same router.
	testParamsIntegration  = catalog.IntegrationID("intg_params")
	testParamsProviderSlug = "params-provider"
	testParamsProviderName = "Params Provider"
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

// testParamsProviderDefinition declares a required non-secret "region" and a
// required secret "apiKey" expected param (Slice 3, AC3-AC5).
func testParamsProviderDefinition() catalog.ProviderDefinition {
	return catalog.ProviderDefinition{
		Slug:         testParamsProviderSlug,
		Name:         testParamsProviderName,
		Logo:         "https://static.beecon.dev/providers/params-provider.png",
		AuthScheme:   "oauth2",
		AuthorizeURL: "https://params-provider.example.com/oauth2/authorize",
		TokenURL:     "https://params-provider.example.com/oauth2/token",
		Scopes:       []string{"read"},
		ExpectedParams: []catalog.ExpectedParam{
			{Name: "region", DisplayName: "Region", Description: "Your account's region.", Required: true, Secret: false},
			{Name: "apiKey", DisplayName: "API Key", Description: "Your account's API key.", Required: true, Secret: true},
		},
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
		Integrations: fakeIntegrationReader{integrations: map[catalog.IntegrationID]catalog.Integration{
			testIntegration:       {ID: testIntegration, ProviderSlug: testProviderSlug, ClientID: "cid", ClientSecret: "csecret"},
			testParamsIntegration: {ID: testParamsIntegration, ProviderSlug: testParamsProviderSlug, ClientID: "params-cid", ClientSecret: "params-csecret"},
		}},
		Providers: fakeProviderReader{definitions: map[string]catalog.ProviderDefinition{
			testProviderSlug:       testProviderDefinition(),
			testParamsProviderSlug: testParamsProviderDefinition(),
		}},
		OAuthClient: oauthClient,
		Now:         c.Now,
	})
	handler, err := connectweb.NewHandler(facade)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	r := chi.NewRouter()
	r.Get("/connect/{token}", handler.ConnectPage)
	r.Post("/connect/{token}/params", handler.SubmitParams)
	r.Get("/connect/oauth/callback", handler.Callback)
	return &testFixture{router: r, facade: facade, clock: c}
}

func (f *testFixture) initiate(t *testing.T) connections.InitiatedConnection {
	t.Helper()
	return f.initiateForIntegration(t, testIntegration)
}

func (f *testFixture) initiateForIntegration(t *testing.T, integrationID catalog.IntegrationID) connections.InitiatedConnection {
	t.Helper()
	initiated, err := f.facade.Initiate(context.Background(), testOrg, testUser, integrationID, allowedRedirect)
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

// postParams submits the param-collection form (Slice 3): form-urlencoded,
// exactly as a browser's <form method="post"> submits it — connectweb's
// SubmitParams reads r.ParseForm(), which only parses the body when
// Content-Type is set.
func (f *testFixture) postParams(token string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/connect/"+token+"/params", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

// inputBlockForField returns the HTML of the single <input> element for the
// given field name, so a test can assert on that field's own attributes
// (type, value) without being confused by another field's input elsewhere in
// the form.
func inputBlockForField(t *testing.T, body, fieldName string) string {
	t.Helper()
	for _, block := range strings.Split(body, "<input") {
		if strings.Contains(block, `name="`+fieldName+`"`) {
			return block
		}
	}
	t.Fatalf("no <input> found for field %q in body: %s", fieldName, body)
	return ""
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

// --- Param-collection form (Slice 3, AC3, AC4, AC5, AC6) ---

func TestConnectPage_ShowsTheParamCollectionFormWhenTheIntegrationDeclaresExpectedParams(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})
	initiated := f.initiateForIntegration(t, testParamsIntegration)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")

	w := f.getConnectPage(token)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `action="/connect/`+token+`/params"`) {
		t.Errorf("body does not carry the param-collection form's action attribute: %s", body)
	}
	if !strings.Contains(body, "Region") || !strings.Contains(body, "API Key") {
		t.Errorf("body does not carry both expected param labels: %s", body)
	}
	if strings.Contains(body, "params-provider.example.com") {
		t.Errorf("body must never forward to the provider before params are collected: %s", body)
	}
}

// TestConnectPage_SecretFlaggedParamRendersAsPasswordInput is AC5.
func TestConnectPage_SecretFlaggedParamRendersAsPasswordInput(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})
	initiated := f.initiateForIntegration(t, testParamsIntegration)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")

	w := f.getConnectPage(token)

	body := w.Body.String()
	apiKeyInput := inputBlockForField(t, body, "apiKey")
	if !strings.Contains(apiKeyInput, `type="password"`) {
		t.Errorf("secret field apiKey's input = %q, want type=\"password\"", apiKeyInput)
	}
	regionInput := inputBlockForField(t, body, "region")
	if !strings.Contains(regionInput, `type="text"`) {
		t.Errorf("non-secret field region's input = %q, want type=\"text\"", regionInput)
	}
}

// TestConnectPage_ProviderWithNoExpectedParamsNeverShowsTheParamCollectionForm
// is AC6, asserted explicitly: an integration whose provider declares no
// expected params must forward straight to the provider's connect page — the
// param-collection form path is never taken.
func TestConnectPage_ProviderWithNoExpectedParamsNeverShowsTheParamCollectionForm(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})
	initiated := f.initiate(t) // testIntegration -> Outlook fixture, no ExpectedParams
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")

	w := f.getConnectPage(token)

	body := w.Body.String()
	if strings.Contains(body, `/params"`) {
		t.Errorf("body carries a param-collection form action for a provider with no expected params: %s", body)
	}
	if extractHref(t, body) == "" {
		t.Fatal("no Connect action href found — a provider with no expected params must forward straight to the provider")
	}
}

// TestSubmitParams_InvalidTokenShowsErrorPageAndNeverForwards is AC2 on the
// POST path: SubmitParams shares connections.Facade's own validateConnectToken
// with ConnectPage (oauth.go:120-123) rather than re-implementing the check —
// this pins that sharing at the HTTP layer, so a future refactor that
// accidentally drops the call on the POST path fails loudly here.
func TestSubmitParams_InvalidTokenShowsErrorPageAndNeverForwards(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})

	w := f.postParams("token-that-does-not-exist", url.Values{"region": {"eu"}, "apiKey": {"k"}})

	if w.Code == http.StatusOK {
		t.Fatalf("status = %d, want a non-200 error status; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "params-provider.example.com") {
		t.Errorf("error page body %s must never forward to the provider", body)
	}
	if strings.Contains(body, `action="/connect/`) {
		t.Errorf("error page body %s must not still show the param-collection form", body)
	}
}

// TestSubmitParams_MissingRequiredFieldReRendersFormWithInlineErrorAndNeverForwards
// is AC4.
func TestSubmitParams_MissingRequiredFieldReRendersFormWithInlineErrorAndNeverForwards(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})
	initiated := f.initiateForIntegration(t, testParamsIntegration)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")

	w := f.postParams(token, url.Values{"region": {"eu"}})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (a validation failure re-renders the form, not an HTTP error); body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "This field is required.") {
		t.Errorf("body does not carry an inline required-field error: %s", body)
	}
	if !strings.Contains(body, `action="/connect/`+token+`/params"`) {
		t.Errorf("body does not still show the param-collection form: %s", body)
	}
	if strings.Contains(body, "params-provider.example.com") {
		t.Errorf("body must never forward to the provider after a failed submission: %s", body)
	}
}

// TestSubmitParams_ReRenderEchoesTheNonSecretValueButNeverTheSecretValue is
// AC5's other half: even when the secret field itself carried a value on the
// failed submission, that value must never be echoed back.
func TestSubmitParams_ReRenderEchoesTheNonSecretValueButNeverTheSecretValue(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})
	initiated := f.initiateForIntegration(t, testParamsIntegration)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")
	const secretValue = "super-secret-api-key-value"

	// region is left blank so the submission still fails validation, even
	// though apiKey itself carries a (secret) value on this same submission —
	// proving the secret is never echoed back regardless of why the form
	// re-renders.
	w := f.postParams(token, url.Values{"region": {""}, "apiKey": {secretValue}})

	body := w.Body.String()
	if strings.Contains(body, secretValue) {
		t.Fatalf("body contains the submitted secret value %q — it must never be echoed back", secretValue)
	}
	apiKeyInput := inputBlockForField(t, body, "apiKey")
	if !strings.Contains(apiKeyInput, `value=""`) {
		t.Errorf("apiKey input = %q, want its value left blank on re-render", apiKeyInput)
	}
}

// TestSubmitParams_ValidSubmissionStoresValuesAndRendersTheProvidersConnectPage
// is AC3/AC7's forwarding half: a submission with every required field
// present stores the values and renders the provider's own connect page next.
func TestSubmitParams_ValidSubmissionStoresValuesAndRendersTheProvidersConnectPage(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})
	initiated := f.initiateForIntegration(t, testParamsIntegration)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")
	const secretValue = "super-secret-api-key-value"

	w := f.postParams(token, url.Values{"region": {"eu"}, "apiKey": {secretValue}})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, testParamsProviderName) {
		t.Errorf("body does not carry the provider name after a successful submission: %s", body)
	}
	if extractHref(t, body) == "" {
		t.Fatal("no Connect action href found after a successful submission")
	}
	if strings.Contains(body, secretValue) {
		t.Fatalf("body contains the raw secret value after a successful submission: %s", body)
	}
}
