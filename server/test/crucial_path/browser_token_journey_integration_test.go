//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, connectionDTO,
// wireErrorEnvelope, and doJSONRequest already declared there). This file
// tells Slice 5's story end to end against the real composition root: an
// installation admin issues a user-token signing secret (shown exactly once;
// listing shows only id/prefix/createdAt, and the raw secret is never stored
// in plaintext), a consumer's server mints a user-scoped HS256 JWT locally —
// right here, with the exact compact-JWT construction the SDK will use in
// Slice 9, rather than any library — and that token drives the browser
// surface (list integrations, initiate with the userId forced from the
// token, list/get/reconnect only its own user's connections), while an
// expired, tampered, wrong-secret, wrong-algorithm token and every
// server-only endpoint (user creation, logs, tool execution) are all
// rejected as unauthorized.
package crucial_path

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"beecon/internal/app"
	"beecon/test/support"
)

type issuedSigningSecretDTO struct {
	ID        string `json:"id"`
	Secret    string `json:"secret"`
	Prefix    string `json:"prefix"`
	CreatedAt string `json:"createdAt"`
}

type signingSecretDTO struct {
	ID        string `json:"id"`
	Prefix    string `json:"prefix"`
	CreatedAt string `json:"createdAt"`
}

func b64urlNoPad(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

// mintBrowserUserToken builds a compact HS256 JWT — {"alg":"HS256","kid":kid}
// . {"sub":userID,"iat":iat,"exp":exp}, HMAC-SHA256 signed over
// "header.payload" under the raw secret — exactly the construction PD20
// commits the SDK to minting locally in Slice 9, and exactly what
// access.Facade.VerifyUserToken parses. alg is exposed as a parameter so the
// tamper/wrong-algorithm rejection tests can build otherwise-valid tokens
// that name a different (or "none") algorithm.
func mintBrowserUserToken(t *testing.T, secret, kid, alg, userID string, iat, exp int64) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": alg, "kid": kid, "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payload, err := json.Marshal(map[string]any{"sub": userID, "iat": iat, "exp": exp})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	signingInput := b64urlNoPad(header) + "." + b64urlNoPad(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + b64urlNoPad(mac.Sum(nil))
}

// browserTokenFixture is the org/integration/signing-secret/user scaffolding
// every sub-test in this file needs before it can mint its own user token.
type browserTokenFixture struct {
	orgID              string
	orgAuth            string
	integrationID      string
	allowedRedirectURI string
	signingSecret      string
	signingSecretKid   string
}

func newBrowserTokenFixture(t *testing.T, wired *app.Wired) browserTokenFixture {
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
		t.Fatalf("issue org key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &orgKey); err != nil {
		t.Fatalf("decode org key: %v", err)
	}
	orgAuth := "Bearer " + orgKey.Key

	var signingSecret issuedSigningSecretDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/signing-secrets", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue signing secret status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &signingSecret); err != nil {
		t.Fatalf("decode signing secret: %v", err)
	}

	return browserTokenFixture{
		orgID:              org.ID,
		orgAuth:            orgAuth,
		integrationID:      integration.ID,
		allowedRedirectURI: allowedRedirectURI,
		signingSecret:      signingSecret.Secret,
		signingSecretKid:   signingSecret.ID,
	}
}

