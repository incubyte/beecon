// Package httpapi (in-package test) exercises the catalog handlers through an
// actual chi router mounted with the same route patterns app/router.go uses,
// backed by the driven/memory facade with a fake provider definition so tests
// don't depend on the real embedded outlook.yaml.
package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"beecon/internal/catalog"
	memory "beecon/internal/catalog/driven/memory"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

func fakeDefinitions() []catalog.ProviderDefinition {
	return []catalog.ProviderDefinition{
		{
			Slug:         "outlook",
			Name:         "Outlook",
			Logo:         "https://static.beecon.dev/providers/outlook.png",
			AuthScheme:   "oauth2",
			AuthorizeURL: "https://example.com/authorize",
			TokenURL:     "https://example.com/token",
			Scopes:       []string{"Mail.Read"},
		},
	}
}

func newTestRouter(t *testing.T) chi.Router {
	t.Helper()
	facade, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitions()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Post("/", h.Create)
	r.Get("/", h.List)
	return r
}

func doRequest(r chi.Router, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// doRequestAsOrg mirrors organizations/driving/httpapi's helper: List reads
// the organization only from the request context, injected by OrgAuth in
// production.
func doRequestAsOrg(r chi.Router, method, path string, org organizations.OrgID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if org != "" {
		req = req.WithContext(organizations.WithOrgID(req.Context(), org))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

type wireErrorEnvelope struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details,omitempty"`
	} `json:"error"`
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) wireErrorEnvelope {
	t.Helper()
	var env wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("response body is not the PD5 error envelope: %v\nbody: %s", err, w.Body.String())
	}
	return env
}

