//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for
// the package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, wireErrorEnvelope,
// doJSONRequest, createOrgAndKey (key_rotation_journey_integration_test.go),
// createEndpointAt/listEndpointsAt/findEndpointItem
// (webhook_endpoints_management_journey_integration_test.go), governanceDTO
// (governance_journey_integration_test.go), outlookDefinitionAgainst/
// openConnectPageAndGetState/initiatedConnectionDTO
// (oauth_handshake_journey_integration_test.go) — same package). This file
// tells Slice 9's story end to end against the real composition root, the
// real EndpointPorter/IntegrationExistenceChecker adapters over
// *delivery.Facade/*catalog.Facade (app/endpoint_porter.go,
// app/integration_checker.go), and a real SQLite database:
//
//  1. An org with real webhook-endpoint secrets, a real integration client
//     secret, an org API key, and a completed OAuth connection (real
//     access/refresh tokens) exports a document that contains none of that
//     secret material — by value AND by field name.
//  2. An import whose schemaVersion this installation doesn't support is
//     rejected before anything else runs, and nothing changes.
//  3. Importing with no dryRun query param at all defaults to a dry-run: it
//     reports the plan and flags a governance-referenced integration id this
//     installation doesn't have, and writes nothing.
//  4. mode=merge (dryRun=false) upserts the document's own settings and
//     leaves an endpoint the document never mentions alone.
//  5. mode=replace makes governance/endpoints match the document exactly,
//     deleting an endpoint the document omits.
//  6. The full config-migration journey: exporting org A's config and
//     importing it into a fresh org B mints org B its own fresh endpoint
//     secrets — never org A's.
package crucial_path

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"beecon/test/support"
)

type configGovernanceWireDTO struct {
	AllowList   *[]string `json:"allowList"`
	Hidden      []string  `json:"hidden"`
	Featured    []string  `json:"featured"`
	FeaturedCap int       `json:"featuredCap"`
}

type configEndpointWireDTO struct {
	URL        string    `json:"url"`
	EventTypes *[]string `json:"eventTypes"`
}

type configRetentionWireDTO struct {
	LogRetentionDays   *int `json:"logRetentionDays"`
	EventRetentionDays *int `json:"eventRetentionDays"`
}

type configDocumentWireDTO struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Governance    configGovernanceWireDTO `json:"governance"`
	Endpoints     []configEndpointWireDTO `json:"endpoints"`
	Retention     configRetentionWireDTO  `json:"retention"`
}

type configChangeWireDTO struct {
	Area   string `json:"area"`
	Field  string `json:"field"`
	Action string `json:"action"`
	Detail string `json:"detail"`
}

type importPlanWireDTO struct {
	Plan     []configChangeWireDTO `json:"plan"`
	Warnings []string              `json:"warnings"`
}

type importedSecretWireDTO struct {
	EndpointID string `json:"wepId"`
	Secret     string `json:"secret"`
}

type importApplyWireDTO struct {
	Applied []configChangeWireDTO   `json:"applied"`
	Secrets []importedSecretWireDTO `json:"secrets"`
}

func exportConfig(t *testing.T, router http.Handler, adminAuth, orgID string) (int, configDocumentWireDTO, string) {
	t.Helper()
	w := doJSONRequest(t, router, http.MethodGet, "/api/v1/organizations/"+orgID+"/config/export", adminAuth, "")
	var dto configDocumentWireDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode config export: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto, w.Body.String()
}

func importConfig(t *testing.T, router http.Handler, adminAuth, orgID, query, body string) (int, string) {
	t.Helper()
	path := "/api/v1/organizations/" + orgID + "/config/import"
	if query != "" {
		path += "?" + query
	}
	w := doJSONRequest(t, router, http.MethodPost, path, adminAuth, body)
	return w.Code, w.Body.String()
}

func decodeImportPlan(t *testing.T, rawBody string) importPlanWireDTO {
	t.Helper()
	var dto importPlanWireDTO
	if err := json.Unmarshal([]byte(rawBody), &dto); err != nil {
		t.Fatalf("decode import plan: %v; body=%s", err, rawBody)
	}
	return dto
}

func decodeImportApply(t *testing.T, rawBody string) importApplyWireDTO {
	t.Helper()
	var dto importApplyWireDTO
	if err := json.Unmarshal([]byte(rawBody), &dto); err != nil {
		t.Fatalf("decode import apply: %v; body=%s", err, rawBody)
	}
	return dto
}

