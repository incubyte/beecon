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
	"beecon/internal/registrybundle"
)

const registryReviewTestAdminKey = "test-admin-key"

func registryReviewFixture() []catalog.ProviderDefinition {
	return []catalog.ProviderDefinition{
		{
			Slug: "outlook", Name: "Outlook", Logo: "https://static.beecon.dev/providers/outlook.png", AuthScheme: "oauth2",
			AuthorizeURL: "https://example.com/authorize", TokenURL: "https://example.com/token", Scopes: []string{"Mail.Read"},
		},
	}
}

// registryReviewBundle's ContentHash is the real registrybundle.ContentHash
// of the rest of its own fields — not left empty — because Activate (used
// as setup in the happy-path tests below) now verifies it (Slice 4, PD67)
// exactly as a real registry-pulled bundle's would be.
func registryReviewBundle(version string) registrybundle.Bundle {
	bundle := registrybundle.Bundle{
		FormatVersion: 1,
		ProviderSlug:  "outlook",
		Version:       version,
		Name:          "Outlook",
		AuthScheme:    "oauth2",
		OAuth: registrybundle.OAuthConfig{
			AuthorizeURL: "https://example.com/authorize",
			TokenURL:     "https://example.com/token",
		},
		Tools: []registrybundle.Tool{
			{
				Slug: "outlook-list-messages", Name: "List messages",
				InputSchema: map[string]any{"type": "object"}, OutputSchema: map[string]any{"type": "object"},
				Mapping: registrybundle.ToolMapping{Method: "GET", Path: "/v1.0/me/messages"},
			},
		},
	}
	bundle.ContentHash, _ = registrybundle.ContentHash(bundle)
	return bundle
}