// createUser creates a user under the fixture's organization through the org
// API key (user creation is server-key-only — AC9 of this very slice) and
// returns its id.
func (f browserTokenFixture) createUser(t *testing.T, wired *app.Wired, name string) string {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", f.orgAuth, `{"name":"`+name+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var user userDTO
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode user: %v", err)
	}
	return user.ID
}

// mintValidTokenFor mints a fresh, unexpired (2h, PD20's default) token for
// userID under the fixture's own signing secret.
func (f browserTokenFixture) mintValidTokenFor(t *testing.T, userID string) string {
	t.Helper()
	now := time.Now()
	return mintBrowserUserToken(t, f.signingSecret, f.signingSecretKid, "HS256", userID, now.Unix(), now.Add(2*time.Hour).Unix())
}

// TestBrowserTokenJourney_IssueSigningSecretShownOnceAndListedSafely is AC1.
func TestBrowserTokenJourney_IssueSigningSecretShownOnceAndListedSafely(t *testing.T) {
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

	var issued issuedSigningSecretDTO
	t.Run("issuing a signing secret returns a usk_-prefixed id and the full secret", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/signing-secrets", adminAuth, "")
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &issued); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !strings.HasPrefix(issued.ID, "usk_") {
			t.Errorf("id = %q, want it to start with %q", issued.ID, "usk_")
		}
		if issued.Secret == "" {
			t.Error("secret is empty, want the freshly minted secret")
		}
	})

	t.Run("listing signing secrets shows only id, prefix, and createdAt — never the secret", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+org.ID+"/signing-secrets", adminAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		if strings.Contains(w.Body.String(), issued.Secret) {
			t.Fatalf("list response %s contains the raw issued secret %q", w.Body.String(), issued.Secret)
		}
		var list []signingSecretDTO
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode list: %v; body=%s", err, w.Body.String())
		}
		if len(list) != 1 || list[0].ID != issued.ID {
			t.Fatalf("list = %+v, want exactly the one issued secret %q", list, issued.ID)
		}
		if list[0].Prefix == "" {
			t.Error("prefix is empty, want the cosmetic display prefix")
		}
		if list[0].CreatedAt == "" {
			t.Error("createdAt is empty")
		}
	})

	t.Run("the persisted signing secret row never stores the raw secret in plaintext", func(t *testing.T) {
		rows, err := wired.DB.QueryContext(context.Background(), "SELECT encrypted_secret FROM signing_secrets WHERE id = ?", issued.ID)
		if err != nil {
			t.Fatalf("query signing_secrets: %v", err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatal("expected a signing_secrets row for the issued secret")
		}
		var encrypted string
		if err := rows.Scan(&encrypted); err != nil {
			t.Fatalf("scan encrypted_secret: %v", err)
		}
		if encrypted == issued.Secret {
			t.Fatal("encrypted_secret column equals the raw secret verbatim — it must be vault ciphertext")
		}
	})
}

// TestBrowserTokenJourney_ValidTokenDrivesTheBrowserSurface is AC2 (minted
// in-test with the SDK's own HMAC construction), AC3, AC4, AC5, and AC6.
func TestBrowserTokenJourney_ValidTokenDrivesTheBrowserSurface(t *testing.T) {
	wired := support.BootApp(t)
	fixture := newBrowserTokenFixture(t, wired)
	adaID := fixture.createUser(t, wired, "Ada Lovelace")
	bobID := fixture.createUser(t, wired, "Bob Builder")
	adaToken := fixture.mintValidTokenFor(t, adaID)
	adaAuth := "Bearer " + adaToken

	t.Run("a valid user token can list the integrations available to its organization", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/integrations/", adaAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var list []integrationSummaryDTO
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode list: %v; body=%s", err, w.Body.String())
		}
		found := false
		for _, i := range list {
			if i.ID == fixture.integrationID {
				found = true
			}
		}
		if !found {
			t.Errorf("list %+v does not include the fixture's integration %q", list, fixture.integrationID)
		}
	})

	var adaConnection initiatedConnectionDTO
	t.Run("a valid user token can initiate a connection — userId comes from the token, a body userId is ignored", func(t *testing.T) {
		body := `{"userId":"` + bobID + `","integrationId":"` + fixture.integrationID + `","redirectUri":"` + fixture.allowedRedirectURI + `"}`
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/initiate", adaAuth, body)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &adaConnection); err != nil {
			t.Fatalf("decode initiated connection: %v", err)
		}

		// Confirm ownership landed on the token's own user, not the bogus
		// body userId, by reading the raw connection back through the
		// organization's own API key (unaffected by any token-user filter).
		got := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/connections/"+adaConnection.ID, fixture.orgAuth, "")
		if got.Code != http.StatusOK {
			t.Fatalf("get connection status = %d, want %d; body=%s", got.Code, http.StatusOK, got.Body.String())
		}
		var conn connectionDTO
		if err := json.Unmarshal(got.Body.Bytes(), &conn); err != nil {
			t.Fatalf("decode connection: %v", err)
		}
		if conn.UserID != adaID {
			t.Errorf("connection userId = %q, want the token's own user %q (the body's userId %q must be ignored)", conn.UserID, adaID, bobID)
		}
	})

	var bobConnection initiatedConnectionDTO
	t.Run("org-key initiates a second connection for Bob, used below to prove cross-user isolation", func(t *testing.T) {
		body := `{"userId":"` + bobID + `","integrationId":"` + fixture.integrationID + `","redirectUri":"` + fixture.allowedRedirectURI + `"}`
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/initiate", fixture.orgAuth, body)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &bobConnection); err != nil {
			t.Fatalf("decode initiated connection: %v", err)
		}
	})

	t.Run("a valid user token can list its own connections and none of another user's", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/connections/", adaAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var page connectionsPageDTO
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode connections page: %v; body=%s", err, w.Body.String())
		}
		if len(page.Items) != 1 || page.Items[0].ID != adaConnection.ID {
			t.Fatalf("Items = %+v, want exactly Ada's own connection %q", page.Items, adaConnection.ID)
		}
	})

	t.Run("a valid user token can get its own connection", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/connections/"+adaConnection.ID, adaAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("getting another user's connection with a valid user token is not-found", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/connections/"+bobConnection.ID, adaAuth, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if env.Error.Code != "not_found" {
			t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
		}
	})

	t.Run("a valid user token can start a reconnect on its own connection", func(t *testing.T) {
		// Reconnect is only allowed from ACTIVE/EXPIRED/DISCONNECTED
		// (Slice 4) — Ada's connection is still INITIATED (its OAuth
		// handshake was never completed), so bring it to DISCONNECTED via
		// the org API key first; the reconnect itself below is still driven
		// entirely by Ada's own user token.
		disable := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+adaConnection.ID+"/disable", fixture.orgAuth, "")
		if disable.Code != http.StatusOK {
			t.Fatalf("disable status = %d, want %d; body=%s", disable.Code, http.StatusOK, disable.Body.String())
		}

		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+adaConnection.ID+"/reconnect", adaAuth,
			`{"redirectUri":"`+fixture.allowedRedirectURI+`"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		var reconnected initiatedConnectionDTO
		if err := json.Unmarshal(w.Body.Bytes(), &reconnected); err != nil {
			t.Fatalf("decode reconnect response: %v", err)
		}
		if reconnected.ID != adaConnection.ID {
			t.Errorf("id = %q, want the same stable id %q", reconnected.ID, adaConnection.ID)
		}
	})

	t.Run("starting a reconnect on another user's connection with a valid user token is not-found", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+bobConnection.ID+"/reconnect", adaAuth,
			`{"redirectUri":"`+fixture.allowedRedirectURI+`"}`)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
		}
	})
}

