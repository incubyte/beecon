// facade_config_test.go exercises Slice 9's (PD46) config export/import
// story at the facade layer: Facade.ExportConfig/ImportConfig wired against
// the in-memory Repository plus a fake EndpointPorter/IntegrationExistenceChecker
// (organizations cannot depend on a real delivery/catalog facade —
// BOUNDARIES — so these fakes stand in for the app/ composition-root
// adapters exactly the way a consumer-defined port is meant to be tested).
// See config_handler_test.go for the HTTP boundary and
// test/crucial_path/config_export_import_journey_integration_test.go for the
// real end-to-end wiring (real delivery secrets, real cross-org transfer).
package organizations_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"beecon/internal/organizations"
	memory "beecon/internal/organizations/driven/memory"
)

// fakeEndpointPorter is a minimal in-memory stand-in for
// organizations.EndpointPorter (Slice 9): CreateEndpoint mints a
// deterministic, obviously-fake "whsec_"-prefixed secret so tests can assert
// a fresh one is returned and never reused.
type fakeEndpointPorter struct {
	mu      sync.Mutex
	nextID  int
	byOrg   map[organizations.OrgID][]organizations.PortedEndpoint
	secrets map[string]string
}

func newFakeEndpointPorter() *fakeEndpointPorter {
	return &fakeEndpointPorter{byOrg: map[organizations.OrgID][]organizations.PortedEndpoint{}, secrets: map[string]string{}}
}

var _ organizations.EndpointPorter = (*fakeEndpointPorter)(nil)

func (f *fakeEndpointPorter) ListEndpoints(_ context.Context, org organizations.OrgID) ([]organizations.PortedEndpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := make([]organizations.PortedEndpoint, len(f.byOrg[org]))
	copy(items, f.byOrg[org])
	return items, nil
}

func (f *fakeEndpointPorter) CreateEndpoint(_ context.Context, org organizations.OrgID, url string, eventTypes []string) (organizations.PortedEndpointSecret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := fmt.Sprintf("wep_test_%d", f.nextID)
	secret := fmt.Sprintf("whsec_test_secret_%d", f.nextID)
	f.byOrg[org] = append(f.byOrg[org], organizations.PortedEndpoint{ID: id, URL: url, EventTypes: eventTypes})
	f.secrets[id] = secret
	return organizations.PortedEndpointSecret{ID: id, Secret: secret}, nil
}

func (f *fakeEndpointPorter) UpdateEndpoint(_ context.Context, org organizations.OrgID, endpointID, url string, eventTypes []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := f.byOrg[org]
	for i := range items {
		if items[i].ID == endpointID {
			items[i].URL = url
			items[i].EventTypes = eventTypes
			return nil
		}
	}
	return fmt.Errorf("fakeEndpointPorter: endpoint %q not found in org %q", endpointID, org)
}

func (f *fakeEndpointPorter) DeleteEndpoint(_ context.Context, org organizations.OrgID, endpointID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := f.byOrg[org]
	for i := range items {
		if items[i].ID == endpointID {
			f.byOrg[org] = append(items[:i:i], items[i+1:]...)
			delete(f.secrets, endpointID)
			return nil
		}
	}
	return fmt.Errorf("fakeEndpointPorter: endpoint %q not found in org %q", endpointID, org)
}

// seed lets a test pre-populate an org's endpoint list directly (bypassing
// CreateEndpoint) when it needs a known id/URL/secret triple without
// exercising the create path itself.
func (f *fakeEndpointPorter) seed(org organizations.OrgID, endpoint organizations.PortedEndpoint, secret string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byOrg[org] = append(f.byOrg[org], endpoint)
	if secret != "" {
		f.secrets[endpoint.ID] = secret
	}
}

func (f *fakeEndpointPorter) allMintedSecrets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	values := make([]string, 0, len(f.secrets))
	for _, secret := range f.secrets {
		values = append(values, secret)
	}
	return values
}

// fakeIntegrationChecker is a minimal stand-in for
// organizations.IntegrationExistenceChecker: existing names the ids this
// fake installation actually has.
type fakeIntegrationChecker struct {
	existing map[string]bool
}