func putGovernanceAdmin(t *testing.T, router http.Handler, adminAuth, orgID string, allowList *[]string, hidden, featured []string, cap int) int {
	t.Helper()
	allowListJSON := "null"
	if allowList != nil {
		encoded, err := json.Marshal(*allowList)
		if err != nil {
			t.Fatalf("marshal allowList: %v", err)
		}
		allowListJSON = string(encoded)
	}
	hiddenJSON, err := json.Marshal(hidden)
	if err != nil {
		t.Fatalf("marshal hidden: %v", err)
	}
	featuredJSON, err := json.Marshal(featured)
	if err != nil {
		t.Fatalf("marshal featured: %v", err)
	}
	body := fmt.Sprintf(`{"allowList":%s,"hidden":%s,"onboarding":{"featured":%s,"cap":%d}}`, allowListJSON, hiddenJSON, featuredJSON, cap)
	w := doJSONRequest(t, router, http.MethodPut, "/api/v1/organizations/"+orgID+"/governance", adminAuth, body)
	return w.Code
}

func putRetentionAdmin(t *testing.T, router http.Handler, adminAuth, orgID string, logDays *int) int {
	t.Helper()
	logDaysJSON := "null"
	if logDays != nil {
		logDaysJSON = strconv.Itoa(*logDays)
	}
	body := fmt.Sprintf(`{"logDays":%s,"eventDays":null}`, logDaysJSON)
	w := doJSONRequest(t, router, http.MethodPut, "/api/v1/organizations/"+orgID+"/retention", adminAuth, body)
	return w.Code
}

func getGovernanceAdmin(t *testing.T, router http.Handler, adminAuth, orgID string) (int, governanceDTO) {
	t.Helper()
	w := doJSONRequest(t, router, http.MethodGet, "/api/v1/organizations/"+orgID+"/governance", adminAuth, "")
	var dto governanceDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode governance: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto
}