// newRegistryReviewRouter builds the real router with a real
// cataloghttp.RegistryHandler (backed by the in-memory facade, wired with a
// seedable in-memory RegistryClient) mounted exactly as app/router.go wires
// it — GET /api/v1/registry/providers/{slug}/versions and .../diff behind
// ConsoleAuth — while every other handler stays nil, mirroring
// newProviderDefinitionsRouter's own convention. Returning the seedable
// RegistryClient and the catalog.Facade lets a test publish versions and
// activate one before exercising the routes.
func newRegistryReviewRouter(t *testing.T) (http.Handler, *memory.RegistryClient, *catalog.Facade) {
	t.Helper()
	database, err := db.New(config.DriverSQLite, "file:registry_review_route_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	registryClient := memory.NewRegistryClient()
	catalogFacade, err := memory.NewFacadeWithOverrides(memory.Overrides{
		Definitions:          registryReviewFixture(),
		RegistryClient:       registryClient,
		ActivatedDefinitions: memory.NewActivatedDefinitionRepository(),
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	registryHandler := cataloghttp.NewRegistryHandler(catalogFacade, errorRenderer)

	cfg := &config.Config{AdminAPIKey: registryReviewTestAdminKey}
	noOperatorsYet := func(context.Context) (bool, error) { return false, nil }
	router := buildRouter(
		cfg,
		database,
		nil, // organizationsHandler
		nil, // accessHandler
		nil, // catalogHandler
		registryHandler,
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
	return router, registryClient, catalogFacade
}

func doRegistryReviewRequest(router http.Handler, path, authorizationHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authorizationHeader != "" {
		req.Header.Set("Authorization", authorizationHeader)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestRegistryVersionsRoute_RejectsARequestWithNoCredential(t *testing.T) {
	router, registryClient, _ := newRegistryReviewRouter(t)
	registryClient.Seed("outlook", registryReviewBundle("1.0.0"))

	w := doRegistryReviewRequest(router, "/api/v1/registry/providers/outlook/versions", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestRegistryDiffRoute_RejectsARequestWithNoCredential(t *testing.T) {
	router, registryClient, _ := newRegistryReviewRouter(t)
	registryClient.Seed("outlook", registryReviewBundle("1.0.0"))

	w := doRegistryReviewRequest(router, "/api/v1/registry/providers/outlook/diff?to=1.0.0", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestRegistryDiffRoute_AMissingToQueryParamIsRejectedWithAClearValidationError
// pins the diff route's own required-query-param check: unlike the version
// list, a diff target is meaningless without a `to` version.
func TestRegistryDiffRoute_AMissingToQueryParamIsRejectedWithAClearValidationError(t *testing.T) {
	router, registryClient, _ := newRegistryReviewRouter(t)
	registryClient.Seed("outlook", registryReviewBundle("1.0.0"))

	w := doRegistryReviewRequest(router, "/api/v1/registry/providers/outlook/diff", "Bearer "+registryReviewTestAdminKey)

	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("status = %d, want a 4xx for a missing `to` query param; body=%s", w.Code, w.Body.String())
	}
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v; body=%s", err, w.Body.String())
	}
	if env.Error.Code != catalog.CodeValidationFailed {
		t.Errorf("error.code = %q, want %q", env.Error.Code, catalog.CodeValidationFailed)
	}
}

// TestRegistryVersionsRoute_HappyPathReturnsTheVersionListWithTheActiveFlagSet
// proves the version-list route is actually reachable through the real
// ConsoleAuth-guarded router and returns the documented DTO shape (API
// Shape: {items:[{version, active}], activeVersion}).
func TestRegistryVersionsRoute_HappyPathReturnsTheVersionListWithTheActiveFlagSet(t *testing.T) {
	router, registryClient, catalogFacade := newRegistryReviewRouter(t)
	registryClient.Seed("outlook", registryReviewBundle("1.0.0"))
	registryClient.Seed("outlook", registryReviewBundle("1.1.0"))
	if _, err := catalogFacade.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	w := doRegistryReviewRequest(router, "/api/v1/registry/providers/outlook/versions", "Bearer "+registryReviewTestAdminKey)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page struct {
		Items []struct {
			Version string `json:"version"`
			Active  bool   `json:"active"`
		} `json:"items"`
		ActiveVersion string `json:"activeVersion"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if page.ActiveVersion != "1.0.0" {
		t.Errorf("activeVersion = %q, want %q", page.ActiveVersion, "1.0.0")
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %+v, want both seeded versions", page.Items)
	}
	for _, item := range page.Items {
		wantActive := item.Version == "1.0.0"
		if item.Active != wantActive {
			t.Errorf("item %+v: active = %v, want %v", item, item.Active, wantActive)
		}
	}
}

// TestRegistryDiffRoute_HappyPathReturnsTheAddedChangedRemovedDTOShape proves
// the diff route is reachable through the real router and returns the
// documented DTO shape (API Shape: {from, to, added, changed, removed}).
func TestRegistryDiffRoute_HappyPathReturnsTheAddedChangedRemovedDTOShape(t *testing.T) {
	router, registryClient, catalogFacade := newRegistryReviewRouter(t)
	registryClient.Seed("outlook", registryReviewBundle("1.0.0"))
	target := registryReviewBundle("1.1.0")
	target.Tools = append(target.Tools, registrybundle.Tool{
		Slug: "outlook-get-message", Name: "Get message",
		InputSchema: map[string]any{"type": "object"}, OutputSchema: map[string]any{"type": "object"},
		Mapping: registrybundle.ToolMapping{Method: "GET", Path: "/v1.0/me/messages/{id}"},
	})
	registryClient.Seed("outlook", target)
	if _, err := catalogFacade.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	w := doRegistryReviewRequest(router, "/api/v1/registry/providers/outlook/diff?to=1.1.0", "Bearer "+registryReviewTestAdminKey)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var diff struct {
		From    string `json:"from"`
		To      string `json:"to"`
		Added   struct {
			Tools []string `json:"tools"`
		} `json:"added"`
		Changed struct {
			Tools []string `json:"tools"`
		} `json:"changed"`
		Removed struct {
			Tools []string `json:"tools"`
		} `json:"removed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &diff); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if diff.From != "1.0.0" || diff.To != "1.1.0" {
		t.Errorf("from/to = %q/%q, want 1.0.0/1.1.0", diff.From, diff.To)
	}
	if len(diff.Added.Tools) != 1 || diff.Added.Tools[0] != "outlook-get-message" {
		t.Errorf("added.tools = %v, want [outlook-get-message]", diff.Added.Tools)
	}
	if len(diff.Changed.Tools) != 0 || len(diff.Removed.Tools) != 0 {
		t.Errorf("changed/removed must be empty: changed=%v removed=%v", diff.Changed.Tools, diff.Removed.Tools)
	}
}

func TestRegistryVersionsRoute_AnUnseededProviderReturnsAnEmptyVersionListNotAnError(t *testing.T) {
	router, _, _ := newRegistryReviewRouter(t)

	w := doRegistryReviewRequest(router, "/api/v1/registry/providers/never-seeded/versions", "Bearer "+registryReviewTestAdminKey)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (an unseeded provider is an empty version list, not an error); body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Items) != 0 {
		t.Errorf("items = %+v, want empty for an unseeded provider", page.Items)
	}
}
