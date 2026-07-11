//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, wireErrorEnvelope, and
// doJSONRequest already declared there). This file tells Slice 4's story end
// to end against the real composition root and a FakeMicrosoft httptest
// server standing in for Microsoft/Graph: the connect page renders and
// carries a single-use CSRF state, the callback exchanges the code for
// tokens and activates the connection under its original id, account
// metadata becomes visible via get-connection, tokens are encrypted at rest
// in the real SQLite database, and every documented failure mode (bad state,
// consent denial, token-exchange/user-info failure) leaves the connection
// exactly where PD11 says it must.
package crucial_path

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/app"
	"beecon/internal/catalog"
	connectionsbun "beecon/internal/connections/driven/bun"
	"beecon/test/support"
)

// connectionWithAccountDTO is Get's response shape once a connection has been
// activated: the same fields connections_journey_integration_test.go's
// connectionDTO carries, plus the account metadata AC6 makes visible.
type connectionWithAccountDTO struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	ProviderSlug string `json:"providerSlug"`
	UserID       string `json:"userId"`
	Account      *struct {
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
	} `json:"account"`
}

var hrefPattern = regexp.MustCompile(`href="([^"]+)"`)

// extractConnectPageState parses a live GET /connect/{token} response for the
// state query param carried on the rendered Connect action's href (html/
// template HTML-escapes "&" to "&amp;" inside attribute values, so this
// unescapes before parsing — mirroring what a browser's HTML parser does).
func extractConnectPageState(t *testing.T, body string) string {
	t.Helper()
	match := hrefPattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("no href found in connect page body: %s", body)
	}
	authorizeURL := html.UnescapeString(match[1])
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorize URL %q: %v", authorizeURL, err)
	}
	return parsed.Query().Get("state")
}

func outlookDefinitionAgainst(fakeMS *support.FakeMicrosoft) []catalog.ProviderDefinition {
	return []catalog.ProviderDefinition{
		{
			Slug:         "outlook",
			Name:         "Outlook",
			Logo:         "https://static.beecon.dev/providers/outlook.png",
			AuthScheme:   "oauth2",
			AuthorizeURL: "https://fake-microsoft.example.com/oauth2/v2.0/authorize",
			TokenURL:     fakeMS.TokenURL,
			UserInfoURL:  fakeMS.UserInfoURL,
			Scopes:       []string{"offline_access", "Mail.Read", "User.Read"},
		},
	}
}

// oauthJourneyFixture is the org/integration/user/redirect-uri scaffolding
// every sub-test in this file needs before it can initiate its own
// connection.
type oauthJourneyFixture struct {
	orgAuth            string
	userID             string
	integrationID      string
	allowedRedirectURI string
}

