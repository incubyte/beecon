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
	deliveryhttp "beecon/internal/delivery/driving/httpapi"
	executionhttp "beecon/internal/execution/driving/httpapi"
	logginghttp "beecon/internal/logging/driving/httpapi"
	orgshttp "beecon/internal/organizations/driving/httpapi"
	triggershttp "beecon/internal/triggers/driving/httpapi"
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
	filesHandler *executionhttp.FilesHandler,
	loggingHandler *logginghttp.Handler,
	triggersHandler *triggershttp.Handler,
	deliveryHandler *deliveryhttp.Handler,
	metricsHandler http.Handler,
	verifyOrgKey authmw.Verify,
	verifyUserToken authmw.VerifyUserToken,
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
	r.Post("/connect/{token}/params", connectWebHandler.SubmitParams)
	r.Get("/connect/oauth/callback", connectWebHandler.Callback)

	// orgOrUser guards the browser-facing subset of the API (PD20): tools
	// list, expected-params, initiate connection, list/get own connections,
	// and reconnect own connection all accept either an org API key or a
	// user-scoped browser token. Every other route below stays
	// org-key/admin-only — including tool execution, file upload, user
	// creation, and logs (Slice 5, AC9).
	orgOrUser := authmw.OrgOrUser(verifyOrgKey, verifyUserToken)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Logger)

		r.Get("/health", healthHandler(database))

		// /metrics is PD24's operability endpoint: a Prometheus text-format
		// scrape target, admin-guarded (never an org API key) since it exposes
		// cross-organization operational signals, not a tenant's own data.
		r.With(authmw.AdminAuth(cfg.AdminAPIKey)).Method(http.MethodGet, "/metrics", metricsHandler)

		r.Route("/api/v1", func(r chi.Router) {
			r.Route("/organizations", func(r chi.Router) {
				r.Use(authmw.AdminAuth(cfg.AdminAPIKey))
				r.Post("/", organizationsHandler.Create)
				r.Get("/{orgId}", organizationsHandler.Get)
				r.Patch("/{orgId}", organizationsHandler.UpdateAllowedRedirectURIs)
				r.Post("/{orgId}/api-keys", accessHandler.Issue)
				r.Get("/{orgId}/api-keys", accessHandler.List)
				r.Delete("/{orgId}/api-keys/{keyId}", accessHandler.Revoke)
				r.Post("/{orgId}/api-keys/{keyId}/rotate", accessHandler.Rotate)
				r.Post("/{orgId}/signing-secrets", accessHandler.IssueSigningSecret)
				r.Get("/{orgId}/signing-secrets", accessHandler.ListSigningSecrets)
			})

			r.Route("/integrations", func(r chi.Router) {
				r.With(authmw.AdminAuth(cfg.AdminAPIKey)).Post("/", catalogHandler.Create)
				r.With(orgOrUser).Get("/", catalogHandler.List)
				r.With(orgOrUser).Get("/{intgId}/expected-params", catalogHandler.GetExpectedParams)
			})

			r.Route("/users", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.Post("/", organizationsHandler.CreateUser)
				r.Get("/{userId}", organizationsHandler.GetUser)
			})

			r.Route("/connections", func(r chi.Router) {
				r.With(orgOrUser).Post("/initiate", connectionsHandler.Initiate)
				r.With(orgOrUser).Get("/", connectionsHandler.List)
				r.With(orgOrUser).Get("/{connectionId}", connectionsHandler.Get)
				r.With(authmw.OrgAuth(verifyOrgKey)).Post("/{connectionId}/disable", connectionsHandler.Disable)
				r.With(authmw.OrgAuth(verifyOrgKey)).Delete("/{connectionId}", connectionsHandler.Delete)
				r.With(orgOrUser).Post("/{connectionId}/reconnect", connectionsHandler.Reconnect)
			})

			r.Route("/tools", func(r chi.Router) {
				r.With(orgOrUser).Get("/", catalogHandler.ListTools)
				r.With(authmw.OrgAuth(verifyOrgKey)).Get("/{slug}", catalogHandler.GetTool)
				r.With(authmw.OrgAuth(verifyOrgKey)).Post("/{slug}/execute", executionHandler.Execute)
			})

			// /trigger-definitions is Slice 1's catalog API (PD28/PD35): list
			// accepts either an org API key or a user-scoped browser token (API
			// Shape), get-by-slug is org-key-only, mirroring /tools' own split.
			r.Route("/trigger-definitions", func(r chi.Router) {
				r.With(orgOrUser).Get("/", catalogHandler.ListTriggerDefinitions)
				r.With(authmw.OrgAuth(verifyOrgKey)).Get("/{slug}", catalogHandler.GetTriggerDefinition)
			})

			// /trigger-instances is Slice 2's lifecycle API (PD33): every route
			// is org-key-only (no browser-facing subset today).
			r.Route("/trigger-instances", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.Post("/", triggersHandler.Create)
				r.Get("/", triggersHandler.List)
				r.Get("/{trgId}", triggersHandler.Get)
				r.Post("/{trgId}/disable", triggersHandler.Disable)
				r.Post("/{trgId}/enable", triggersHandler.Enable)
				r.Delete("/{trgId}", triggersHandler.Delete)
			})

			// /files is org-key-only (PD22, Slice 7): never mounted under
			// orgOrUser — a user token must be rejected (closes Slice 5's
			// deferred AC9).
			r.Route("/files", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.Post("/", filesHandler.Upload)
				r.Get("/{fileId}/download", filesHandler.Download)
			})

			r.Route("/logs", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.Get("/", loggingHandler.List)
			})

			// /webhook-endpoint and /events are Slice 3's signed channel
			// (PD27/PD30/PD31): every route is org-key-only (no
			// browser-facing subset).
			r.Route("/webhook-endpoint", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.Put("/", deliveryHandler.SetEndpoint)
				r.Get("/", deliveryHandler.GetEndpoint)
				r.Post("/rotate-secret", deliveryHandler.RotateSecret)
				r.Post("/test", deliveryHandler.SendTest)
			})

			r.Route("/events", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.Get("/", deliveryHandler.ListEvents)
				r.Post("/{evtId}/redeliver", deliveryHandler.Redeliver)
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
