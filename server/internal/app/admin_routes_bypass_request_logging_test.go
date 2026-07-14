package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"

	"beecon/internal/adminui"
	"beecon/internal/config"
	"beecon/internal/db"
)

// TestBuildRouter_AdminRoutesAreNeverPassedThroughRequestLogging mirrors
// TestBuildRouter_ConnectRoutesAreNeverPassedThroughRequestLogging
// (connect_routes_bypass_request_logging_test.go): /admin/* and
// /admin/verify are registered outside the r.Group that applies
// middleware.Logger, the same reasoning as /connect/* — the admin key on
// /admin/verify's Authorization header, and the SPA's own static-asset
// requests, must never flood (or leak into) the request log. /health (the
// logged group) still does get logged, as the positive control.
func TestBuildRouter_AdminRoutesAreNeverPassedThroughRequestLogging(t *testing.T) {
	database, err := db.New(config.DriverSQLite, "file:admin_routes_bypass_logging_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	adminUIHandler, err := adminui.Handler()
	if err != nil {
		t.Fatalf("adminui.Handler(): unexpected error: %v", err)
	}

	var loggedRequests []string
	originalLogger := middleware.DefaultLogger
	middleware.DefaultLogger = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			loggedRequests = append(loggedRequests, r.URL.String())
			next.ServeHTTP(w, r)
		})
	}
	t.Cleanup(func() { middleware.DefaultLogger = originalLogger })

	cfg := &config.Config{AdminAPIKey: "test-admin-key"}
	router := buildRouter(cfg, database, nil, nil, nil, nil, nil, adminUIHandler, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	if w := get(router, "/health"); w.Code != http.StatusOK {
		t.Fatalf("/health status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	get(router, "/admin/organizations")
	get(router, "/admin/verify")
	get(router, "/admin/verify?ignored=test-admin-key-in-query-should-never-appear-logged")

	if len(loggedRequests) != 1 {
		t.Fatalf("logged requests = %v, want exactly one (the /health request)", loggedRequests)
	}
	if loggedRequests[0] != "/health" {
		t.Errorf("logged request = %q, want %q", loggedRequests[0], "/health")
	}
}

// TestBuildRouter_AdminVerifyIsGuardedByAdminAuth pins that /admin/verify —
// even though it sits outside the logged group — still requires the admin
// key: a missing key gets 401, and the correct key gets 204 (FD3).
func TestBuildRouter_AdminVerifyIsGuardedByAdminAuth(t *testing.T) {
	database, err := db.New(config.DriverSQLite, "file:admin_verify_guard_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	adminUIHandler, err := adminui.Handler()
	if err != nil {
		t.Fatalf("adminui.Handler(): unexpected error: %v", err)
	}

	cfg := &config.Config{AdminAPIKey: "test-admin-key"}
	router := buildRouter(cfg, database, nil, nil, nil, nil, nil, adminUIHandler, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	if w := get(router, "/admin/verify"); w.Code != http.StatusUnauthorized {
		t.Fatalf("no key: status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/verify", nil)
	req.Header.Set("Authorization", "Bearer test-admin-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("valid key: status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
}
