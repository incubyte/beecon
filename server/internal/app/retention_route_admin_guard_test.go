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

const retentionRouteTestAdminKey = "test-admin-key"

// newRetentionRouteRouter builds the real router with a real organizations
// handler (backed by the in-memory facade) mounted exactly as app/router.go
// wires it — GET/PUT /api/v1/organizations/{orgId}/retention behind
// AdminAuth plus InjectOrgFromPath, the same console mount governance's own
// route already uses — mirroring newOrganizationsListRouter's convention
// (organizations_list_route_admin_guard_test.go).
func newRetentionRouteRouter(t *testing.T) http.Handler {
	t.Helper()
	database, err := db.New(config.DriverSQLite, "file:retention_route_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	orgFacade := memory.NewFacadeWithOverrides(memory.Overrides{})
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	organizationsHandler := orgshttp.NewHandler(orgFacade, errorRenderer)

	cfg := &config.Config{AdminAPIKey: retentionRouteTestAdminKey}
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

func doRetentionRouteRequest(router http.Handler, method, path, authorizationHeader, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authorizationHeader != "" {
		req.Header.Set("Authorization", authorizationHeader)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestRetentionRoute_GetRejectsARequestWithNoAdminKey(t *testing.T) {
	router := newRetentionRouteRouter(t)

	w := doRetentionRouteRequest(router, http.MethodGet, "/api/v1/organizations/org_1/retention", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestRetentionRoute_PutRejectsAWrongAdminKey(t *testing.T) {
	router := newRetentionRouteRouter(t)

	w := doRetentionRouteRequest(router, http.MethodPut, "/api/v1/organizations/org_1/retention", "Bearer wrong-key", `{"logDays":14}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestRetentionRoute_RoundTripsUnderTheRealAdminGuardedOrgScopedMount is the
// full-stack pin: behind the correct admin key, at the real
// /api/v1/organizations/{orgId}/retention path (not a bespoke test router),
// a PUT is readable back by a subsequent GET — the wiring router.go actually
// ships, not a shortcut.
func TestRetentionRoute_RoundTripsUnderTheRealAdminGuardedOrgScopedMount(t *testing.T) {
	router := newRetentionRouteRouter(t)
	adminAuth := "Bearer " + retentionRouteTestAdminKey

	putW := doRetentionRouteRequest(router, http.MethodPut, "/api/v1/organizations/org_1/retention", adminAuth, `{"logDays":21,"eventDays":0}`)
	if putW.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d; body=%s", putW.Code, http.StatusOK, putW.Body.String())
	}

	getW := doRetentionRouteRequest(router, http.MethodGet, "/api/v1/organizations/org_1/retention", adminAuth, "")
	if getW.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d; body=%s", getW.Code, http.StatusOK, getW.Body.String())
	}
	var dto struct {
		LogDays   *int `json:"logDays"`
		EventDays *int `json:"eventDays"`
	}
	if err := json.Unmarshal(getW.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, getW.Body.String())
	}
	if dto.LogDays == nil || *dto.LogDays != 21 {
		t.Fatalf("logDays = %v, want 21", dto.LogDays)
	}
	if dto.EventDays == nil || *dto.EventDays != 0 {
		t.Fatalf("eventDays = %v, want 0 (unlimited)", dto.EventDays)
	}
}

// TestRetentionRoute_IsOrgScopedUnderTheRealMount pins isolation through the
// real router — writing one org's retention window must never be visible
// through another org's path.
func TestRetentionRoute_IsOrgScopedUnderTheRealMount(t *testing.T) {
	router := newRetentionRouteRouter(t)
	adminAuth := "Bearer " + retentionRouteTestAdminKey

	putA := doRetentionRouteRequest(router, http.MethodPut, "/api/v1/organizations/org_a/retention", adminAuth, `{"logDays":5}`)
	if putA.Code != http.StatusOK {
		t.Fatalf("PUT org_a status = %d, want %d; body=%s", putA.Code, http.StatusOK, putA.Body.String())
	}

	getB := doRetentionRouteRequest(router, http.MethodGet, "/api/v1/organizations/org_b/retention", adminAuth, "")
	if getB.Code != http.StatusOK {
		t.Fatalf("GET org_b status = %d, want %d; body=%s", getB.Code, http.StatusOK, getB.Body.String())
	}
	var dtoB struct {
		LogDays *int `json:"logDays"`
	}
	if err := json.Unmarshal(getB.Body.Bytes(), &dtoB); err != nil {
		t.Fatalf("decode org_b body: %v", err)
	}
	if dtoB.LogDays != nil {
		t.Errorf("org_b's logDays = %v, want nil — org_a's retention window must never leak across", dtoB.LogDays)
	}
}