var _ organizations.IntegrationExistenceChecker = fakeIntegrationChecker{}

func (f fakeIntegrationChecker) IntegrationExists(_ context.Context, id string) (bool, error) {
	return f.existing[id], nil
}

// newConfigTestFacade builds an organizations.Facade backed by the in-memory
// Repository, wired with the fake EndpointPorter/IntegrationExistenceChecker
// Slice 9's ExportConfig/ImportConfig need.
func newConfigTestFacade(porter *fakeEndpointPorter, checker fakeIntegrationChecker) *organizations.Facade {
	return memory.NewFacadeWithOverrides(memory.Overrides{}).
		WithEndpointPorter(porter).
		WithIntegrationChecker(checker)
}

// --- ExportConfig ---

func TestExportConfig_ReturnsTheCurrentSchemaVersion(t *testing.T) {
	facade := newConfigTestFacade(newFakeEndpointPorter(), fakeIntegrationChecker{})

	doc, err := facade.ExportConfig(context.Background(), "org_1")
	if err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}
	if doc.SchemaVersion != organizations.CurrentConfigSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", doc.SchemaVersion, organizations.CurrentConfigSchemaVersion)
	}
}

// TestExportConfig_AnUnconfiguredOrgExportsTheContinuityPreservingDefaults is
// PD42's continuity guarantee carried into the export document: an org that
// has never had governance/retention/endpoints configured exports nil
// allow-list, empty hidden/featured, the platform's default cap, no
// endpoints, and both retention windows nil ("inherit").
func TestExportConfig_AnUnconfiguredOrgExportsTheContinuityPreservingDefaults(t *testing.T) {
	facade := newConfigTestFacade(newFakeEndpointPorter(), fakeIntegrationChecker{})

	doc, err := facade.ExportConfig(context.Background(), "org_never_configured")
	if err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}
	if doc.Governance.AllowList != nil {
		t.Errorf("Governance.AllowList = %v, want nil", doc.Governance.AllowList)
	}
	if len(doc.Governance.Hidden) != 0 || len(doc.Governance.Featured) != 0 {
		t.Errorf("Hidden/Featured = %v/%v, want both empty", doc.Governance.Hidden, doc.Governance.Featured)
	}
	if doc.Governance.FeaturedCap != organizations.DefaultFeaturedCap {
		t.Errorf("FeaturedCap = %d, want the platform default %d", doc.Governance.FeaturedCap, organizations.DefaultFeaturedCap)
	}
	if len(doc.Endpoints) != 0 {
		t.Errorf("Endpoints = %v, want empty", doc.Endpoints)
	}
	if doc.Retention.LogRetentionDays != nil || doc.Retention.EventRetentionDays != nil {
		t.Errorf("Retention = %+v, want both nil (inherit)", doc.Retention)
	}
}