// TestConfigExportJourney_NeverContainsSecretsByValueOrFieldNameEvenWithKeysEndpointsAndConnections
// is Slice 9's headline security AC: an org configured with real secret
// material across every module — an org API key, two webhook endpoints with
// real whsec_ secrets, an Integration with a real OAuth client secret, and a
// completed OAuth connection with real access/refresh tokens — exports a
// document that structurally and by-value carries none of it.
func TestConfigExportJourney_NeverContainsSecretsByValueOrFieldNameEvenWithKeysEndpointsAndConnections(t *testing.T) {
	const rawAccessToken = "config-journey-raw-access-token-value"
	const rawRefreshToken = "config-journey-raw-refresh-token-value"
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken:        rawAccessToken,
		RefreshToken:       rawRefreshToken,
		AccountEmail:       "ada@example.com",
		AccountDisplayName: "Ada Lovelace",
	})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionAgainst(fakeMS))
	adminAuth := "Bearer " + support.AdminAPIKey

	org, issued := createOrgAndKey(t, wired.Router, adminAuth, "Config Export Co")
	orgAuth := "Bearer " + issued.Key
	const allowedRedirectURI = "https://consumer.example.com/callback"

	if w := doJSONRequest(t, wired.Router, http.MethodPatch, "/api/v1/organizations/"+org.ID, adminAuth,
		`{"allowedRedirectUris":["`+allowedRedirectURI+`"]}`); w.Code != http.StatusOK {
		t.Fatalf("set allow-list status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	const rawClientSecret = "config-journey-client-secret-value"
	var integration integrationSummaryDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"outlook","clientId":"config-journey-client-id","clientSecret":"`+rawClientSecret+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create integration status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &integration); err != nil {
		t.Fatalf("decode integration: %v", err)
	}

	var user userDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", orgAuth, `{"name":"Ada Lovelace"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode user: %v", err)
	}

	initiateBody := `{"userId":"` + user.ID + `","integrationId":"` + integration.ID + `","redirectUri":"` + allowedRedirectURI + `"}`
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/initiate", orgAuth, initiateBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("initiate connection status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var initiated initiatedConnectionDTO
	if err := json.Unmarshal(w.Body.Bytes(), &initiated); err != nil {
		t.Fatalf("decode initiated connection: %v", err)
	}
	state := openConnectPageAndGetState(t, wired, initiated)
	w = doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")
	if w.Code != http.StatusFound {
		t.Fatalf("oauth callback status = %d, want %d; body=%s", w.Code, http.StatusFound, w.Body.String())
	}

	basePath := "/api/v1/organizations/" + org.ID + "/webhook-endpoints/"
	statusA, createdA := createEndpointAt(t, wired.Router, basePath, adminAuth, "https://example.com/hook-a", nil)
	if statusA != http.StatusCreated {
		t.Fatalf("create endpoint A status = %d, want %d", statusA, http.StatusCreated)
	}
	statusB, createdB := createEndpointAt(t, wired.Router, basePath, adminAuth, "https://example.com/hook-b", []string{"trigger.event"})
	if statusB != http.StatusCreated {
		t.Fatalf("create endpoint B status = %d, want %d", statusB, http.StatusCreated)
	}

	allowList := []string{integration.ID}
	if code := putGovernanceAdmin(t, wired.Router, adminAuth, org.ID, &allowList, []string{}, []string{}, 8); code != http.StatusOK {
		t.Fatalf("PUT governance status = %d, want %d", code, http.StatusOK)
	}
	logDays := 45
	if code := putRetentionAdmin(t, wired.Router, adminAuth, org.ID, &logDays); code != http.StatusOK {
		t.Fatalf("PUT retention status = %d, want %d", code, http.StatusOK)
	}

	status, doc, rawBody := exportConfig(t, wired.Router, adminAuth, org.ID)
	if status != http.StatusOK {
		t.Fatalf("export status = %d, want %d; body=%s", status, http.StatusOK, rawBody)
	}

	t.Run("the export reflects the org's actual governance, endpoints, and retention", func(t *testing.T) {
		if doc.SchemaVersion != 1 {
			t.Errorf("schemaVersion = %d, want 1", doc.SchemaVersion)
		}
		if doc.Governance.AllowList == nil || len(*doc.Governance.AllowList) != 1 || (*doc.Governance.AllowList)[0] != integration.ID {
			t.Errorf("governance.allowList = %v, want [%s]", doc.Governance.AllowList, integration.ID)
		}
		if len(doc.Endpoints) != 2 {
			t.Fatalf("len(endpoints) = %d, want 2", len(doc.Endpoints))
		}
		if doc.Retention.LogRetentionDays == nil || *doc.Retention.LogRetentionDays != 45 {
			t.Errorf("retention.logRetentionDays = %v, want 45", doc.Retention.LogRetentionDays)
		}
	})

	t.Run("the raw response body never contains any of the org's real secret values", func(t *testing.T) {
		secretsByName := map[string]string{
			"org API key":               issued.Key,
			"webhook endpoint A secret": createdA.Secret,
			"webhook endpoint B secret": createdB.Secret,
			"integration client secret": rawClientSecret,
			"oauth access token":        rawAccessToken,
			"oauth refresh token":       rawRefreshToken,
		}
		for name, secret := range secretsByName {
			if secret == "" {
				t.Fatalf("test setup: %s is empty", name)
			}
			if strings.Contains(rawBody, secret) {
				t.Errorf("export response contains the raw %s by value", name)
			}
		}
	})

	t.Run("the raw response body never contains a secret-shaped field name", func(t *testing.T) {
		for _, forbidden := range []string{
			"secret", "Secret", "whsec_", "credential", "Credential",
			"clientSecret", "apiKey", "accessToken", "refreshToken",
			"connectionId", "userId",
		} {
			if strings.Contains(rawBody, forbidden) {
				t.Errorf("export response body contains the forbidden substring %q: %s", forbidden, rawBody)
			}
		}
	})
}

