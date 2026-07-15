package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"beecon/internal/config"
	"beecon/internal/db"
	"beecon/internal/httpx"
	memory "beecon/internal/organizations/driven/memory"
	orgshttp "beecon/internal/organizations/driving/httpapi"
)

const singleOrgRouteTestAdminKey = "test-admin-key"

// newSingleOrgRouteRouter mirrors newOrganizationsListRouter
// (organizations_list_route_admin_guard_test.go): the real router, a real
// organizations handler backed by the in-memory facade, every other handler
// left nil.
func newSingleOrgRouteRouter(t *testing.T) http.Handler {
	t.Helper()
	database, err := db.New(config.DriverSQLite, "file:single_org_route_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	orgFacade := memory.NewFacadeWithOverrides(memory.Overrides{})
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	organizationsHandler := orgshttp.NewHandler(orgFacade, errorRenderer)

	cfg := &config.Config{AdminAPIKey: singleOrgRouteTestAdminKey}
	noOperatorsYet := func(context.Context) (bool, error) { return false, nil }
	return buildRouter(
		cfg,
		database,
		organizationsHandler,
		nil,            // accessHandler
		nil,            // catalogHandler
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
}

// TestBuildRouter_SingleOrganizationGetAndPatchStillWorkAfterTheAdminConsoleMount
// is a regression pin discovered while testing Slice 2: router.go mounts the
// Admin UI's new r.Route("/{orgId}", ...) subrouter (for /connections and
// /trigger-instances, guarded by the console mount) inside the very same
// r.Route("/organizations", ...) block that already registers direct leaf
// handlers on the identical pattern — r.Get("/{orgId}", ...) and
// r.Patch("/{orgId}", ...), i.e. the pre-existing (Phase 1)
// GET/PATCH-single-organization endpoints. chi cannot serve a leaf handler
// and a mounted subrouter on the same pattern node: the moment the new
// subrouter mount exists, both pre-existing routes 404 — this is a
// regression the Slice 2 router change introduces, not a testability gap
// this file works around, and it fails right now on the current
// router.go. The console-mounted /connections and /trigger-instances
// routes underneath are unaffected (a different, deeper node); only the
// exact "/{orgId}" leaf breaks.
func TestBuildRouter_SingleOrganizationGetAndPatchStillWorkAfterTheAdminConsoleMount(t *testing.T) {
	router := newSingleOrgRouteRouter(t)
	adminAuth := "Bearer " + singleOrgRouteTestAdminKey

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/organizations/", strings.NewReader(`{"name":"Acme"}`))
	createReq.Header.Set("Authorization", adminAuth)
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", createW.Code, http.StatusCreated, createW.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createW.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created org: %v", err)
	}

	t.Run("GET /api/v1/organizations/{orgId} still returns the organization", func(t *testing.T) {
		getReq := httptest.NewRequest(http.MethodGet, "/api/v1/organizations/"+created.ID, nil)
		getReq.Header.Set("Authorization", adminAuth)
		getW := httptest.NewRecorder()
		router.ServeHTTP(getW, getReq)
		if getW.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s — the new admin-console r.Route(\"/{orgId}\", ...) subrouter mount has broken the pre-existing r.Get(\"/{orgId}\", ...) leaf route", getW.Code, http.StatusOK, getW.Body.String())
		}
	})

	t.Run("PATCH /api/v1/organizations/{orgId} still updates the organization's allowed redirect URIs", func(t *testing.T) {
		patchReq := httptest.NewRequest(http.MethodPatch, "/api/v1/organizations/"+created.ID, strings.NewReader(`{"allowedRedirectUris":["https://example.com/callback"]}`))
		patchReq.Header.Set("Authorization", adminAuth)
		patchW := httptest.NewRecorder()
		router.ServeHTTP(patchW, patchReq)
		if patchW.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s — the new admin-console r.Route(\"/{orgId}\", ...) subrouter mount has broken the pre-existing r.Patch(\"/{orgId}\", ...) leaf route", patchW.Code, http.StatusOK, patchW.Body.String())
		}
	})
}