// TestExportConfig_ReflectsTheOrgsActualGovernanceEndpointsAndRetention is
// the export's positive shape assertion: whatever an operator has configured
// comes back out exactly.
func TestExportConfig_ReflectsTheOrgsActualGovernanceEndpointsAndRetention(t *testing.T) {
	ctx := context.Background()
	porter := newFakeEndpointPorter()
	facade := newConfigTestFacade(porter, fakeIntegrationChecker{existing: map[string]bool{"intg_real": true}})
	const org organizations.OrgID = "org_configured"

	allowList := []string{"intg_real"}
	if _, err := facade.SetGovernance(ctx, org, organizations.GovernanceUpdate{
		AllowList: &allowList, Hidden: []string{"intg_hidden"}, Featured: []string{"intg_real"}, FeaturedCap: 3,
	}); err != nil {
		t.Fatalf("SetGovernance: %v", err)
	}
	logDays, eventDays := 45, 90
	if _, err := facade.SetRetention(ctx, org, organizations.RetentionUpdate{LogRetentionDays: &logDays, EventRetentionDays: &eventDays}); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	if _, err := porter.CreateEndpoint(ctx, org, "https://example.com/hook", []string{"trigger.fired"}); err != nil {
		t.Fatalf("seed CreateEndpoint: %v", err)
	}

	doc, err := facade.ExportConfig(ctx, org)
	if err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}
	if doc.Governance.AllowList == nil || len(*doc.Governance.AllowList) != 1 || (*doc.Governance.AllowList)[0] != "intg_real" {
		t.Errorf("AllowList = %v, want [intg_real]", doc.Governance.AllowList)
	}
	if len(doc.Governance.Hidden) != 1 || doc.Governance.Hidden[0] != "intg_hidden" {
		t.Errorf("Hidden = %v, want [intg_hidden]", doc.Governance.Hidden)
	}
	if doc.Governance.FeaturedCap != 3 {
		t.Errorf("FeaturedCap = %d, want 3", doc.Governance.FeaturedCap)
	}
	if len(doc.Endpoints) != 1 || doc.Endpoints[0].URL != "https://example.com/hook" {
		t.Fatalf("Endpoints = %+v, want one endpoint at https://example.com/hook", doc.Endpoints)
	}
	if len(doc.Endpoints[0].EventTypes) != 1 || doc.Endpoints[0].EventTypes[0] != "trigger.fired" {
		t.Errorf("Endpoints[0].EventTypes = %v, want [trigger.fired]", doc.Endpoints[0].EventTypes)
	}
	if doc.Retention.LogRetentionDays == nil || *doc.Retention.LogRetentionDays != 45 {
		t.Errorf("LogRetentionDays = %v, want 45", doc.Retention.LogRetentionDays)
	}
	if doc.Retention.EventRetentionDays == nil || *doc.Retention.EventRetentionDays != 90 {
		t.Errorf("EventRetentionDays = %v, want 90", doc.Retention.EventRetentionDays)
	}
}

// TestExportConfig_NeverCarriesAFreshlyMintedEndpointSecretByValueOrFieldName
// is Slice 9's headline security AC proved at the facade layer: an org whose
// endpoint was created with a real freshly minted secret exports a document
// whose marshaled JSON contains neither that raw secret value nor any
// secret-shaped field name at all — ConfigEndpoint structurally carries only
// URL/EventTypes, but this test also guards against a future field added to
// the type without a compile error (e.g. a mistakenly `json`-tagged embed).
func TestExportConfig_NeverCarriesAFreshlyMintedEndpointSecretByValueOrFieldName(t *testing.T) {
	ctx := context.Background()
	porter := newFakeEndpointPorter()
	facade := newConfigTestFacade(porter, fakeIntegrationChecker{})
	const org organizations.OrgID = "org_with_secrets"

	created, err := porter.CreateEndpoint(ctx, org, "https://example.com/hook", nil)
	if err != nil {
		t.Fatalf("seed CreateEndpoint: %v", err)
	}
	if !strings.HasPrefix(created.Secret, "whsec_") {
		t.Fatalf("test setup: seeded secret %q does not look like a real secret", created.Secret)
	}

	doc, err := facade.ExportConfig(ctx, org)
	if err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}
	marshaled, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal ConfigDocument: %v", err)
	}
	body := string(marshaled)

	if strings.Contains(body, created.Secret) {
		t.Fatalf("exported document contains the raw endpoint secret by value: %s", body)
	}
	for _, forbidden := range []string{"secret", "Secret", "whsec_", "credential", "Credential"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("exported document contains the forbidden substring %q: %s", forbidden, body)
		}
	}
}

// --- ImportConfig: schema version gate ---