// TestConfigImportJourney_SchemaVersionValidatedFirstAndWritesNothing is
// Slice 9's AC: an import document with a schemaVersion this installation
// doesn't support is rejected before anything else runs, and the org's
// existing config is provably unchanged afterward.
func TestConfigImportJourney_SchemaVersionValidatedFirstAndWritesNothing(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	org, _ := createOrgAndKey(t, wired.Router, adminAuth, "Schema Gate Co")

	allowList := []string{"intg_baseline"}
	if code := putGovernanceAdmin(t, wired.Router, adminAuth, org.ID, &allowList, nil, nil, 8); code != http.StatusOK {
		t.Fatalf("baseline PUT governance status = %d, want %d", code, http.StatusOK)
	}
	beforeStatus, beforeDoc, _ := exportConfig(t, wired.Router, adminAuth, org.ID)
	if beforeStatus != http.StatusOK {
		t.Fatalf("baseline export status = %d, want %d", beforeStatus, http.StatusOK)
	}

	badDoc := `{"schemaVersion":999,"governance":{"allowList":[],"hidden":[],"featured":[],"featuredCap":8},"endpoints":[],"retention":{"logRetentionDays":null,"eventRetentionDays":null}}`
	status, rawBody := importConfig(t, wired.Router, adminAuth, org.ID, "dryRun=false", badDoc)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", status, http.StatusUnprocessableEntity, rawBody)
	}
	var env wireErrorEnvelope
	if err := json.Unmarshal([]byte(rawBody), &env); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}

	afterStatus, afterDoc, _ := exportConfig(t, wired.Router, adminAuth, org.ID)
	if afterStatus != http.StatusOK {
		t.Fatalf("post-import export status = %d, want %d", afterStatus, http.StatusOK)
	}
	if afterDoc.Governance.AllowList == nil || len(*afterDoc.Governance.AllowList) != 1 || (*afterDoc.Governance.AllowList)[0] != "intg_baseline" {
		t.Errorf("allowList = %v after a rejected import, want the untouched baseline %v", afterDoc.Governance.AllowList, beforeDoc.Governance.AllowList)
	}
}

// TestConfigImportJourney_DryRunDefaultFlagsUnknownIntegrationIDsAndWritesNothing
// is PD46's default-safety AC: a request with no dryRun query param at all
// is a dry-run, reporting the plan and flagging a governance-referenced
// integration id this installation doesn't have — without writing anything.
func TestConfigImportJourney_DryRunDefaultFlagsUnknownIntegrationIDsAndWritesNothing(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	org, _ := createOrgAndKey(t, wired.Router, adminAuth, "Dry Run Default Co")

	var integration integrationSummaryDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"outlook","clientId":"cid","clientSecret":"csecret"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create integration status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &integration); err != nil {
		t.Fatalf("decode integration: %v", err)
	}

	doc := fmt.Sprintf(`{"schemaVersion":1,"governance":{"allowList":["%s","intg_totally_bogus"],"hidden":[],"featured":[],"featuredCap":8},"endpoints":[{"url":"https://example.com/would-be-created","eventTypes":null}],"retention":{"logRetentionDays":null,"eventRetentionDays":null}}`, integration.ID)

	status, rawBody := importConfig(t, wired.Router, adminAuth, org.ID, "", doc)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", status, http.StatusOK, rawBody)
	}
	plan := decodeImportPlan(t, rawBody)
	if len(plan.Plan) == 0 {
		t.Error("plan is empty, want it to describe the governance/endpoint changes this import would apply")
	}
	foundBogus, mentionedReal := false, false
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "intg_totally_bogus") {
			foundBogus = true
		}
		if strings.Contains(warning, integration.ID) {
			mentionedReal = true
		}
	}
	if !foundBogus {
		t.Errorf("warnings = %v, want one naming intg_totally_bogus", plan.Warnings)
	}
	if mentionedReal {
		t.Errorf("warnings = %v, want the REAL integration id %s never flagged", plan.Warnings, integration.ID)
	}

	govStatus, gov := getGovernanceAdmin(t, wired.Router, adminAuth, org.ID)
	if govStatus != http.StatusOK {
		t.Fatalf("GetGovernance status = %d, want %d", govStatus, http.StatusOK)
	}
	if gov.AllowList != nil {
		t.Errorf("allowList = %v after the default dry-run import, want nil (unwritten)", gov.AllowList)
	}
	endpointsStatus, page := listEndpointsAt(t, wired.Router, "/api/v1/organizations/"+org.ID+"/webhook-endpoints/", adminAuth)
	if endpointsStatus != http.StatusOK {
		t.Fatalf("ListEndpoints status = %d, want %d", endpointsStatus, http.StatusOK)
	}
	if len(page.Items) != 0 {
		t.Errorf("endpoints = %+v after the default dry-run import, want empty (unwritten)", page.Items)
	}
}

