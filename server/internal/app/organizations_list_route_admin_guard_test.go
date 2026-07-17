package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"beecon/internal/config"
	"beecon/internal/db"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
	memory "beecon/internal/organizations/driven/memory"
	orgshttp "beecon/internal/organizations/driving/httpapi"
)

const orgsListTestAdminKey = "test-admin-key"

// newOrganizationsListRouter builds the real router with a real
// organizations handler (backed by the in-memory facade) mounted exactly as
// app/router.go wires it — GET /api/v1/organizations behind AdminAuth —
// while every other handler stays nil, mirroring
// connect_routes_bypass_request_logging_test.go's own convention of only
// wiring the handler under test. Returning the facade too lets a test seed
// organizations directly rather than through the same route it's testing.
func newOrganizationsListRouter(t *testing.T) (http.Handler, *organizations.Facade) {
	t.Helper()
	database, err := db.New(config.DriverSQLite, "file:organizations_list_route_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	orgFacade := memory.NewFacadeWithOverrides(memory.Overrides{})
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	organizationsHandler := orgshttp.NewHandler(orgFacade, errorRenderer)

	cfg := &config.Config{AdminAPIKey: orgsListTestAdminKey}
	noOperatorsYet := func(context.Context) (bool, error) { return false, nil }
	router := buildRouter(
		cfg,
		database,
		organizationsHandler,
		nil,            // accessHandler
		nil,            // catalogHandler
		nil,            // registryHandler
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
	return router, orgFacade
}

func doOrganizationsListRequest(router http.Handler, authorizationHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	if authorizationHeader != "" {
		req.Header.Set("Authorization", authorizationHeader)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestOrganizationsListRoute_RejectsARequestWithNoAdminKey(t *testing.T) {
	router, _ := newOrganizationsListRouter(t)

	w := doOrganizationsListRequest(router, "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestOrganizationsListRoute_RejectsAWrongAdminKey(t *testing.T) {
	router, _ := newOrganizationsListRouter(t)

	w := doOrganizationsListRequest(router, "Bearer wrong-key")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestOrganizationsListRoute_Returns200WithTheCursorPaginatedPageShape pins
// the exact contract the Admin UI's organizations feature (Slice 1) reads:
// GET /api/v1/organizations, behind the admin key, returns the
// {items, nextCursor} envelope with every previously created organization.
func TestOrganizationsListRoute_Returns200WithTheCursorPaginatedPageShape(t *testing.T) {
	router, facade := newOrganizationsListRouter(t)
	seedOrg(t, facade, "Acme")
	seedOrg(t, facade, "Globex")

	w := doOrganizationsListRequest(router, "Bearer "+orgsListTestAdminKey)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page struct {
		Items []struct {
			ID                  string   `json:"id"`
			Name                string   `json:"name"`
			AllowedRedirectUris []string `json:"allowedRedirectUris"`
			CreatedAt           string   `json:"createdAt"`
		} `json:"items"`
		NextCursor string `json:"nextCursor,omitempty"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(page.Items))
	}
	if page.Items[0].Name != "Globex" || page.Items[1].Name != "Acme" {
		t.Fatalf("names = [%q, %q], want [%q, %q] (newest-created first)", page.Items[0].Name, page.Items[1].Name, "Globex", "Acme")
	}
	if page.Items[0].CreatedAt == "" {
		t.Error("createdAt is empty, want a timestamp")
	}
	if page.NextCursor != "" {
		t.Errorf("nextCursor = %q, want empty on a single, complete page", page.NextCursor)
	}
}

func seedOrg(t *testing.T, facade *organizations.Facade, name string) organizations.Organization {
	t.Helper()
	org, err := facade.Create(context.Background(), name)
	if err != nil {
		t.Fatalf("seed organization %q: %v", name, err)
	}
	return org
}
