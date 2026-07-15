package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"beecon/internal/catalog"
	memory "beecon/internal/catalog/driven/memory"
	cataloghttp "beecon/internal/catalog/driving/httpapi"
	"beecon/internal/config"
	"beecon/internal/db"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
	orgsmemory "beecon/internal/organizations/driven/memory"
)

const providerDefinitionsTestAdminKey = "test-admin-key"

func providerDefinitionsFixture() []catalog.ProviderDefinition {
	return []catalog.ProviderDefinition{
		{
			Slug: "outlook", Name: "Outlook", Logo: "https://static.beecon.dev/providers/outlook.png", AuthScheme: "oauth2",
			AuthorizeURL: "https://example.com/authorize", TokenURL: "https://example.com/token", Scopes: []string{"Mail.Read"},
		},
	}
}

// newProviderDefinitionsRouter builds the real router with a real catalog
// handler (backed by the in-memory facade, seeded with providerDefinitionsFixture)
// mounted exactly as app/router.go wires it — GET /api/v1/provider-definitions
// (+ /{slug}) behind AdminAuth, installation-wide, no orgId anywhere — while
// every other handler stays nil, mirroring newOrganizationsListRouter's own
// convention (organizations_list_route_admin_guard_test.go). Returning the
// organizations.Facade too lets a test set up an org's governance directly,
// to prove that path is irrelevant to this route (AC7).
func newProviderDefinitionsRouter(t *testing.T) (http.Handler, *organizations.Facade) {
	t.Helper()
	database, err := db.New(config.DriverSQLite, "file:provider_definitions_route_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	orgsFacade := orgsmemory.NewFacadeWithOverrides(orgsmemory.Overrides{})
	catalogFacade, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: providerDefinitionsFixture(), Governance: orgsFacade})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	catalogHandler := cataloghttp.NewHandler(catalogFacade, errorRenderer)

	cfg := &config.Config{AdminAPIKey: providerDefinitionsTestAdminKey}
	noOperatorsYet := func(context.Context) (bool, error) { return false, nil }
	router := buildRouter(
		cfg,
		database,
		nil, // organizationsHandler
		nil, // accessHandler
		catalogHandler,
		nil,            // connectionsHandler
		nil,            // connectWebHandler
		nil,            // adminUIHandler
		nil,            // executionHandler
		nil,            // filesHandler
		nil,            // loggingHandler
		nil,            // triggersHandler
		nil,            // deliveryHandler
		nil,            // operatorHandler
		nil,            // metricsHandler
		nil,            // dashboardMetricsHandler
		nil,            // verifyOrgKey
		nil,            // verifyUserToken
		nil,            // verifySession
		noOperatorsYet, // operatorsExist
		nil,            // logger
	)
	return router, orgsFacade
}

func doProviderDefinitionsRequest(router http.Handler, path, authorizationHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authorizationHeader != "" {
		req.Header.Set("Authorization", authorizationHeader)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestProviderDefinitionsRoute_RejectsARequestWithNoAdminKey(t *testing.T) {
	router, _ := newProviderDefinitionsRouter(t)

	w := doProviderDefinitionsRequest(router, "/api/v1/provider-definitions", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestProviderDefinitionsRoute_RejectsAWrongAdminKey(t *testing.T) {
	router, _ := newProviderDefinitionsRouter(t)

	w := doProviderDefinitionsRequest(router, "/api/v1/provider-definitions", "Bearer wrong-key")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestProviderDefinitionDetailRoute_RejectsARequestWithNoAdminKey(t *testing.T) {
	router, _ := newProviderDefinitionsRouter(t)

	w := doProviderDefinitionsRequest(router, "/api/v1/provider-definitions/outlook", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestProviderDefinitionsRoute_IsNotNestedUnderAnOrganizationPath pins that
// the provider-definitions read is installation-wide (architecture doc
// §3.1): unlike connections/trigger-instances/logs/events/users/governance,
// it has no /organizations/{orgId}/... form at all — requesting one 404s
// rather than silently scoping to that org. Mirrors
// TestDashboardMetricsRoute_IsNotNestedUnderAnOrganizationPath.
func TestProviderDefinitionsRoute_IsNotNestedUnderAnOrganizationPath(t *testing.T) {
	router, _ := newProviderDefinitionsRouter(t)

	w := doProviderDefinitionsRequest(router, "/api/v1/organizations/org_whatever/provider-definitions", "Bearer "+providerDefinitionsTestAdminKey)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d — provider-definitions must not have an org-scoped path form", w.Code, http.StatusNotFound)
	}
}

// TestProviderDefinitionsRoute_ReturnsEveryLoadedDefinitionEvenWhenAnOrgsGovernanceHidesItsOnlyIntegration
// is the CRITICAL AC7 proof at the full-router/HTTP level (the facade-level
// proof lives in
// internal/catalog/provider_definitions_test.go's
// TestListProviderDefinitions_IsNotFilteredByAnOrgsRestrictiveGovernance):
// even with an organization's governance configured to hide every
// integration it has, the admin-guarded, installation-wide
// GET /api/v1/provider-definitions still returns the real installed estate,
// unfiltered — because this route takes no orgId at all and never consults
// GovernanceReader.
func TestProviderDefinitionsRoute_ReturnsEveryLoadedDefinitionEvenWhenAnOrgsGovernanceHidesItsOnlyIntegration(t *testing.T) {
	router, orgsFacade := newProviderDefinitionsRouter(t)
	org, err := orgsFacade.Create(context.Background(), "Restricted Co")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if _, err := orgsFacade.SetGovernance(context.Background(), org.ID, organizations.GovernanceUpdate{AllowList: &[]string{}}); err != nil {
		t.Fatalf("SetGovernance: %v", err)
	}

	w := doProviderDefinitionsRequest(router, "/api/v1/provider-definitions", "Bearer "+providerDefinitionsTestAdminKey)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page struct {
		Items []struct {
			Slug string `json:"slug"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Items) != 1 || page.Items[0].Slug != "outlook" {
		t.Fatalf("items = %+v, want the outlook provider definition present despite %s's empty-allow-list governance", page.Items, org.ID)
	}
}
