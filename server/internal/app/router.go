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
	orgshttp "beecon/internal/organizations/driving/httpapi"
)

func buildRouter(
	cfg *config.Config,
	database *upstreambun.DB,
	organizationsHandler *orgshttp.Handler,
	accessHandler *accesshttp.Handler,
	catalogHandler *cataloghttp.Handler,
	connectionsHandler *connectionshttp.Handler,
	verifyOrgKey authmw.Verify,
) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
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
