//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, oauthJourneyFixture,
// doJSONRequest, executeTool, executionResultDTO, fetchLogs, logsPageDTO,
// hrefPattern, and connectionRowFromDB — same package). This file tells
// Slice 3's story end to end against the real composition root and
// support.FakeParamProvider: the consumer fetches an integration's expected
// pre-auth params, the connect page shows the param-collection form before
// forwarding, a missing required field is an inline validation error that
// never reaches the provider, the secret field is masked and never echoed,
// a valid submission stores both values vault-encrypted, and the collected
// values are usable via {params.x} templating in both the provider's OAuth
// URLs and a tool's path/header mapping — the fake provider's own view of
// what it received is the proof, not just that Beecon parsed the tokens.
package crucial_path

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"beecon/internal/app"
	"beecon/internal/catalog"
	"beecon/internal/config"
	"beecon/internal/vault"
	"beecon/test/support"
)

type expectedParamFieldDTO struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"`
}

type expectedParamsDTO struct {
	ProviderName string                  `json:"providerName"`
	Fields       []expectedParamFieldDTO `json:"fields"`
}

// doFormRequest submits a form-urlencoded POST — connectweb's SubmitParams
// reads r.ParseForm(), which only parses the body when Content-Type is set,
// exactly as a browser's <form method="post"> submission would.
func doFormRequest(t *testing.T, handler http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// inputBlockForField returns the HTML of the single <input> element for the
// given field name, so a test can assert on that field's own attributes
// without being confused by another field's input elsewhere in the form.
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

// extractConnectActionHref parses a live GET /connect/{token} response for
// the href of its rendered Connect action (html/template HTML-escapes "&" to
// "&amp;" inside attribute values, so this unescapes before returning — the
// same thing a browser's HTML parser does transparently).
func extractConnectActionHref(t *testing.T, body string) string {
	t.Helper()
	match := hrefPattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("no href found in body: %s", body)
	}
	return html.UnescapeString(match[1])
}

// newParamCollectionJourneyFixture is newHubspotJourneyFixture
// (hubspot_journey_integration_test.go) re-pointed at the
// "fake-param-provider" providerSlug.
func newParamCollectionJourneyFixture(t *testing.T, wired *app.Wired) oauthJourneyFixture {
	t.Helper()
	adminAuth := "Bearer " + support.AdminAPIKey
	const allowedRedirectURI = "https://consumer.example.com/fake-param-provider-callback"

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
		`{"providerSlug":"fake-param-provider","clientId":"fake-client-id","clientSecret":"fake-client-secret"}`)
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