// TestImportConfig_AnUnsupportedSchemaVersionIsRejectedAndWritesNothing is
// Slice 9's "checked first" AC: schemaVersion is validated before anything
// else runs, whether the request was a dry-run or an apply, and the org's
// existing governance/endpoints are provably untouched afterward.
func TestImportConfig_AnUnsupportedSchemaVersionIsRejectedAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	porter := newFakeEndpointPorter()
	facade := newConfigTestFacade(porter, fakeIntegrationChecker{})
	const org organizations.OrgID = "org_schema_gate"

	allowList := []string{"intg_baseline"}
	if _, err := facade.SetGovernance(ctx, org, organizations.GovernanceUpdate{AllowList: &allowList, FeaturedCap: 8}); err != nil {
		t.Fatalf("baseline SetGovernance: %v", err)
	}
	if _, err := porter.CreateEndpoint(ctx, org, "https://example.com/baseline", nil); err != nil {
		t.Fatalf("baseline CreateEndpoint: %v", err)
	}
	before, err := facade.ExportConfig(ctx, org)
	if err != nil {
		t.Fatalf("baseline ExportConfig: %v", err)
	}

	badDoc := organizations.ConfigDocument{SchemaVersion: 999}
	for _, dryRun := range []bool{true, false} {
		_, err := facade.ImportConfig(ctx, org, badDoc, organizations.ImportOptions{DryRun: dryRun})
		if err == nil {
			t.Fatalf("ImportConfig(dryRun=%v) with schemaVersion=999: want an error, got nil", dryRun)
		}
	}

	after, err := facade.ExportConfig(ctx, org)
	if err != nil {
		t.Fatalf("post-import ExportConfig: %v", err)
	}
	if !reflectiveDocsEqual(t, before, after) {
		t.Errorf("config changed after a rejected import: before=%+v after=%+v", before, after)
	}
}

// --- ImportConfig: dry-run default + unknown-integration-id warnings ---

// TestImportConfig_DryRunWritesNothingAndReturnsThePlanItWouldApply is the
// "never blind" AC: a dry-run reports the plan it would apply but leaves the
// org's actual governance/endpoints untouched.
func TestImportConfig_DryRunWritesNothingAndReturnsThePlanItWouldApply(t *testing.T) {
	ctx := context.Background()
	porter := newFakeEndpointPorter()
	facade := newConfigTestFacade(porter, fakeIntegrationChecker{existing: map[string]bool{}})
	const org organizations.OrgID = "org_dry_run"

	before, err := facade.ExportConfig(ctx, org)
	if err != nil {
		t.Fatalf("baseline ExportConfig: %v", err)
	}

	allowList := []string{"intg_new"}
	doc := organizations.ConfigDocument{
		SchemaVersion: organizations.CurrentConfigSchemaVersion,
		Governance:    organizations.ConfigGovernance{AllowList: &allowList, FeaturedCap: 8},
		Endpoints:     []organizations.ConfigEndpoint{{URL: "https://example.com/new-hook"}},
	}

	result, err := facade.ImportConfig(ctx, org, doc, organizations.ImportOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ImportConfig (dry-run): %v", err)
	}
	if len(result.Plan) == 0 {
		t.Error("Plan is empty, want it to describe the governance/endpoint changes a dry-run would apply")
	}
	if len(result.Applied) != 0 || len(result.Secrets) != 0 {
		t.Errorf("a dry-run populated Applied/Secrets: %+v/%+v, want both empty", result.Applied, result.Secrets)
	}

	after, err := facade.ExportConfig(ctx, org)
	if err != nil {
		t.Fatalf("post-dry-run ExportConfig: %v", err)
	}
	if !reflectiveDocsEqual(t, before, after) {
		t.Errorf("a dry-run changed the org's config: before=%+v after=%+v", before, after)
	}
}

// TestImportConfig_DryRunFlagsAnIntegrationIDThatDoesNotExistInThisInstallation
// is the AC's other half: a governance reference to an id this installation
// doesn't have is reported, not silently dropped, and still writes nothing.
func TestImportConfig_DryRunFlagsAnIntegrationIDThatDoesNotExistInThisInstallation(t *testing.T) {
	ctx := context.Background()
	porter := newFakeEndpointPorter()
	facade := newConfigTestFacade(porter, fakeIntegrationChecker{existing: map[string]bool{"intg_real": true}})
	const org organizations.OrgID = "org_unknown_id"

	allowList := []string{"intg_real", "intg_totally_bogus"}
	doc := organizations.ConfigDocument{
		SchemaVersion: organizations.CurrentConfigSchemaVersion,
		Governance:    organizations.ConfigGovernance{AllowList: &allowList, FeaturedCap: 8},
	}

	result, err := facade.ImportConfig(ctx, org, doc, organizations.ImportOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ImportConfig (dry-run): %v", err)
	}
	found := false
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "intg_totally_bogus") {
			found = true
		}
		if strings.Contains(warning, "intg_real") {
			t.Errorf("warning %q names the KNOWN integration id intg_real, want only the unknown one flagged", warning)
		}
	}
	if !found {
		t.Errorf("Warnings = %v, want one naming intg_totally_bogus", result.Warnings)
	}

	governance, err := facade.GetGovernance(ctx, org)
	if err != nil {
		t.Fatalf("GetGovernance: %v", err)
	}
	if governance.AllowList != nil {
		t.Errorf("AllowList = %v after a dry-run, want nil (unwritten)", governance.AllowList)
	}
}

