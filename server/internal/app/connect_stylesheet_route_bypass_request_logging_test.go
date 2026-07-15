package app

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5/middleware"

	"beecon/internal/config"
	memory "beecon/internal/connections/driven/memory"
	"beecon/internal/connectweb"
	"beecon/internal/db"
)

// TestBuildRouter_StyleCSSRouteIsNeverPassedThroughRequestLogging pins Slice
// 10's /connect/style.css mount (PD48): router.go registers it outside the
// r.Group that applies middleware.Logger, the same reasoning already pinned
// for /connect/{token} and /connect/oauth/callback in
// connect_routes_bypass_request_logging_test.go — reused here so a future
// regression that accidentally moves style.css inside the logged group fails
// loudly, while /health (the logged group) still gets logged as the positive
// control.
//
// Reuses this package's get helper, declared in
// connect_routes_bypass_request_logging_test.go.
func TestBuildRouter_StyleCSSRouteIsNeverPassedThroughRequestLogging(t *testing.T) {
	database, err := db.New(config.DriverSQLite, "file:connect_stylesheet_bypass_logging_test?mode=memory&cache=shared")
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
	router := buildRouter(cfg, database, nil, nil, nil, nil, connectWebHandler, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	if w := get(router, "/health"); w.Code != http.StatusOK {
		t.Fatalf("/health status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if w := get(router, "/connect/style.css"); w.Code != http.StatusOK {
		t.Fatalf("/connect/style.css status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	if len(loggedRequests) != 1 {
		t.Fatalf("logged requests = %v, want exactly one (the /health request)", loggedRequests)
	}
	if loggedRequests[0] != "/health" {
		t.Errorf("logged request = %q, want %q", loggedRequests[0], "/health")
	}
}