// TestParamCollectionJourney tells Slice 3's story end to end: expected
// params reported via the catalog API, the connect page's param-collection
// form (validation, masking, storage), and the collected values reaching the
// fake provider — in its OAuth URLs and in a tool's path/header mapping.
func TestParamCollectionJourney(t *testing.T) {
	fp := support.NewFakeParamProvider(t, support.FakeParamProviderScript{
		AccessToken:  "raw-fake-param-provider-access-token",
		RefreshToken: "raw-fake-param-provider-refresh-token",
		AccountEmail: "ada@example.com",
		AccountName:  "Ada Lovelace",
		WidgetName:   "Blue Widget",
	})
	wired := support.BootAppWithProviderDefinitions(t, []catalog.ProviderDefinition{support.FakeParamProviderDefinition(fp)})
	fixture := newParamCollectionJourneyFixture(t, wired)

	var expectedParams expectedParamsDTO
	t.Run("consumer fetches the integration's expected params: provider name plus region/apiKey field shapes", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/integrations/"+fixture.integrationID+"/expected-params", fixture.orgAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &expectedParams); err != nil {
			t.Fatalf("decode expected params: %v; body=%s", err, w.Body.String())
		}
		if expectedParams.ProviderName != "Fake Param Provider" {
			t.Errorf("providerName = %q, want %q", expectedParams.ProviderName, "Fake Param Provider")
		}
		if len(expectedParams.Fields) != 2 {
			t.Fatalf("len(fields) = %d, want 2; fields=%+v", len(expectedParams.Fields), expectedParams.Fields)
		}
		byName := map[string]expectedParamFieldDTO{}
		for _, field := range expectedParams.Fields {
			byName[field.Name] = field
		}
		if region, ok := byName["region"]; !ok || !region.Required || region.Secret {
			t.Errorf("region field = %+v (present=%v), want required=true secret=false", region, ok)
		}
		if apiKey, ok := byName["apiKey"]; !ok || !apiKey.Required || !apiKey.Secret {
			t.Errorf("apiKey field = %+v (present=%v), want required=true secret=true", apiKey, ok)
		}
	})

	initiated := fixture.initiate(t, wired)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")

	t.Run("opening the connect page shows the param-collection form, masking the secret field, never forwarding to the provider yet", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/"+token, "", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, `action="/connect/`+token+`/params"`) {
			t.Errorf("body does not carry the param-collection form's action: %s", body)
		}
		if strings.Contains(body, fp.BaseURL) {
			t.Errorf("body must never forward to the provider before params are collected: %s", body)
		}
		apiKeyInput := inputBlockForField(t, body, "apiKey")
		if !strings.Contains(apiKeyInput, `type="password"`) {
			t.Errorf("secret field apiKey's input = %q, want type=\"password\"", apiKeyInput)
		}
		regionInput := inputBlockForField(t, body, "region")
		if !strings.Contains(regionInput, `type="text"`) {
			t.Errorf("non-secret field region's input = %q, want type=\"text\"", regionInput)
		}
	})

	t.Run("submitting with the required apiKey missing shows an inline error and never calls the provider", func(t *testing.T) {
		w := doFormRequest(t, wired.Router, "/connect/"+token+"/params", url.Values{"region": {"eu"}})
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (a validation failure re-renders the form); body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, "This field is required.") {
			t.Errorf("body does not carry an inline required-field error: %s", body)
		}
		if fp.LastTokenRegion != "" {
			t.Fatalf("provider received a token request for region %q — a failed submission must never forward", fp.LastTokenRegion)
		}
	})

	const region = "eu"
	const apiKey = "super-secret-fake-provider-api-key"
	var callbackState string
	t.Run("submitting both required fields stores them and renders the provider's own connect page", func(t *testing.T) {
		w := doFormRequest(t, wired.Router, "/connect/"+token+"/params", url.Values{"region": {region}, "apiKey": {apiKey}})
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		body := w.Body.String()
		if strings.Contains(body, apiKey) {
			t.Fatalf("body contains the raw secret apiKey value: %s", body)
		}
		href := extractConnectActionHref(t, body)
		if !strings.Contains(href, "/"+region+"/oauth/authorize") {
			t.Errorf("authorize href = %q, want the {params.region} token substituted with the collected value %q", href, region)
		}
		parsed, err := url.Parse(href)
		if err != nil {
			t.Fatalf("parse authorize href: %v", err)
		}
		callbackState = parsed.Query().Get("state")
		if callbackState == "" {
			t.Fatal("authorize href carries no state param")
		}
	})

	t.Run("the connection's stored params are encrypted at rest — never the raw secret or the non-secret value in plaintext", func(t *testing.T) {
		row := connectionRowFromDB(t, wired.DB, initiated.ID)
		if row.EncryptedParams == "" {
			t.Fatal("encrypted_params is empty, want the vault ciphertext of the submitted values")
		}
		if strings.Contains(row.EncryptedParams, apiKey) {
			t.Errorf("encrypted_params %q contains the raw secret apiKey value — it must be vault ciphertext", row.EncryptedParams)
		}

		// Decrypt with the same vault the app booted with and compare the exact
		// stored values, rather than pattern-matching the ciphertext for the
		// short "eu" region value: random-nonce AES-GCM ciphertext, base64
		// encoded, can coincidentally contain a short substring like "eu" —
		// a decrypt-and-compare is the only assertion that can't false-fail.
		key, err := config.DecodeEncryptionKey(support.EncryptionKeyBase64)
		if err != nil {
			t.Fatalf("DecodeEncryptionKey: %v", err)
		}
		tokenVault, err := vault.NewVault(key)
		if err != nil {
			t.Fatalf("NewVault: %v", err)
		}
		plaintext, err := tokenVault.Decrypt(row.EncryptedParams)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		var stored map[string]string
		if err := json.Unmarshal([]byte(plaintext), &stored); err != nil {
			t.Fatalf("unmarshal decrypted params: %v", err)
		}
		if stored["region"] != region || stored["apiKey"] != apiKey {
			t.Errorf("decrypted stored params = %v, want {region: %q, apiKey: %q}", stored, region, apiKey)
		}
	})

	t.Run("completing the callback activates the connection, having reached the provider's region-templated token endpoint", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+callbackState, "", "")
		if w.Code != http.StatusFound {
			t.Fatalf("callback status = %d, want %d; body=%s", w.Code, http.StatusFound, w.Body.String())
		}
		if fp.LastTokenRegion != region {
			t.Errorf("provider's token endpoint received region %q, want the collected value %q (AC8: {params.x} templating in OAuth URLs)", fp.LastTokenRegion, region)
		}
		got := fixture.getConnection(t, wired, initiated.ID)
		if got.Status != "ACTIVE" {
			t.Errorf("status = %q, want %q", got.Status, "ACTIVE")
		}
	})

	t.Run("executing the fake tool sends the region in its path and the secret apiKey as a header", func(t *testing.T) {
		status, dto := executeTool(t, wired, fixture.orgAuth, "fake-param-provider-get-widget", fixture.userID, initiated.ID, `{"widgetId":"widget-1"}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if !dto.Successful {
			t.Fatalf("successful = false, want true; error = %+v", dto.Error)
		}
		if fp.LastWidgetRegion != region {
			t.Errorf("widget request region = %q, want the collected value %q (AC8: {params.x} templating in a tool's path mapping)", fp.LastWidgetRegion, region)
		}
		if fp.LastWidgetAPIKeyHeader != apiKey {
			t.Errorf("widget request X-Api-Key header = %q, want the collected secret value %q (AC8: {params.x} templating in a tool's header mapping)", fp.LastWidgetAPIKeyHeader, apiKey)
		}
	})

	t.Run("the secret apiKey never appears in any log entry or API response", func(t *testing.T) {
		page := fetchLogs(t, wired, fixture.orgAuth, "?connectionId="+initiated.ID+"&limit=100")
		rawLogs, _ := json.Marshal(page)
		if strings.Contains(string(rawLogs), apiKey) {
			t.Fatalf("logs API response contains the raw secret apiKey value: %s", rawLogs)
		}

		connectionResponse := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/connections/"+initiated.ID, fixture.orgAuth, "")
		if strings.Contains(connectionResponse.Body.String(), apiKey) {
			t.Fatalf("get-connection response contains the raw secret apiKey value: %s", connectionResponse.Body.String())
		}

		rows, err := wired.DB.QueryContext(context.Background(),
			"SELECT request_body, response_body FROM event_logs WHERE connection_id = ?", initiated.ID)
		if err != nil {
			t.Fatalf("query event_logs: %v", err)
		}
		defer rows.Close()
		rowCount := 0
		for rows.Next() {
			rowCount++
			var requestBody, responseBody string
			if err := rows.Scan(&requestBody, &responseBody); err != nil {
				t.Fatalf("scan event_logs row: %v", err)
			}
			if strings.Contains(requestBody, apiKey) || strings.Contains(responseBody, apiKey) {
				t.Errorf("event_logs row contains the raw secret apiKey value: request=%q response=%q", requestBody, responseBody)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate event_logs rows: %v", err)
		}
		if rowCount != 2 {
			t.Fatalf("dumped %d event_logs rows for this connection, want 2 (one oauth_token_exchange, one tool_execution)", rowCount)
		}
	})
}