// --- ImportConfig: merge mode ---

// TestImportConfig_MergeUpsertsMentionedSettingsAndLeavesUnmentionedEndpointsAlone
// is merge's headline AC: fields the document sets are upserted, a field it
// doesn't mention (Hidden, here) is untouched, an existing endpoint the
// document never names is left exactly as it is (no delete), and a new URL
// is created.
func TestImportConfig_MergeUpsertsMentionedSettingsAndLeavesUnmentionedEndpointsAlone(t *testing.T) {
	ctx := context.Background()
	porter := newFakeEndpointPorter()
	facade := newConfigTestFacade(porter, fakeIntegrationChecker{existing: map[string]bool{"intg_new": true}})
	const org organizations.OrgID = "org_merge"

	if _, err := facade.SetGovernance(ctx, org, organizations.GovernanceUpdate{Hidden: []string{"intg_stays_hidden"}, FeaturedCap: 8}); err != nil {
		t.Fatalf("baseline SetGovernance: %v", err)
	}
	if _, err := porter.CreateEndpoint(ctx, org, "https://example.com/untouched", nil); err != nil {
		t.Fatalf("baseline CreateEndpoint: %v", err)
	}

	allowList := []string{"intg_new"}
	doc := organizations.ConfigDocument{
		SchemaVersion: organizations.CurrentConfigSchemaVersion,
		Governance:    organizations.ConfigGovernance{AllowList: &allowList},
		Endpoints:     []organizations.ConfigEndpoint{{URL: "https://example.com/created-by-merge"}},
	}

	result, err := facade.ImportConfig(ctx, org, doc, organizations.ImportOptions{DryRun: false, Mode: organizations.ImportModeMerge})
	if err != nil {
		t.Fatalf("ImportConfig (merge apply): %v", err)
	}
	if len(result.Secrets) != 1 {
		t.Fatalf("Secrets = %+v, want exactly one freshly minted secret for the created endpoint", result.Secrets)
	}

	governance, err := facade.GetGovernance(ctx, org)
	if err != nil {
		t.Fatalf("GetGovernance: %v", err)
	}
	if governance.AllowList == nil || len(*governance.AllowList) != 1 || (*governance.AllowList)[0] != "intg_new" {
		t.Errorf("AllowList = %v, want [intg_new] (the doc's mentioned field upserted)", governance.AllowList)
	}
	if len(governance.Hidden) != 1 || governance.Hidden[0] != "intg_stays_hidden" {
		t.Errorf("Hidden = %v, want [intg_stays_hidden] (a field merge's doc never mentioned must be untouched)", governance.Hidden)
	}

	endpoints, err := porter.ListEndpoints(ctx, org)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	urls := map[string]bool{}
	for _, endpoint := range endpoints {
		urls[endpoint.URL] = true
	}
	if len(endpoints) != 2 {
		t.Fatalf("len(endpoints) = %d, want 2 (the untouched pre-existing one plus the newly created one); got %+v", len(endpoints), endpoints)
	}
	if !urls["https://example.com/untouched"] {
		t.Error("the pre-existing endpoint the doc never mentioned was removed — merge must never delete")
	}
	if !urls["https://example.com/created-by-merge"] {
		t.Error("the doc's new endpoint was not created")
	}
}

// --- ImportConfig: replace mode ---

