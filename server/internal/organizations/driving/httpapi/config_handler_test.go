// config_handler_test.go (in-package test, same package as
// handler_test.go/governance_handler_test.go — reuses their
// wireErrorEnvelope, decodeError, and doRequestAsOrg helpers). Slice 9
// (PD46): ExportConfig/ImportConfig read the organization only from request
// context, injected in production by the admin console's org-scoped mount —
// these tests inject that context directly, same shortcut the other
// handler tests already use. A fake EndpointPorter/IntegrationExistenceChecker
// stand in for the app/ composition-root adapters over delivery/catalog
// (organizations cannot import either — BOUNDARIES).
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"beecon/internal/httpx"
	"beecon/internal/organizations"
	memory "beecon/internal/organizations/driven/memory"
)

type fakeConfigEndpointPorter struct {
	nextID  int
	byOrg   map[organizations.OrgID][]organizations.PortedEndpoint
	secrets map[string]string
}

func newFakeConfigEndpointPorter() *fakeConfigEndpointPorter {
	return &fakeConfigEndpointPorter{byOrg: map[organizations.OrgID][]organizations.PortedEndpoint{}, secrets: map[string]string{}}
}

var _ organizations.EndpointPorter = (*fakeConfigEndpointPorter)(nil)

func (f *fakeConfigEndpointPorter) ListEndpoints(_ context.Context, org organizations.OrgID) ([]organizations.PortedEndpoint, error) {
	items := make([]organizations.PortedEndpoint, len(f.byOrg[org]))
	copy(items, f.byOrg[org])
	return items, nil
}

func (f *fakeConfigEndpointPorter) CreateEndpoint(_ context.Context, org organizations.OrgID, url string, eventTypes []string) (organizations.PortedEndpointSecret, error) {
	f.nextID++
	id := fmt.Sprintf("wep_htest_%d", f.nextID)
	secret := fmt.Sprintf("whsec_htest_secret_%d", f.nextID)
	f.byOrg[org] = append(f.byOrg[org], organizations.PortedEndpoint{ID: id, URL: url, EventTypes: eventTypes})
	f.secrets[id] = secret
	return organizations.PortedEndpointSecret{ID: id, Secret: secret}, nil
}

func (f *fakeConfigEndpointPorter) UpdateEndpoint(_ context.Context, org organizations.OrgID, endpointID, url string, eventTypes []string) error {
	items := f.byOrg[org]
	for i := range items {
		if items[i].ID == endpointID {
			items[i].URL = url
			items[i].EventTypes = eventTypes
			return nil
		}
	}
	return fmt.Errorf("endpoint %q not found in org %q", endpointID, org)
}