// TestConfigImportJourney_MergeUpsertsMentionedSettingsAndLeavesUnmentionedEndpointAlone
// is merge's headline AC exercised end to end: the document's own settings
// are upserted, an org setting the document never mentions is left alone,
// and an existing endpoint the document doesn't name survives untouched.
func TestConfigImportJourney_MergeUpsertsMentionedSettingsAndLeavesUnmentionedEndpointAlone(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	org, _ := createOrgAndKey(t, wired.Router, adminAuth, "Merge Journey Co")

	if code := putGovernanceAdmin(t, wired.Router, adminAuth, org.ID, nil, []string{"intg_stays_hidden"}, nil, 8); code != http.StatusOK {
		t.Fatalf("baseline PUT governance status = %d, want %d", code, http.StatusOK)
	}
	basePath := "/api/v1/organizations/" + org.ID + "/webhook-endpoints/"
	if status, _ := createEndpointAt(t, wired.Router, basePath, adminAuth, "https://example.com/untouched", nil); status != http.StatusCreated {
		t.Fatalf("baseline create endpoint status = %d, want %d", status, http.StatusCreated)
	}

	var integration integrationSummaryDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"outlook","clientId":"cid","clientSecret":"csecret"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create integration status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &integration); err != nil {
		t.Fatalf("decode integration: %v", err)
	}

	doc := fmt.Sprintf(`{"schemaVersion":1,"governance":{"allowList":["%s"],"hidden":[],"featured":[],"featuredCap":8},"endpoints":[{"url":"https://example.com/created-by-merge","eventTypes":null}],"retention":{"logRetentionDays":null,"eventRetentionDays":null}}`, integration.ID)
	status, rawBody := importConfig(t, wired.Router, adminAuth, org.ID, "dryRun=false", doc)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", status, http.StatusOK, rawBody)
	}
	applied := decodeImportApply(t, rawBody)
	if len(applied.Secrets) != 1 || !strings.HasPrefix(applied.Secrets[0].Secret, "whsec_") {
		t.Fatalf("secrets = %+v, want exactly one freshly minted whsec_ secret", applied.Secrets)
	}

	govStatus, gov := getGovernanceAdmin(t, wired.Router, adminAuth, org.ID)
	if govStatus != http.StatusOK {
		t.Fatalf("GetGovernance status = %d, want %d", govStatus, http.StatusOK)
	}
	if gov.AllowList == nil || len(*gov.AllowList) != 1 || (*gov.AllowList)[0] != integration.ID {
		t.Errorf("allowList = %v, want [%s] (the doc's mentioned field upserted)", gov.AllowList, integration.ID)
	}
	if len(gov.Hidden) != 1 || gov.Hidden[0] != "intg_stays_hidden" {
		t.Errorf("hidden = %v, want [intg_stays_hidden] — merge must leave a field the doc never mentions untouched", gov.Hidden)
	}

	endpointsStatus, page := listEndpointsAt(t, wired.Router, basePath, adminAuth)
	if endpointsStatus != http.StatusOK {
		t.Fatalf("ListEndpoints status = %d, want %d", endpointsStatus, http.StatusOK)
	}
	if len(page.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2 (the untouched pre-existing endpoint plus the newly created one)", len(page.Items))
	}
	urls := map[string]bool{}
	for _, item := range page.Items {
		urls[item.URL] = true
	}
	if !urls["https://example.com/untouched"] {
		t.Error("the pre-existing endpoint the doc never mentioned was removed — merge must never delete")
	}
	if !urls["https://example.com/created-by-merge"] {
		t.Error("the doc's new endpoint was not created")
	}
}