// TestImportConfig_ReplaceDeletesAnEndpointAbsentFromTheDocumentAndFullyReplacesGovernance
// is replace's headline AC: an existing endpoint whose URL the document
// omits is deleted, and governance matches the document exactly (including
// clearing a field the document leaves empty, unlike merge).
func TestImportConfig_ReplaceDeletesAnEndpointAbsentFromTheDocumentAndFullyReplacesGovernance(t *testing.T) {
	ctx := context.Background()
	porter := newFakeEndpointPorter()
	facade := newConfigTestFacade(porter, fakeIntegrationChecker{existing: map[string]bool{"intg_kept": true}})
	const org organizations.OrgID = "org_replace"

	if _, err := facade.SetGovernance(ctx, org, organizations.GovernanceUpdate{Hidden: []string{"intg_should_be_cleared"}, FeaturedCap: 8}); err != nil {
		t.Fatalf("baseline SetGovernance: %v", err)
	}
	keptEndpoint, err := porter.CreateEndpoint(ctx, org, "https://example.com/keep-me", nil)
	if err != nil {
		t.Fatalf("baseline CreateEndpoint (kept): %v", err)
	}
	if _, err := porter.CreateEndpoint(ctx, org, "https://example.com/delete-me", nil); err != nil {
		t.Fatalf("baseline CreateEndpoint (to delete): %v", err)
	}

	allowList := []string{"intg_kept"}
	doc := organizations.ConfigDocument{
		SchemaVersion: organizations.CurrentConfigSchemaVersion,
		Governance:    organizations.ConfigGovernance{AllowList: &allowList}, // Hidden/Featured absent -> cleared under replace
		Endpoints:     []organizations.ConfigEndpoint{{URL: "https://example.com/keep-me", EventTypes: []string{"trigger.fired"}}},
	}

	if _, err := facade.ImportConfig(ctx, org, doc, organizations.ImportOptions{DryRun: false, Mode: organizations.ImportModeReplace}); err != nil {
		t.Fatalf("ImportConfig (replace apply): %v", err)
	}

	endpoints, err := porter.ListEndpoints(ctx, org)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("len(endpoints) = %d, want 1 (the doc-absent endpoint deleted); got %+v", len(endpoints), endpoints)
	}
	if endpoints[0].ID != keptEndpoint.ID || endpoints[0].URL != "https://example.com/keep-me" {
		t.Errorf("surviving endpoint = %+v, want the kept one, updated to the doc's own filter", endpoints[0])
	}
	if len(endpoints[0].EventTypes) != 1 || endpoints[0].EventTypes[0] != "trigger.fired" {
		t.Errorf("surviving endpoint's EventTypes = %v, want [trigger.fired] (replace also updates what it keeps)", endpoints[0].EventTypes)
	}

	governance, err := facade.GetGovernance(ctx, org)
	if err != nil {
		t.Fatalf("GetGovernance: %v", err)
	}
	if len(governance.Hidden) != 0 {
		t.Errorf("Hidden = %v, want cleared — replace must match the document exactly, including removing what it omits", governance.Hidden)
	}
	if governance.AllowList == nil || len(*governance.AllowList) != 1 || (*governance.AllowList)[0] != "intg_kept" {
		t.Errorf("AllowList = %v, want [intg_kept]", governance.AllowList)
	}
}

// TestImportConfig_ReplaceFullyReplacesRetentionIndependentlyOfGovernance
// pins the "other half" nuance: a replace import that sets both governance
// and retention lands both, in full — SetGovernance/SetRetention each
// individually preserve the *other* half from current state (their
// pre-existing per-field contract), but ImportConfig's own apply calls both
// in the same request, so neither half reverts the other back to its
// pre-import value.
func TestImportConfig_ReplaceFullyReplacesRetentionIndependentlyOfGovernance(t *testing.T) {
	ctx := context.Background()
	porter := newFakeEndpointPorter()
	facade := newConfigTestFacade(porter, fakeIntegrationChecker{})
	const org organizations.OrgID = "org_replace_retention"

	oldLogDays := 10
	if _, err := facade.SetRetention(ctx, org, organizations.RetentionUpdate{LogRetentionDays: &oldLogDays}); err != nil {
		t.Fatalf("baseline SetRetention: %v", err)
	}

	newLogDays, newEventDays := 60, 120
	doc := organizations.ConfigDocument{
		SchemaVersion: organizations.CurrentConfigSchemaVersion,
		Governance:    organizations.ConfigGovernance{Hidden: []string{"intg_x"}},
		Retention:     organizations.ConfigRetention{LogRetentionDays: &newLogDays, EventRetentionDays: &newEventDays},
	}
	if _, err := facade.ImportConfig(ctx, org, doc, organizations.ImportOptions{DryRun: false, Mode: organizations.ImportModeReplace}); err != nil {
		t.Fatalf("ImportConfig (replace apply): %v", err)
	}

	retention, err := facade.GetRetention(ctx, org)
	if err != nil {
		t.Fatalf("GetRetention: %v", err)
	}
	if retention.LogRetentionDays == nil || *retention.LogRetentionDays != 60 {
		t.Errorf("LogRetentionDays = %v, want 60", retention.LogRetentionDays)
	}
	if retention.EventRetentionDays == nil || *retention.EventRetentionDays != 120 {
		t.Errorf("EventRetentionDays = %v, want 120", retention.EventRetentionDays)
	}
	governance, err := facade.GetGovernance(ctx, org)
	if err != nil {
		t.Fatalf("GetGovernance: %v", err)
	}
	if len(governance.Hidden) != 1 || governance.Hidden[0] != "intg_x" {
		t.Errorf("Hidden = %v, want [intg_x] — the governance half applied in the same replace must not be lost", governance.Hidden)
	}
}