func newOAuthJourneyFixture(t *testing.T, wired *app.Wired) oauthJourneyFixture {
	t.Helper()
	adminAuth := "Bearer " + support.AdminAPIKey
	const allowedRedirectURI = "https://consumer.example.com/callback"

	var org organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Acme"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}

	var integration integrationSummaryDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"outlook","clientId":"the-client-id","clientSecret":"the-client-secret"}`)
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

func (f oauthJourneyFixture) initiate(t *testing.T, wired *app.Wired) initiatedConnectionDTO {
	t.Helper()
	body := `{"userId":"` + f.userID + `","integrationId":"` + f.integrationID + `","redirectUri":"` + f.allowedRedirectURI + `"}`
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/initiate", f.orgAuth, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("initiate status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var initiated initiatedConnectionDTO
	if err := json.Unmarshal(w.Body.Bytes(), &initiated); err != nil {
		t.Fatalf("decode initiated connection: %v", err)
	}
	return initiated
}

// openConnectPageAndGetState performs the connect-page GET the initiated
// connection's own redirectUrl names, and extracts the single-use CSRF state
// the rendered Connect action carries (AC1, AC3).
func openConnectPageAndGetState(t *testing.T, wired *app.Wired, initiated initiatedConnectionDTO) string {
	t.Helper()
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/"+token, "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("connect page status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	return extractConnectPageState(t, w.Body.String())
}

func (f oauthJourneyFixture) getConnection(t *testing.T, wired *app.Wired, id string) connectionWithAccountDTO {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/connections/"+id, f.orgAuth, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get connection status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var got connectionWithAccountDTO
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode connection: %v; body=%s", err, w.Body.String())
	}
	return got
}

// connectionRowFromDB reads the raw connections table row directly, so tests
// can assert on exactly what landed in the SQLite database file — not just
// what the facade/API chooses to expose.
func connectionRowFromDB(t *testing.T, db *upstreambun.DB, id string) *connectionsbun.ConnectionRow {
	t.Helper()
	row := new(connectionsbun.ConnectionRow)
	err := db.NewSelect().Model(row).Where("id = ?", id).Scan(context.Background())
	if err != nil {
		t.Fatalf("query connections row for %q: %v", id, err)
	}
	return row
}

func TestOAuthHandshakeJourney_HappyPath(t *testing.T) {
	const rawAccessToken = "raw-microsoft-access-token-value"
	const rawRefreshToken = "raw-microsoft-refresh-token-value"
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken:        rawAccessToken,
		RefreshToken:       rawRefreshToken,
		AccountEmail:       "ada@example.com",
		AccountDisplayName: "Ada Lovelace",
	})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionAgainst(fakeMS))
	fixture := newOAuthJourneyFixture(t, wired)

	initiated := fixture.initiate(t, wired)

	var state string
	t.Run("opening the connect page renders the provider and a single-use CSRF state", func(t *testing.T) {
		state = openConnectPageAndGetState(t, wired, initiated)
		if state == "" {
			t.Fatal("connect page carried no CSRF state")
		}
	})

	var callbackLocation string
	t.Run("the callback exchanges the code, activates the connection, and redirects to the consumer with the stable id and success status", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")
		if w.Code != http.StatusFound {
			t.Fatalf("callback status = %d, want %d; body=%s", w.Code, http.StatusFound, w.Body.String())
		}
		callbackLocation = w.Header().Get("Location")
		if strings.Contains(callbackLocation, rawAccessToken) || strings.Contains(callbackLocation, rawRefreshToken) {
			t.Fatalf("callback redirect Location %q must never contain a raw token", callbackLocation)
		}
		parsed, err := url.Parse(callbackLocation)
		if err != nil {
			t.Fatalf("parse redirect location: %v", err)
		}
		if got := parsed.Query().Get("connectionId"); got != initiated.ID {
			t.Errorf("connectionId = %q, want the stable id %q from initiate", got, initiated.ID)
		}
		if got := parsed.Query().Get("status"); got != "success" {
			t.Errorf("status = %q, want %q", got, "success")
		}
		if base := parsed.Scheme + "://" + parsed.Host + parsed.Path; base != fixture.allowedRedirectURI {
			t.Errorf("redirect base = %q, want the consumer's own redirectUri %q", base, fixture.allowedRedirectURI)
		}
	})

	t.Run("get-connection shows ACTIVE status and the account email/display name, never tokens", func(t *testing.T) {
		got := fixture.getConnection(t, wired, initiated.ID)
		if got.ID != initiated.ID {
			t.Errorf("id = %q, want the stable id %q", got.ID, initiated.ID)
		}
		if got.Status != "ACTIVE" {
			t.Errorf("status = %q, want %q", got.Status, "ACTIVE")
		}
		if got.Account == nil {
			t.Fatal("account is nil, want the captured profile")
		}
		if got.Account.Email != "ada@example.com" {
			t.Errorf("account.email = %q, want %q", got.Account.Email, "ada@example.com")
		}
		if got.Account.DisplayName != "Ada Lovelace" {
			t.Errorf("account.displayName = %q, want %q", got.Account.DisplayName, "Ada Lovelace")
		}
	})

	t.Run("the raw tokens never appear anywhere in any API response", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/connections/"+initiated.ID, fixture.orgAuth, "")
		if strings.Contains(w.Body.String(), rawAccessToken) || strings.Contains(w.Body.String(), rawRefreshToken) {
			t.Fatalf("get-connection response %s contains a raw token", w.Body.String())
		}
	})

	t.Run("tokens are stored encrypted at rest in the real SQLite database — never the raw values", func(t *testing.T) {
		row := connectionRowFromDB(t, wired.DB, initiated.ID)
		if row.EncryptedAccessToken == "" || row.EncryptedRefreshToken == "" {
			t.Fatal("encrypted token columns must not be empty for an ACTIVE connection")
		}
		if row.EncryptedAccessToken == rawAccessToken || strings.Contains(row.EncryptedAccessToken, rawAccessToken) {
			t.Errorf("encrypted_access_token %q contains the raw access token — it must be ciphertext", row.EncryptedAccessToken)
		}
		if row.EncryptedRefreshToken == rawRefreshToken || strings.Contains(row.EncryptedRefreshToken, rawRefreshToken) {
			t.Errorf("encrypted_refresh_token %q contains the raw refresh token — it must be ciphertext", row.EncryptedRefreshToken)
		}
	})

	t.Run("a second callback with the already-used state shows an error page and does not disturb the ACTIVE connection", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")
		if w.Code == http.StatusFound {
			t.Fatalf("second callback with an already-used state status = %d, want an error status, not a redirect", w.Code)
		}
		got := fixture.getConnection(t, wired, initiated.ID)
		if got.Status != "ACTIVE" {
			t.Errorf("status after replaying a consumed state = %q, want it to remain %q", got.Status, "ACTIVE")
		}
	})
}

func TestOAuthHandshakeJourney_InvalidExpiredAndAlreadyCompletedConnectLinks(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionAgainst(fakeMS))

	t.Run("an invalid connect token shows an error page and never forwards to the provider", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/does-not-exist", "", "")
		if w.Code == http.StatusOK {
			t.Fatalf("status = %d, want a non-200 error status; body=%s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "fake-microsoft.example.com") {
			t.Errorf("error page body must never contain a provider URL: %s", w.Body.String())
		}
	})

	t.Run("an already-completed connect link shows an error page and never forwards to the provider", func(t *testing.T) {
		fixture := newOAuthJourneyFixture(t, wired)
		initiated := fixture.initiate(t, wired)
		state := openConnectPageAndGetState(t, wired, initiated)
		callback := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")
		if callback.Code != http.StatusFound {
			t.Fatalf("callback status = %d, want %d; body=%s", callback.Code, http.StatusFound, callback.Body.String())
		}
		token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")

		w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/"+token, "", "")

		if w.Code == http.StatusOK {
			t.Fatalf("status = %d, want a non-200 error status for an already-completed connection; body=%s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "fake-microsoft.example.com") {
			t.Errorf("error page body must never contain a provider URL: %s", w.Body.String())
		}
	})
}

func TestOAuthHandshakeJourney_CallbackStateFailureModes(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionAgainst(fakeMS))
	fixture := newOAuthJourneyFixture(t, wired)

	t.Run("a callback with no state param at all shows an error page, connection stays INITIATED", func(t *testing.T) {
		initiated := fixture.initiate(t, wired)
		openConnectPageAndGetState(t, wired, initiated)

		w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code", "", "")

		if w.Code == http.StatusFound {
			t.Fatalf("status = %d, want an error status, not a redirect", w.Code)
		}
		got := fixture.getConnection(t, wired, initiated.ID)
		if got.Status != "INITIATED" {
			t.Errorf("status = %q, want %q", got.Status, "INITIATED")
		}
	})

	t.Run("a callback with a state nobody minted shows an error page, connection stays INITIATED", func(t *testing.T) {
		initiated := fixture.initiate(t, wired)
		openConnectPageAndGetState(t, wired, initiated)

		w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state=nobody-minted-this-state", "", "")

		if w.Code == http.StatusFound {
			t.Fatalf("status = %d, want an error status, not a redirect", w.Code)
		}
		got := fixture.getConnection(t, wired, initiated.ID)
		if got.Status != "INITIATED" {
			t.Errorf("status = %q, want %q", got.Status, "INITIATED")
		}
	})

	t.Run("when the user denies consent, the browser returns to the consumer's redirectUri with an error status and the connection stays INITIATED", func(t *testing.T) {
		initiated := fixture.initiate(t, wired)
		state := openConnectPageAndGetState(t, wired, initiated)

		w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?state="+state+"&error=access_denied", "", "")

		if w.Code != http.StatusFound {
			t.Fatalf("status = %d, want %d (redirect back to consumer even on denial); body=%s", w.Code, http.StatusFound, w.Body.String())
		}
		location := w.Header().Get("Location")
		parsed, err := url.Parse(location)
		if err != nil {
			t.Fatalf("parse redirect location: %v", err)
		}
		if got := parsed.Query().Get("status"); got != "error" {
			t.Errorf("status = %q, want %q", got, "error")
		}
		if got := parsed.Query().Get("connectionId"); got != initiated.ID {
			t.Errorf("connectionId = %q, want %q", got, initiated.ID)
		}
		got := fixture.getConnection(t, wired, initiated.ID)
		if got.Status != "INITIATED" {
			t.Errorf("status after consent denial = %q, want it to remain %q", got.Status, "INITIATED")
		}
	})
}

func TestOAuthHandshakeJourney_TokenExchangeFailureShowsErrorAndConnectionStaysInitiated(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{FailTokenExchange: true})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionAgainst(fakeMS))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := fixture.initiate(t, wired)
	state := openConnectPageAndGetState(t, wired, initiated)

	w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")

	if w.Code == http.StatusFound {
		t.Fatalf("status = %d, want an error status, not a redirect; body=%s", w.Code, w.Body.String())
	}
	got := fixture.getConnection(t, wired, initiated.ID)
	if got.Status != "INITIATED" {
		t.Errorf("status = %q, want %q", got.Status, "INITIATED")
	}
}

func TestOAuthHandshakeJourney_UserInfoFailureShowsErrorAndConnectionStaysInitiated(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: "at", RefreshToken: "rt", FailUserInfo: true})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionAgainst(fakeMS))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := fixture.initiate(t, wired)
	state := openConnectPageAndGetState(t, wired, initiated)

	w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")

	if w.Code == http.StatusFound {
		t.Fatalf("status = %d, want an error status, not a redirect; body=%s", w.Code, w.Body.String())
	}
	got := fixture.getConnection(t, wired, initiated.ID)
	if got.Status != "INITIATED" {
		t.Errorf("status = %q, want %q", got.Status, "INITIATED")
	}
}