// TestConfigImportJourney_ReplaceDeletesAnEndpointAbsentFromTheDocumentAndFullyReplacesGovernance
// is replace's headline AC exercised end to end: an existing endpoint whose
// URL the document omits is deleted, and governance matches the document
// exactly — including clearing a field the document leaves empty, unlike
// merge.
func TestConfigImportJourney_ReplaceDeletesAnEndpointAbsentFromTheDocumentAndFullyReplacesGovernance(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	org, _ := createOrgAndKey(t, wired.Router, adminAuth, "Replace Journey Co")

	if code := putGovernanceAdmin(t, wired.Router, adminAuth, org.ID, nil, []string{"intg_should_clear"}, nil, 8); code != http.StatusOK {
		t.Fatalf("baseline PUT governance status = %d, want %d", code, http.StatusOK)
	}
	basePath := "/api/v1/organizations/" + org.ID + "/webhook-endpoints/"
	if status, _ := createEndpointAt(t, wired.Router, basePath, adminAuth, "https://example.com/keep-me", nil); status != http.StatusCreated {
		t.Fatalf("create endpoint (keep) status = %d, want %d", status, http.StatusCreated)
	}
	if status, _ := createEndpointAt(t, wired.Router, basePath, adminAuth, "https://example.com/delete-me", nil); status != http.StatusCreated {
		t.Fatalf("create endpoint (delete) status = %d, want %d", status, http.StatusCreated)
	}

	var integration integrationSummaryDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"outlook","clientId":"cid","clientSecret":"csecret"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create integration status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &integration); err != nil {
		t.Fatalf("decode integration: %v", err)
	}

	doc := fmt.Sprintf(`{"schemaVersion":1,"governance":{"allowList":["%s"],"hidden":[],"featured":[],"featuredCap":8},"endpoints":[{"url":"https://example.com/keep-me","eventTypes":["trigger.event"]}],"retention":{"logRetentionDays":null,"eventRetentionDays":null}}`, integration.ID)
	status, rawBody := importConfig(t, wired.Router, adminAuth, org.ID, "dryRun=false&mode=replace", doc)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", status, http.StatusOK, rawBody)
	}

	endpointsStatus, page := listEndpointsAt(t, wired.Router, basePath, adminAuth)
	if endpointsStatus != http.StatusOK {
		t.Fatalf("ListEndpoints status = %d, want %d", endpointsStatus, http.StatusOK)
	}
	if len(page.Items) != 1 || page.Items[0].URL != "https://example.com/keep-me" {
		t.Fatalf("items = %+v, want exactly [keep-me] — the doc-absent endpoint must be deleted under replace", page.Items)
	}
	if len(page.Items[0].EventTypes) != 1 || page.Items[0].EventTypes[0] != "trigger.event" {
		t.Errorf("surviving endpoint's eventTypes = %v, want [trigger.event] (replace also updates what it keeps)", page.Items[0].EventTypes)
	}

	govStatus, gov := getGovernanceAdmin(t, wired.Router, adminAuth, org.ID)
	if govStatus != http.StatusOK {
		t.Fatalf("GetGovernance status = %d, want %d", govStatus, http.StatusOK)
	}
	if len(gov.Hidden) != 0 {
		t.Errorf("hidden = %v, want cleared — replace must match the document exactly, including removing what it omits", gov.Hidden)
	}
	if gov.AllowList == nil || len(*gov.AllowList) != 1 || (*gov.AllowList)[0] != integration.ID {
		t.Errorf("allowList = %v, want [%s]", gov.AllowList, integration.ID)
	}
}