func TestCreate_Returns201WithTheIntegrationSummaryDTO(t *testing.T) {
	r := newTestRouter(t)

	w := doRequest(r, http.MethodPost, "/", `{"providerSlug":"outlook","clientId":"cid","clientSecret":"csecret"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto integrationSummaryDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if !strings.HasPrefix(dto.ID, "intg_") {
		t.Errorf("id = %q, want it to start with %q", dto.ID, "intg_")
	}
	if dto.ProviderSlug != "outlook" {
		t.Errorf("providerSlug = %q, want %q", dto.ProviderSlug, "outlook")
	}
	if dto.Name != "Outlook" {
		t.Errorf("name = %q, want %q", dto.Name, "Outlook")
	}
	if dto.AuthScheme != "oauth2" {
		t.Errorf("authScheme = %q, want %q", dto.AuthScheme, "oauth2")
	}
}

// TestCreate_ResponseBodyNeverContainsTheClientSecret is AC4, asserted at the
// wire level: the raw HTTP response bytes must never contain the secret
// string supplied at creation.
func TestCreate_ResponseBodyNeverContainsTheClientSecret(t *testing.T) {
	r := newTestRouter(t)
	const secret = "super-secret-oauth-client-secret"

	w := doRequest(r, http.MethodPost, "/", `{"providerSlug":"outlook","clientId":"cid","clientSecret":"`+secret+`"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if strings.Contains(w.Body.String(), secret) {
		t.Fatalf("response body contains the client secret: %s", w.Body.String())
	}
}

func TestCreate_Returns422ForAnUnknownProviderSlug(t *testing.T) {
	r := newTestRouter(t)

	w := doRequest(r, http.MethodPost, "/", `{"providerSlug":"does-not-exist","clientId":"cid","clientSecret":"csecret"}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
}

func TestCreate_Returns422ForAMalformedJSONBody(t *testing.T) {
	r := newTestRouter(t)

	w := doRequest(r, http.MethodPost, "/", `{"providerSlug":`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

func TestList_Returns200WithEveryCreatedIntegrationSummary(t *testing.T) {
	r := newTestRouter(t)
	doRequest(r, http.MethodPost, "/", `{"providerSlug":"outlook","clientId":"cid","clientSecret":"csecret"}`)

	w := doRequestAsOrg(r, http.MethodGet, "/", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dtos []integrationSummaryDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dtos); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(dtos) != 1 {
		t.Fatalf("len(dtos) = %d, want 1", len(dtos))
	}
	if dtos[0].ProviderSlug != "outlook" {
		t.Errorf("providerSlug = %q, want %q", dtos[0].ProviderSlug, "outlook")
	}
}

// TestList_IsIdenticalRegardlessOfWhichOrganizationAsks is PD7: Phase 1
// integrations are installation-level, visible to every organization.
func TestList_IsIdenticalRegardlessOfWhichOrganizationAsks(t *testing.T) {
	r := newTestRouter(t)
	doRequest(r, http.MethodPost, "/", `{"providerSlug":"outlook","clientId":"cid","clientSecret":"csecret"}`)

	wA := doRequestAsOrg(r, http.MethodGet, "/", "org_a", "")
	wB := doRequestAsOrg(r, http.MethodGet, "/", "org_b", "")

	if wA.Body.String() != wB.Body.String() {
		t.Errorf("org A's list (%s) differs from org B's list (%s); PD7 says every organization sees the same installation-wide list", wA.Body.String(), wB.Body.String())
	}
}

func TestList_Returns401WhenNoOrgContext(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// --- ListTools / GetTool (Slice 1's catalog API) ---

func minimalSchema() map[string]any {
	return map[string]any{"type": "object"}
}

// fakeDefinitionsWithTools is fakeDefinitions' outlook provider plus two
// tools (one deprecated), so ListTools/GetTool have something to filter and
// fetch without depending on the real embedded outlook.yaml.
func fakeDefinitionsWithTools() []catalog.ProviderDefinition {
	defs := fakeDefinitions()
	defs[0].Tools = []catalog.ProviderTool{
		{Slug: "outlook-get-message", Name: "Get email message", Description: "Retrieves a message by id.", InputSchema: minimalSchema(), OutputSchema: minimalSchema()},
		{Slug: "outlook-legacy-tool", Name: "Legacy tool", Description: "Deprecated.", InputSchema: minimalSchema(), OutputSchema: minimalSchema(), Deprecated: true},
	}
	return defs
}

func newToolsTestRouter(t *testing.T) chi.Router {
	t.Helper()
	facade, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitionsWithTools()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Get("/tools", h.ListTools)
	r.Get("/tools/{slug}", h.GetTool)
	return r
}

func TestListTools_Returns200WithTheToolsPageShapeFilteredByProviderSlug(t *testing.T) {
	r := newToolsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/tools?providerSlug=outlook", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page toolsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1 (the deprecated tool is excluded by default)", len(page.Items))
	}
	item := page.Items[0]
	if item.Slug != "outlook-get-message" {
		t.Errorf("slug = %q, want %q", item.Slug, "outlook-get-message")
	}
	if item.Provider.Slug != "outlook" {
		t.Errorf("provider.slug = %q, want %q", item.Provider.Slug, "outlook")
	}
	if item.Deprecated {
		t.Error("deprecated = true, want false")
	}
}

func TestListTools_IncludeDeprecatedQueryParamIncludesTheDeprecatedTool(t *testing.T) {
	r := newToolsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/tools?providerSlug=outlook&includeDeprecated=true", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page toolsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(page.Items))
	}
}

func TestListTools_Returns401WhenNoOrgContext(t *testing.T) {
	r := newToolsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/tools", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestGetTool_Returns200WithTheToolDetail(t *testing.T) {
	r := newToolsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/tools/outlook-get-message", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto toolSummaryDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.Slug != "outlook-get-message" {
		t.Errorf("slug = %q, want %q", dto.Slug, "outlook-get-message")
	}
	if dto.Provider.Slug != "outlook" {
		t.Errorf("provider.slug = %q, want %q", dto.Provider.Slug, "outlook")
	}
}

// TestListTools_Returns422ForANonNumericLimit covers handler.go's
// parseIntQueryParam failure branch: a limit that cannot be parsed as an
// integer must fail loudly rather than silently falling back to the default.
func TestListTools_Returns422ForANonNumericLimit(t *testing.T) {
	r := newToolsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/tools?limit=not-a-number", "org_1", "")

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
}

func TestGetTool_Returns401WhenNoOrgContext(t *testing.T) {
	r := newToolsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/tools/outlook-get-message", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestGetTool_Returns404ForAnUnknownSlug(t *testing.T) {
	r := newToolsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/tools/does-not-exist", "org_1", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}

// --- GetExpectedParams (Slice 3, AC2) ---

// fakeDefinitionsWithExpectedParams is fakeDefinitions' Outlook provider plus
// a required non-secret "region" and a required secret "apiKey" expected
// param.
func fakeDefinitionsWithExpectedParams() []catalog.ProviderDefinition {
	defs := fakeDefinitions()
	defs[0].ExpectedParams = []catalog.ExpectedParam{
		{Name: "region", DisplayName: "Region", Description: "Your account's region.", Required: true, Secret: false},
		{Name: "apiKey", DisplayName: "API Key", Description: "Your account's API key.", Required: true, Secret: true},
	}
	return defs
}

// newExpectedParamsTestRouter wires GetExpectedParams behind the same route
// pattern app/router.go uses, returning the created integration's id so tests
// can address it.
func newExpectedParamsTestRouter(t *testing.T) (chi.Router, string) {
	t.Helper()
	facade, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitionsWithExpectedParams()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	created, err := facade.CreateIntegration(context.Background(), "outlook", "cid", "csecret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Get("/integrations/{intgId}/expected-params", h.GetExpectedParams)
	return r, string(created.ID)
}

func TestGetExpectedParams_Returns200WithTheProvidersNameAndFieldShapes(t *testing.T) {
	r, integrationID := newExpectedParamsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/integrations/"+integrationID+"/expected-params", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto expectedParamsDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.ProviderName != "Outlook" {
		t.Errorf("providerName = %q, want %q", dto.ProviderName, "Outlook")
	}
	if len(dto.Fields) != 2 {
		t.Fatalf("len(fields) = %d, want 2", len(dto.Fields))
	}
	byName := map[string]expectedParamFieldDTO{}
	for _, field := range dto.Fields {
		byName[field.Name] = field
	}
	region, ok := byName["region"]
	if !ok || !region.Required || region.Secret {
		t.Errorf("region field = %+v (present=%v), want required=true secret=false", region, ok)
	}
	apiKey, ok := byName["apiKey"]
	if !ok || !apiKey.Required || !apiKey.Secret {
		t.Errorf("apiKey field = %+v (present=%v), want required=true secret=true", apiKey, ok)
	}
}

func TestGetExpectedParams_Returns401WhenNoOrgContext(t *testing.T) {
	r, integrationID := newExpectedParamsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/integrations/"+integrationID+"/expected-params", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestGetExpectedParams_Returns404ForAnUnknownIntegrationID(t *testing.T) {
	r, _ := newExpectedParamsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/integrations/intg_does_not_exist/expected-params", "org_1", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}

// --- ListTriggerDefinitions / GetTriggerDefinition (Slice 1's catalog API) ---

// fakeDefinitionsWithTriggers is fakeDefinitions' outlook provider plus one
// trigger, so ListTriggerDefinitions/GetTriggerDefinition have something to
// filter and fetch without depending on the real embedded outlook.yaml.
func fakeDefinitionsWithTriggers() []catalog.ProviderDefinition {
	defs := fakeDefinitions()
	defs[0].Triggers = []catalog.TriggerDefinition{
		{
			Slug: "outlook-message-received", Name: "New message received",
			Description:   "Triggered when a new message arrives.",
			ConfigSchema:  minimalSchema(),
			PayloadSchema: minimalSchema(),
			Ingestion:     "poll", PollIntervalSeconds: 60,
		},
	}
	return defs
}

func newTriggerDefinitionsTestRouter(t *testing.T) chi.Router {
	t.Helper()
	facade, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitionsWithTriggers()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Get("/trigger-definitions", h.ListTriggerDefinitions)
	r.Get("/trigger-definitions/{slug}", h.GetTriggerDefinition)
	return r
}

func TestListTriggerDefinitions_Returns200WithTheTriggerDefinitionsPageShapeFilteredByProviderSlug(t *testing.T) {
	r := newTriggerDefinitionsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/trigger-definitions?providerSlug=outlook", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page triggerDefinitionsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(page.Items))
	}
	item := page.Items[0]
	if item.Slug != "outlook-message-received" {
		t.Errorf("slug = %q, want %q", item.Slug, "outlook-message-received")
	}
	if item.Ingestion != "poll" {
		t.Errorf("ingestion = %q, want %q", item.Ingestion, "poll")
	}
	if item.Provider.Slug != "outlook" {
		t.Errorf("provider.slug = %q, want %q", item.Provider.Slug, "outlook")
	}
	if len(item.ConfigSchema) == 0 || len(item.PayloadSchema) == 0 {
		t.Errorf("configSchema/payloadSchema must not be empty: %+v / %+v", item.ConfigSchema, item.PayloadSchema)
	}
}

func TestListTriggerDefinitions_Returns401WhenNoOrgContext(t *testing.T) {
	r := newTriggerDefinitionsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/trigger-definitions", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestListTriggerDefinitions_Returns404ForAnUnknownProviderSlug is Slice 1's
// AC6: listing trigger definitions for an unknown provider slug is not-found.
func TestListTriggerDefinitions_Returns404ForAnUnknownProviderSlug(t *testing.T) {
	r := newTriggerDefinitionsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/trigger-definitions?providerSlug=does-not-exist", "org_1", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}

func TestGetTriggerDefinition_Returns200WithTheTriggerDefinitionDetail(t *testing.T) {
	r := newTriggerDefinitionsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/trigger-definitions/outlook-message-received", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto triggerDefinitionSummaryDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.Slug != "outlook-message-received" {
		t.Errorf("slug = %q, want %q", dto.Slug, "outlook-message-received")
	}
	if dto.Provider.Slug != "outlook" {
		t.Errorf("provider.slug = %q, want %q", dto.Provider.Slug, "outlook")
	}
}

func TestGetTriggerDefinition_Returns401WhenNoOrgContext(t *testing.T) {
	r := newTriggerDefinitionsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/trigger-definitions/outlook-message-received", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestGetTriggerDefinition_Returns404ForAnUnknownSlug(t *testing.T) {
	r := newTriggerDefinitionsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/trigger-definitions/does-not-exist", "org_1", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}
