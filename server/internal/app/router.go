package app

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	upstreambun "github.com/uptrace/bun"

	"beecon/internal/access/driving/authmw"
	"beecon/internal/config"
	orgshttp "beecon/internal/organizations/driving/httpapi"
)

func buildRouter(cfg *config.Config, database *upstreambun.DB, organizationsHandler *orgshttp.Handler) chi.Router {
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