// TestConfigMigrationJourney_ExportOrgAImportIntoOrgBMintsFreshSecretsNeverCrossingOver
// is Slice 9's end-to-end "move config between installations" story: org
// A's exported document, imported into a fresh org B (dry-run showing the
// plan, then applied as a merge), gives org B its own governance and
// endpoints — with brand-new endpoint secrets that are never org A's own,
// and org A's real secret never appears anywhere in the transfer.
func TestConfigMigrationJourney_ExportOrgAImportIntoOrgBMintsFreshSecretsNeverCrossingOver(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	orgA, _ := createOrgAndKey(t, wired.Router, adminAuth, "Migration Org A")
	orgB, _ := createOrgAndKey(t, wired.Router, adminAuth, "Migration Org B")

	var integration integrationSummaryDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"outlook","clientId":"cid","clientSecret":"csecret"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create integration status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &integration); err != nil {
		t.Fatalf("decode integration: %v", err)
	}

	allowList := []string{integration.ID}
	if code := putGovernanceAdmin(t, wired.Router, adminAuth, orgA.ID, &allowList, []string{}, []string{}, 8); code != http.StatusOK {
		t.Fatalf("PUT org A governance status = %d, want %d", code, http.StatusOK)
	}
	logDays := 45
	if code := putRetentionAdmin(t, wired.Router, adminAuth, orgA.ID, &logDays); code != http.StatusOK {
		t.Fatalf("PUT org A retention status = %d, want %d", code, http.StatusOK)
	}
	orgABasePath := "/api/v1/organizations/" + orgA.ID + "/webhook-endpoints/"
	statusA, createdA := createEndpointAt(t, wired.Router, orgABasePath, adminAuth, "https://example.com/migrated-hook", nil)
	if statusA != http.StatusCreated {
		t.Fatalf("create org A endpoint status = %d, want %d", statusA, http.StatusCreated)
	}

	exportStatus, exported, exportedRawBody := exportConfig(t, wired.Router, adminAuth, orgA.ID)
	if exportStatus != http.StatusOK {
		t.Fatalf("export org A status = %d, want %d", exportStatus, http.StatusOK)
	}
	if strings.Contains(exportedRawBody, createdA.Secret) {
		t.Fatalf("org A's own export already leaked its endpoint secret: %s", exportedRawBody)
	}
	docBytes, err := json.Marshal(exported)
	if err != nil {
		t.Fatalf("re-marshal exported document: %v", err)
	}
	docJSON := string(docBytes)

	t.Run("importing org A's document into org B as a dry-run shows the plan and writes nothing", func(t *testing.T) {
		status, rawBody := importConfig(t, wired.Router, adminAuth, orgB.ID, "", docJSON)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", status, http.StatusOK, rawBody)
		}
		plan := decodeImportPlan(t, rawBody)
		if len(plan.Plan) == 0 {
			t.Error("plan is empty, want it to describe org B gaining org A's governance/retention/endpoint")
		}
		if strings.Contains(rawBody, createdA.Secret) {
			t.Errorf("dry-run plan response contains org A's raw endpoint secret: %s", rawBody)
		}
		govStatus, gov := getGovernanceAdmin(t, wired.Router, adminAuth, orgB.ID)
		if govStatus != http.StatusOK {
			t.Fatalf("GetGovernance (org B) status = %d, want %d", govStatus, http.StatusOK)
		}
		if gov.AllowList != nil {
			t.Errorf("org B's allowList = %v after a dry-run, want nil (unwritten)", gov.AllowList)
		}
	})

	var mintedSecret string
	t.Run("applying the merge gives org B its own governance, retention, and endpoint with a freshly minted secret", func(t *testing.T) {
		status, rawBody := importConfig(t, wired.Router, adminAuth, orgB.ID, "dryRun=false", docJSON)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", status, http.StatusOK, rawBody)
		}
		applied := decodeImportApply(t, rawBody)
		if len(applied.Secrets) != 1 {
			t.Fatalf("secrets = %+v, want exactly one freshly minted secret for org B's new endpoint", applied.Secrets)
		}
		mintedSecret = applied.Secrets[0].Secret
		if !strings.HasPrefix(mintedSecret, "whsec_") {
			t.Errorf("minted secret = %q, want a whsec_-prefixed secret", mintedSecret)
		}
		if mintedSecret == createdA.Secret {
			t.Error("org B's freshly minted secret equals org A's own secret — secrets must never cross organizations")
		}
		if strings.Contains(rawBody, createdA.Secret) {
			t.Errorf("apply response contains org A's raw endpoint secret: %s", rawBody)
		}

		govStatus, gov := getGovernanceAdmin(t, wired.Router, adminAuth, orgB.ID)
		if govStatus != http.StatusOK {
			t.Fatalf("GetGovernance (org B) status = %d, want %d", govStatus, http.StatusOK)
		}
		if gov.AllowList == nil || len(*gov.AllowList) != 1 || (*gov.AllowList)[0] != integration.ID {
			t.Errorf("org B's allowList = %v, want [%s]", gov.AllowList, integration.ID)
		}

		orgBBasePath := "/api/v1/organizations/" + orgB.ID + "/webhook-endpoints/"
		endpointsStatus, page := listEndpointsAt(t, wired.Router, orgBBasePath, adminAuth)
		if endpointsStatus != http.StatusOK {
			t.Fatalf("ListEndpoints (org B) status = %d, want %d", endpointsStatus, http.StatusOK)
		}
		if len(page.Items) != 1 || page.Items[0].URL != "https://example.com/migrated-hook" {
			t.Fatalf("org B's endpoints = %+v, want exactly one at the migrated URL", page.Items)
		}
	})

	t.Run("org A's own endpoint is unaffected by the transfer into org B", func(t *testing.T) {
		endpointsStatus, page := listEndpointsAt(t, wired.Router, orgABasePath, adminAuth)
		if endpointsStatus != http.StatusOK {
			t.Fatalf("ListEndpoints (org A) status = %d, want %d", endpointsStatus, http.StatusOK)
		}
		if len(page.Items) != 1 || page.Items[0].ID != createdA.ID {
			t.Fatalf("org A's endpoints = %+v, want its own single endpoint untouched", page.Items)
		}
	})
}
