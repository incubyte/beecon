package app

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	upstreambun "github.com/uptrace/bun"

	"beecon/internal/access/driving/authmw"
	accesshttp "beecon/internal/access/driving/httpapi"
	cataloghttp "beecon/internal/catalog/driving/httpapi"
	"beecon/internal/config"
	connectionshttp "beecon/internal/connections/driving/httpapi"
	"beecon/internal/connectweb"
	executionhttp "beecon/internal/execution/driving/httpapi"
	logginghttp "beecon/internal/logging/driving/httpapi"
	orgshttp "beecon/internal/organizations/driving/httpapi"
)

func buildRouter(
	cfg *config.Config,
	database *upstreambun.DB,
	organizationsHandler *orgshttp.Handler,
	accessHandler *accesshttp.Handler,
	catalogHandler *cataloghttp.Handler,
	connectionsHandler *connectionshttp.Handler,
	connectWebHandler *connectweb.Handler,
	executionHandler *executionhttp.Handler,
	loggingHandler *logginghttp.Handler,
	verifyOrgKey authmw.Verify,
) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	// /connect/* are the middle-man pages the end user's browser visits
	// directly (AC1-AC9): unauthenticated by an org API key — the single-use
	// connect token, and later the CSRF state, are the credentials. They sit
	// outside the logged group below so the connect token and the OAuth
	// authorization code never land in the chi request log's path/query.
	r.Get("/connect/{token}", connectWebHandler.ConnectPage)
	r.Get("/connect/oauth/callback", connectWebHandler.Callback)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Logger)

		r.Get("/health", healthHandler(database))

		r.Route("/api/v1", func(r chi.Router) {
			r.Route("/organizations", func(r chi.Router) {
				r.Use(authmw.AdminAuth(cfg.AdminAPIKey))
				r.Post("/", organizationsHandler.Create)
				r.Get("/{orgId}", organizationsHandler.Get)
				r.Patch("/{orgId}", organizationsHandler.UpdateAllowedRedirectURIs)
				r.Post("/{orgId}/api-keys", accessHandler.Issue)
				r.Get("/{orgId}/api-keys", accessHandler.List)
				r.Delete("/{orgId}/api-keys/{keyId}", accessHandler.Revoke)
			})

			r.Route("/integrations", func(r chi.Router) {
				r.With(authmw.AdminAuth(cfg.AdminAPIKey)).Post("/", catalogHandler.Create)
				r.With(authmw.OrgAuth(verifyOrgKey)).Get("/", catalogHandler.List)
			})

			r.Route("/users", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.Post("/", organizationsHandler.CreateUser)
				r.Get("/{userId}", organizationsHandler.GetUser)
			})

			r.Route("/connections", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.Post("/initiate", connectionsHandler.Initiate)
				r.Get("/{connectionId}", connectionsHandler.Get)
			})

			r.Route("/tools", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.Get("/", catalogHandler.ListTools)
				r.Get("/{slug}", catalogHandler.GetTool)
				r.Post("/{slug}/execute", executionHandler.Execute)
			})

			r.Route("/logs", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.Get("/", loggingHandler.List)
			})
		})
	})

	return r
}

func healthHandler(database *upstreambun.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := database.PingContext(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}
