package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"

	"beecon/internal/config"
	memory "beecon/internal/connections/driven/memory"
	"beecon/internal/connectweb"
	"beecon/internal/db"
)

// TestBuildRouter_ConnectRoutesAreNeverPassedThroughRequestLogging pins the
// before-merge fix to router.go: /connect/{token} and /connect/oauth/callback
// are registered outside the r.Group that applies middleware.Logger, so a
// connect token and an OAuth authorization code/CSRF state never reach the
// request log, while /health (the logged group) still does.
//
// It substitutes middleware.DefaultLogger — the package-level var
// middleware.Logger itself delegates to, and chi documents as reconfigurable
// for exactly this kind of custom-logging substitution — with a recording
// middleware before building the router, then restores it. chi builds each
// route's middleware chain synchronously as routes are registered, so the
// substitution must (and does) happen before buildRouter runs.
//
// Every other handler is left nil: each /api/v1 sub-route requires an API key
// first, so it never reaches its nil handler. connectWebHandler is real (an
// in-memory connections facade with no data seeded), so the two connect
// requests below resolve through connectweb's own "not found" error path
// rather than panicking — only the logged-path wiring is under test here, not
// connectweb's own behavior (covered in internal/connectweb/handler_test.go
// and the OAuth handshake journey).
func TestBuildRouter_ConnectRoutesAreNeverPassedThroughRequestLogging(t *testing.T) {
	database, err := db.New(config.DriverSQLite, "file:connect_routes_bypass_logging_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	connectWebHandler, err := connectweb.NewHandler(memory.NewFacadeWithOverrides(memory.Overrides{}))
	if err != nil {
		t.Fatalf("connectweb.NewHandler: %v", err)
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
	router := buildRouter(cfg, database, nil, nil, nil, nil, connectWebHandler, nil, nil, nil)

	if w := get(router, "/health"); w.Code != http.StatusOK {
		t.Fatalf("/health status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	get(router, "/connect/does-not-exist")
	get(router, "/connect/oauth/callback?code=the-secret-auth-code&state=the-csrf-state")

	if len(loggedRequests) != 1 {
		t.Fatalf("logged requests = %v, want exactly one (the /health request)", loggedRequests)
	}
	if loggedRequests[0] != "/health" {
		t.Errorf("logged request = %q, want %q", loggedRequests[0], "/health")
	}
	for _, logged := range loggedRequests {
		if strings.Contains(logged, "the-secret-auth-code") || strings.Contains(logged, "the-csrf-state") {
			t.Fatalf("logged requests %v must never contain the connect token, OAuth code, or CSRF state", loggedRequests)
		}
	}
}

func get(router http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}