// TestBrowserTokenJourney_RejectionMatrix is AC7, AC8, and AC9: an expired
// token, a tampered payload, a wrong-secret signature, alg:none, an
// unsupported algorithm, and every server-only endpoint (user creation,
// logs, tool execution — file upload completes this matrix in Slice 7) are
// all rejected as unauthorized.
func TestBrowserTokenJourney_RejectionMatrix(t *testing.T) {
	wired := support.BootApp(t)
	fixture := newBrowserTokenFixture(t, wired)
	adaID := fixture.createUser(t, wired, "Ada Lovelace")

	assertUnauthorized := func(t *testing.T, token string) {
		t.Helper()
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/integrations/", "Bearer "+token, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if env.Error.Code != "unauthorized" {
			t.Errorf("error.code = %q, want %q", env.Error.Code, "unauthorized")
		}
	}

	t.Run("an expired token is rejected as unauthorized", func(t *testing.T) {
		now := time.Now()
		expired := mintBrowserUserToken(t, fixture.signingSecret, fixture.signingSecretKid, "HS256", adaID,
			now.Add(-3*time.Hour).Unix(), now.Add(-1*time.Hour).Unix())
		assertUnauthorized(t, expired)
	})

	t.Run("a token with a tampered payload is rejected as unauthorized", func(t *testing.T) {
		valid := fixture.mintValidTokenFor(t, adaID)
		segments := strings.Split(valid, ".")
		if len(segments) != 3 {
			t.Fatalf("minted token %q is not a three-segment compact JWT", valid)
		}
		forgedPayload, err := json.Marshal(map[string]any{"sub": "user_someone_else", "iat": time.Now().Unix(), "exp": time.Now().Add(2 * time.Hour).Unix()})
		if err != nil {
			t.Fatalf("marshal forged payload: %v", err)
		}
		tampered := segments[0] + "." + b64urlNoPad(forgedPayload) + "." + segments[2]
		assertUnauthorized(t, tampered)
	})

	t.Run("a token signed with the wrong secret is rejected as unauthorized", func(t *testing.T) {
		now := time.Now()
		wrongSecretToken := mintBrowserUserToken(t, "totally-different-secret", fixture.signingSecretKid, "HS256", adaID,
			now.Unix(), now.Add(2*time.Hour).Unix())
		assertUnauthorized(t, wrongSecretToken)
	})

	t.Run("a token asserting alg:none is rejected as unauthorized", func(t *testing.T) {
		now := time.Now()
		algNoneToken := mintBrowserUserToken(t, fixture.signingSecret, fixture.signingSecretKid, "none", adaID,
			now.Unix(), now.Add(2*time.Hour).Unix())
		assertUnauthorized(t, algNoneToken)
	})

	t.Run("a token asserting an unsupported non-none algorithm is rejected as unauthorized", func(t *testing.T) {
		now := time.Now()
		hs512Token := mintBrowserUserToken(t, fixture.signingSecret, fixture.signingSecretKid, "HS512", adaID,
			now.Unix(), now.Add(2*time.Hour).Unix())
		assertUnauthorized(t, hs512Token)
	})

	validAuth := "Bearer " + fixture.mintValidTokenFor(t, adaID)

	t.Run("a valid user token cannot create a user", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", validAuth, `{"name":"Someone Else"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("a valid user token cannot list logs", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/logs/", validAuth, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("a valid user token cannot execute a tool", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/tools/outlook-list-messages/execute", validAuth, `{"arguments":{}}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})
}