// --- ImportConfig: fresh secrets on create ---

// TestImportConfig_ApplyMintsAFreshSecretPerCreatedEndpointNeverReusingAnExistingOne
// is Slice 9's secret-minting AC: every endpoint an apply creates gets its
// own freshly minted secret (never present anywhere in the import document,
// which structurally cannot carry one, and never equal to any other
// endpoint's own secret).
func TestImportConfig_ApplyMintsAFreshSecretPerCreatedEndpointNeverReusingAnExistingOne(t *testing.T) {
	ctx := context.Background()
	porter := newFakeEndpointPorter()
	facade := newConfigTestFacade(porter, fakeIntegrationChecker{})
	const org organizations.OrgID = "org_fresh_secrets"

	preExisting, err := porter.CreateEndpoint(ctx, org, "https://example.com/already-there", nil)
	if err != nil {
		t.Fatalf("baseline CreateEndpoint: %v", err)
	}

	doc := organizations.ConfigDocument{
		SchemaVersion: organizations.CurrentConfigSchemaVersion,
		Endpoints: []organizations.ConfigEndpoint{
			{URL: "https://example.com/already-there"}, // existing URL -> no secret minted
			{URL: "https://example.com/brand-new-one"},
			{URL: "https://example.com/brand-new-two"},
		},
	}

	result, err := facade.ImportConfig(ctx, org, doc, organizations.ImportOptions{DryRun: false, Mode: organizations.ImportModeMerge})
	if err != nil {
		t.Fatalf("ImportConfig (merge apply): %v", err)
	}
	if len(result.Secrets) != 2 {
		t.Fatalf("Secrets = %+v, want exactly 2 (one per newly created endpoint, none for the already-existing URL)", result.Secrets)
	}
	seen := map[string]bool{}
	for _, minted := range result.Secrets {
		if minted.Secret == "" {
			t.Error("a minted secret was empty")
		}
		if minted.Secret == preExisting.Secret {
			t.Errorf("minted secret %q reuses the pre-existing endpoint's own secret", minted.Secret)
		}
		if seen[minted.Secret] {
			t.Errorf("minted secret %q was returned more than once", minted.Secret)
		}
		seen[minted.Secret] = true
	}
	// Cross-check against the porter's own bookkeeping: every secret it ever
	// minted for this org is distinct.
	all := porter.allMintedSecrets()
	uniqueAll := map[string]bool{}
	for _, secret := range all {
		if uniqueAll[secret] {
			t.Errorf("the porter minted the same secret twice: %q", secret)
		}
		uniqueAll[secret] = true
	}
}

// reflectiveDocsEqual compares two ConfigDocuments by their JSON
// representation — simplest way to assert "config did not change" without
// hand-rolling a deep-equal for every nested slice/pointer field.
func reflectiveDocsEqual(t *testing.T, a, b organizations.ConfigDocument) bool {
	t.Helper()
	aJSON, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	bJSON, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	return string(aJSON) == string(bJSON)
}