func (f *fakeConfigEndpointPorter) DeleteEndpoint(_ context.Context, org organizations.OrgID, endpointID string) error {
	items := f.byOrg[org]
	for i := range items {
		if items[i].ID == endpointID {
			f.byOrg[org] = append(items[:i:i], items[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("endpoint %q not found in org %q", endpointID, org)
}

type fakeConfigIntegrationChecker struct {
	existing map[string]bool
}

var _ organizations.IntegrationExistenceChecker = fakeConfigIntegrationChecker{}

func (f fakeConfigIntegrationChecker) IntegrationExists(_ context.Context, id string) (bool, error) {
	return f.existing[id], nil
}

func newConfigTestRouter() (chi.Router, *fakeConfigEndpointPorter) {
	porter := newFakeConfigEndpointPorter()
	facade := memory.NewFacadeWithOverrides(memory.Overrides{}).
		WithEndpointPorter(porter).
		WithIntegrationChecker(fakeConfigIntegrationChecker{existing: map[string]bool{"intg_real": true}})
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Get("/organizations/{orgId}/config/export", h.ExportConfig)
	r.Post("/organizations/{orgId}/config/import", h.ImportConfig)
	r.Get("/organizations/{orgId}/governance", h.GetGovernance)
	r.Put("/organizations/{orgId}/governance", h.UpdateGovernance)
	return r, porter
}

func TestExportConfig_Returns401WhenNoOrgInContext(t *testing.T) {
	r, _ := newConfigTestRouter()

	w := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/config/export", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestImportConfig_Returns401WhenNoOrgInContext(t *testing.T) {
	r, _ := newConfigTestRouter()

	w := doRequestAsOrg(r, http.MethodPost, "/organizations/org_1/config/import", "", `{"schemaVersion":1}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestExportConfig_ReturnsTheDocumentedShapeIncludingConfiguredEndpoints pins
// the export's own JSON contract (Slice 9, API Shape): schemaVersion,
// governance{allowList,hidden,featured,featuredCap}, endpoints[{url,
// eventTypes}], retention{logRetentionDays,eventRetentionDays} — nothing
// else.
func TestExportConfig_ReturnsTheDocumentedShapeIncludingConfiguredEndpoints(t *testing.T) {
	r, porter := newConfigTestRouter()
	if _, err := porter.CreateEndpoint(context.Background(), "org_1", "https://example.com/hook", []string{"trigger.fired"}); err != nil {
		t.Fatalf("seed endpoint: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/config/export", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto configDocumentDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.SchemaVersion != organizations.CurrentConfigSchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", dto.SchemaVersion, organizations.CurrentConfigSchemaVersion)
	}
	if len(dto.Endpoints) != 1 || dto.Endpoints[0].URL != "https://example.com/hook" {
		t.Fatalf("endpoints = %+v, want one at https://example.com/hook", dto.Endpoints)
	}
	if dto.Endpoints[0].EventTypes == nil || len(*dto.Endpoints[0].EventTypes) != 1 || (*dto.Endpoints[0].EventTypes)[0] != "trigger.fired" {
		t.Errorf("endpoints[0].eventTypes = %v, want [trigger.fired]", dto.Endpoints[0].EventTypes)
	}
	if dto.Governance.FeaturedCap != organizations.DefaultFeaturedCap {
		t.Errorf("governance.featuredCap = %d, want the platform default %d", dto.Governance.FeaturedCap, organizations.DefaultFeaturedCap)
	}
}

// TestExportConfig_ResponseBodyNeverContainsASecretShapedFieldName is Slice
// 9's no-secrets AC pinned at the raw HTTP response body: even with an
// endpoint on record, the export response never contains a
// secret/credential/token-shaped field name anywhere in its JSON.
func TestExportConfig_ResponseBodyNeverContainsASecretShapedFieldName(t *testing.T) {
	r, porter := newConfigTestRouter()
	if _, err := porter.CreateEndpoint(context.Background(), "org_1", "https://example.com/hook", nil); err != nil {
		t.Fatalf("seed endpoint: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/config/export", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.String()
	for _, forbidden := range []string{"secret", "Secret", "whsec_", "credential", "Credential", "clientSecret", "apiKey", "token", "Token"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("export response body contains the forbidden substring %q: %s", forbidden, body)
		}
	}
}

// TestImportConfig_AbsentDryRunQueryParamDefaultsToADryRunThatWritesNothing
// is PD46's default at the HTTP boundary: a request with no dryRun query
// param at all is treated exactly like dryRun=true.
func TestImportConfig_AbsentDryRunQueryParamDefaultsToADryRunThatWritesNothing(t *testing.T) {
	r, _ := newConfigTestRouter()
	body := `{"schemaVersion":1,"governance":{"allowList":["intg_real"],"hidden":[],"featured":[],"featuredCap":8},"endpoints":[],"retention":{"logRetentionDays":null,"eventRetentionDays":null}}`

	w := doRequestAsOrg(r, http.MethodPost, "/organizations/org_1/config/import", "org_1", body)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var plan importPlanDTO
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatalf("decode body as a dry-run plan: %v; body=%s", err, w.Body.String())
	}
	if len(plan.Plan) == 0 {
		t.Error("plan is empty, want it to describe the governance change the import would apply")
	}

	getResp := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/governance", "org_1", "")
	var governance governanceDTO
	if err := json.Unmarshal(getResp.Body.Bytes(), &governance); err != nil {
		t.Fatalf("decode governance: %v", err)
	}
	if governance.AllowList != nil {
		t.Errorf("allowList = %v after the default dry-run import, want nil (unwritten)", governance.AllowList)
	}
}

// TestImportConfig_DryRunFalseAppliesAndReturnsTheAppliedShape is the
// dryRun=false counterpart: only the literal "false" turns off the default
// dry-run, and applying returns the applied/secrets shape instead of a plan.
func TestImportConfig_DryRunFalseAppliesAndReturnsTheAppliedShape(t *testing.T) {
	r, _ := newConfigTestRouter()
	body := `{"schemaVersion":1,"governance":{"allowList":["intg_real"],"hidden":[],"featured":[],"featuredCap":8},"endpoints":[{"url":"https://example.com/new-hook","eventTypes":null}],"retention":{"logRetentionDays":null,"eventRetentionDays":null}}`

	w := doRequestAsOrg(r, http.MethodPost, "/organizations/org_1/config/import?dryRun=false", "org_1", body)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var applied importApplyDTO
	if err := json.Unmarshal(w.Body.Bytes(), &applied); err != nil {
		t.Fatalf("decode body as an apply result: %v; body=%s", err, w.Body.String())
	}
	if len(applied.Applied) == 0 {
		t.Error("applied is empty, want it to describe the governance/endpoint changes actually made")
	}
	if len(applied.Secrets) != 1 || !strings.HasPrefix(applied.Secrets[0].Secret, "whsec_") {
		t.Fatalf("secrets = %+v, want exactly one freshly minted whsec_ secret for the new endpoint", applied.Secrets)
	}

	getResp := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/governance", "org_1", "")
	var governance governanceDTO
	if err := json.Unmarshal(getResp.Body.Bytes(), &governance); err != nil {
		t.Fatalf("decode governance: %v", err)
	}
	if governance.AllowList == nil || len(*governance.AllowList) != 1 || (*governance.AllowList)[0] != "intg_real" {
		t.Errorf("allowList = %v after dryRun=false, want [intg_real] (applied)", governance.AllowList)
	}
}

// TestImportConfig_AnUnsupportedSchemaVersionIs422AndWritesNothing is Slice
// 9's schema-version-first AC at the HTTP boundary.
func TestImportConfig_AnUnsupportedSchemaVersionIs422AndWritesNothing(t *testing.T) {
	r, _ := newConfigTestRouter()
	// Baseline: give org_1 a known allow-list so a (would-be, wrongly
	// applied) write is detectable.
	baseline := doRequestAsOrg(r, http.MethodPut, "/organizations/org_1/governance", "org_1",
		`{"allowList":["intg_real"],"hidden":[],"onboarding":{"featured":[],"cap":8}}`)
	if baseline.Code != http.StatusOK {
		t.Fatalf("baseline PUT governance status = %d, want %d; body=%s", baseline.Code, http.StatusOK, baseline.Body.String())
	}

	w := doRequestAsOrg(r, http.MethodPost, "/organizations/org_1/config/import?dryRun=false", "org_1",
		`{"schemaVersion":999,"governance":{"allowList":[],"hidden":[],"featured":[],"featuredCap":8},"endpoints":[],"retention":{}}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}

	getResp := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/governance", "org_1", "")
	var governance governanceDTO
	if err := json.Unmarshal(getResp.Body.Bytes(), &governance); err != nil {
		t.Fatalf("decode governance: %v", err)
	}
	if governance.AllowList == nil || len(*governance.AllowList) != 1 || (*governance.AllowList)[0] != "intg_real" {
		t.Errorf("allowList = %v, want the untouched baseline [intg_real] — an unsupported schema version must write nothing", governance.AllowList)
	}
}

// TestImportConfig_ReplaceModeDeletesAnEndpointAbsentFromTheDocument is
// replace's headline AC exercised through the mode query param at the HTTP
// boundary.
func TestImportConfig_ReplaceModeDeletesAnEndpointAbsentFromTheDocument(t *testing.T) {
	r, porter := newConfigTestRouter()
	if _, err := porter.CreateEndpoint(context.Background(), "org_1", "https://example.com/to-delete", nil); err != nil {
		t.Fatalf("seed endpoint: %v", err)
	}

	body := `{"schemaVersion":1,"governance":{"allowList":null,"hidden":[],"featured":[],"featuredCap":8},"endpoints":[],"retention":{"logRetentionDays":null,"eventRetentionDays":null}}`
	w := doRequestAsOrg(r, http.MethodPost, "/organizations/org_1/config/import?dryRun=false&mode=replace", "org_1", body)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	endpoints, err := porter.ListEndpoints(context.Background(), "org_1")
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(endpoints) != 0 {
		t.Errorf("endpoints = %+v after a replace import omitting every URL, want empty", endpoints)
	}
}

// TestConfigExport_IsStrictlyOrgScopedAtTheHandlerLayer is Slice 9's
// isolation AC exercised through the HTTP handlers directly: exporting one
// org's config must never reflect a different org's own settings/endpoints.
func TestConfigExport_IsStrictlyOrgScopedAtTheHandlerLayer(t *testing.T) {
	r, porter := newConfigTestRouter()
	putA := doRequestAsOrg(r, http.MethodPut, "/organizations/org_a/governance", "org_a",
		`{"allowList":["intg_a_only"],"hidden":[],"onboarding":{"featured":[],"cap":8}}`)
	if putA.Code != http.StatusOK {
		t.Fatalf("PUT org_a governance status = %d, want %d; body=%s", putA.Code, http.StatusOK, putA.Body.String())
	}
	if _, err := porter.CreateEndpoint(context.Background(), "org_a", "https://example.com/org-a-only", nil); err != nil {
		t.Fatalf("seed org_a endpoint: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodGet, "/organizations/org_b/config/export", "org_b", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dtoB configDocumentDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dtoB); err != nil {
		t.Fatalf("decode org_b export: %v", err)
	}
	if dtoB.Governance.AllowList != nil {
		t.Errorf("org_b's allowList = %v, want nil — org_a's allow-list must never leak across", dtoB.Governance.AllowList)
	}
	if len(dtoB.Endpoints) != 0 {
		t.Errorf("org_b's endpoints = %+v, want empty — org_a's endpoint must never leak across", dtoB.Endpoints)
	}
}
