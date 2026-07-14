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
	adminUIHandler http.Handler,
	executionHandler *executionhttp.Handler,
	filesHandler *executionhttp.FilesHandler,
	loggingHandler *logginghttp.Handler,
	triggersHandler *triggershttp.Handler,
	deliveryHandler *deliveryhttp.Handler,
	metricsHandler http.Handler,
	dashboardMetricsHandler http.Handler,
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

	// /connect/style.css is the shared design-token stylesheet the three
	// connect templates link (Slice 10, PD48), replacing their previously
	// duplicated inline <style> blocks. A literal chi route, so it resolves
	// before the "/connect/{token}" pattern above; it carries no token or
	// secret, but stays alongside the other /connect/* routes for locality.
	r.Get("/connect/style.css", connectWebHandler.Stylesheet)

	// /admin/* is the embedded Admin UI (PD47/FD3, Slice 1): a static SPA
	// mount, and /admin/verify is a thin pre-flight check for the gate
	// screen. Both sit outside the logged group below, the same reasoning as
	// /connect/* — the admin key never lands in the chi request log, and the
	// SPA's own static-asset requests would otherwise flood it.
	r.Handle("/admin/*", adminUIHandler)
	r.With(authmw.AdminAuth(cfg.AdminAPIKey)).Get("/admin/verify", authmw.VerifyAdminKey)

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
			// /dashboard/metrics is Slice 3's typed JSON read for the Admin
			// UI's dashboard (architecture doc §3, this slice's "metrics
			// read path" decision): admin-guarded and installation-wide,
			// like GET /metrics itself, sourced from the same registry.
			r.With(authmw.AdminAuth(cfg.AdminAPIKey)).Method(http.MethodGet, "/dashboard/metrics", dashboardMetricsHandler)

			r.Route("/organizations", func(r chi.Router) {
				r.Use(authmw.AdminAuth(cfg.AdminAPIKey))
				r.Post("/", organizationsHandler.Create)
				r.Get("/", organizationsHandler.List)

				// Every /{orgId}/... route — the pre-existing single-organization
				// endpoints and, since Slice 2, the Admin UI's org-scoped console
				// mount — lives under this ONE r.Route("/{orgId}", ...): chi
				// cannot serve a leaf handler and a second, separately-registered
				// subrouter on the identical pattern node (regression pinned by
				// TestBuildRouter_SingleOrganizationGetAndPatchStillWorkAfterTheAdminConsoleMount),
				// so every /{orgId} sub-path is a relative path registered here,
				// never a second top-level r.Route("/{orgId}", ...) call.
				r.Route("/{orgId}", func(r chi.Router) {
					r.Get("/", organizationsHandler.Get)
					r.Patch("/", organizationsHandler.UpdateAllowedRedirectURIs)
					r.Post("/api-keys", accessHandler.Issue)
					r.Get("/api-keys", accessHandler.List)
					r.Delete("/api-keys/{keyId}", accessHandler.Revoke)
					r.Post("/api-keys/{keyId}/rotate", accessHandler.Rotate)
					r.Post("/signing-secrets", accessHandler.IssueSigningSecret)
					r.Get("/signing-secrets", accessHandler.ListSigningSecrets)

					// The Admin UI's org-scoped console routes (Slice 2, FD3): this
					// subtree already sits inside the /organizations block's own
					// AdminAuth (r.Use above), so it only needs {orgId} injected
					// into context — InjectOrgFromPath (not the full AdminOrgScope)
					// avoids checking the admin key a second time. The existing
					// org-key-guarded connections and trigger-instances handlers
					// read org from context exactly like every other org-scoped
					// handler, reused verbatim.
					r.Group(func(r chi.Router) {
						r.Use(authmw.InjectOrgFromPath)

						r.Route("/connections", func(r chi.Router) {
							r.Get("/", connectionsHandler.List)
							r.Get("/{connectionId}", connectionsHandler.Get)
							r.Post("/{connectionId}/disable", connectionsHandler.Disable)
							r.Delete("/{connectionId}", connectionsHandler.Delete)
							r.Post("/{connectionId}/reconnect", connectionsHandler.Reconnect)
						})

						r.Route("/trigger-instances", func(r chi.Router) {
							r.Get("/", triggersHandler.List)
							r.Get("/{trgId}", triggersHandler.Get)
							r.Post("/{trgId}/disable", triggersHandler.Disable)
							r.Post("/{trgId}/enable", triggersHandler.Enable)
							r.Delete("/{trgId}", triggersHandler.Delete)
						})

						// The Admin UI's OBSERVE surfaces (Slice 3): the same
						// existing org-key-guarded logs and events handlers,
						// reused verbatim under the console's org-scoped mount —
						// no new handler, no new port, same reasoning as
						// connections/trigger-instances above.
						r.Route("/logs", func(r chi.Router) {
							r.Get("/", loggingHandler.List)
						})

						r.Route("/events", func(r chi.Router) {
							r.Get("/", deliveryHandler.ListEvents)
							r.Post("/{evtId}/redeliver", deliveryHandler.Redeliver)
						})

						// The Admin UI's ADMINISTER end-users area (Slice 4,
						// PD40): List is the new list-users-per-org read;
						// Post reuses the pre-existing org-scoped
						// CreateUser verbatim, the same "existing handler,
						// admin-key mount" pattern as connections/logs/events
						// above — an operator creating a user from the
						// console is indistinguishable, at the handler
						// level, from the org itself creating one with its
						// own org API key.
						r.Route("/users", func(r chi.Router) {
							r.Get("/", organizationsHandler.ListUsersByOrg)
							r.Post("/", organizationsHandler.CreateUser)
						})

						// The Admin UI's GOVERN > Governance area (Slice 5,
						// PD42/PD43): GetGovernance/UpdateGovernance are new
						// admin-only reads/writes; ListWithVisibility reuses
						// the existing catalogHandler under this same
						// org-scoped console mount (AC1's unfiltered
						// operator view), the same "existing handler,
						// admin-key mount" pattern connections/logs/events
						// above already established.
						r.Route("/governance", func(r chi.Router) {
							r.Get("/", organizationsHandler.GetGovernance)
							r.Put("/", organizationsHandler.UpdateGovernance)
							r.Get("/catalog", catalogHandler.ListWithVisibility)
						})

						// The Admin UI's GOVERN > Settings > Retention area
						// (Slice 7, PD44): GetRetention/UpdateRetention are
						// new admin-only reads/writes over the same
						// org_governance settings row governance itself
						// lives on (FD8) — same org-scoped console mount,
						// same reasoning as governance above.
						r.Route("/retention", func(r chi.Router) {
							r.Get("/", organizationsHandler.GetRetention)
							r.Put("/", organizationsHandler.UpdateRetention)
						})

						// The Admin UI's GOVERN > Settings > Webhook
						// Endpoints area (Slice 8, PD45): the same
						// multi-endpoint CRUD handlers the org-key mount
						// below exposes, reused verbatim under the
						// console's org-scoped mount — admin-key requests
						// carry no scope (RequireWrite's own doc comment),
						// so no requireWrite guard is needed here, the same
						// reasoning already established for
						// connections/logs/events/governance/retention
						// above.
						r.Route("/webhook-endpoints", func(r chi.Router) {
							r.Get("/", deliveryHandler.ListEndpoints)
							r.Post("/", deliveryHandler.CreateEndpoint)
							r.Put("/{wepId}", deliveryHandler.UpdateEndpoint)
							r.Delete("/{wepId}", deliveryHandler.DeleteEndpoint)
							r.Post("/{wepId}/rotate-secret", deliveryHandler.RotateEndpointSecret)
							r.Post("/{wepId}/enable", deliveryHandler.EnableEndpoint)
							r.Post("/{wepId}/disable", deliveryHandler.DisableEndpoint)
						})

						// The Admin UI's GOVERN > Config export/import area
						// (Slice 9, PD46): ExportConfig/ImportConfig are new
						// admin-only reads/writes assembled entirely inside
						// organizations (governance + retention) plus the
						// EndpointPorter port over delivery (app/
						// endpoint_porter.go) — never a secret, never a
						// credential, never delivery imported directly.
						r.Route("/config", func(r chi.Router) {
							r.Get("/export", organizationsHandler.ExportConfig)
							r.Post("/import", organizationsHandler.ImportConfig)
						})
					})
				})
			})

			r.Route("/integrations", func(r chi.Router) {
				r.With(authmw.AdminAuth(cfg.AdminAPIKey)).Post("/", catalogHandler.Create)
				r.With(orgOrUser).Get("/", catalogHandler.List)
				r.With(orgOrUser).Get("/{intgId}/expected-params", catalogHandler.GetExpectedParams)
			})

			// /provider-definitions is Slice 6's new installation-wide operator
			// read (PD40): admin-guarded, no orgId in the path — the raw
			// installed estate, never governance-filtered (AC7), distinct from
			// /integrations' org-facing (and, since Slice 5, governance-filtered)
			// List above. The Admin UI's CATALOG > Providers/Tools/Trigger
			// Definitions area reads this list plus each provider's full bundle
			// detail to render every catalog surface, since a provider
			// definition's bundle already carries every tool and trigger it
			// declares.
			r.Route("/provider-definitions", func(r chi.Router) {
				r.Use(authmw.AdminAuth(cfg.AdminAPIKey))
				r.Get("/", catalogHandler.ListProviderDefinitions)
				r.Get("/{slug}", catalogHandler.GetProviderDefinition)
			})

			// RequireWrite (PD41, Slice 4) rejects a read-only org API key
			// with a scope-explaining 403 on every mutating route below; it
			// reads the scope OrgAuth (or, on an orgOrUser-mounted route,
			// OrgOrUser's org-key branch) injected into context, and passes
			// a user-token or admin-key request straight through — scope is
			// an org-key concept only (BOUNDARIES).
			requireWrite := authmw.RequireWrite

			r.Route("/users", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.With(requireWrite).Post("/", organizationsHandler.CreateUser)
				r.Get("/{userId}", organizationsHandler.GetUser)
			})

			r.Route("/connections", func(r chi.Router) {
				r.With(orgOrUser).Post("/initiate", connectionsHandler.Initiate)
				r.With(orgOrUser).Get("/", connectionsHandler.List)
				r.With(orgOrUser).Get("/{connectionId}", connectionsHandler.Get)
				r.With(authmw.OrgAuth(verifyOrgKey), requireWrite).Post("/{connectionId}/disable", connectionsHandler.Disable)
				r.With(authmw.OrgAuth(verifyOrgKey), requireWrite).Delete("/{connectionId}", connectionsHandler.Delete)
				r.With(orgOrUser, requireWrite).Post("/{connectionId}/reconnect", connectionsHandler.Reconnect)
			})

			r.Route("/tools", func(r chi.Router) {
				r.With(orgOrUser).Get("/", catalogHandler.ListTools)
				r.With(authmw.OrgAuth(verifyOrgKey)).Get("/{slug}", catalogHandler.GetTool)
				r.With(authmw.OrgAuth(verifyOrgKey), requireWrite).Post("/{slug}/execute", executionHandler.Execute)
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
				r.With(requireWrite).Post("/", triggersHandler.Create)
				r.Get("/", triggersHandler.List)
				r.Get("/{trgId}", triggersHandler.Get)
				r.With(requireWrite).Post("/{trgId}/disable", triggersHandler.Disable)
				r.With(requireWrite).Post("/{trgId}/enable", triggersHandler.Enable)
				r.With(requireWrite).Delete("/{trgId}", triggersHandler.Delete)
			})

			// /files is org-key-only (PD22, Slice 7): never mounted under
			// orgOrUser — a user token must be rejected (closes Slice 5's
			// deferred AC9).
			r.Route("/files", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.With(requireWrite).Post("/", filesHandler.Upload)
				r.Get("/{fileId}/download", filesHandler.Download)
			})

			r.Route("/logs", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.Get("/", loggingHandler.List)
			})

			// /webhook-endpoint and /events are Slice 3's signed channel
			// (PD27/PD30/PD31): every route is org-key-only (no
			// browser-facing subset). "test" IS RequireWrite-guarded even
			// though PD41's explicit list only names "set/rotate": SendTest
			// enqueues a real outbox Event (persists an evt_ row visible in
			// the events list) and nudges the dispatcher to make a genuine
			// outbound HTTP delivery — it is neither a listing nor an
			// inspection call, so PD41 ("read-only keys pass on
			// listing/inspection, rejected on any mutating call") requires
			// it to reject a read-only key.
			r.Route("/webhook-endpoint", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.With(requireWrite).Put("/", deliveryHandler.SetEndpoint)
				r.Get("/", deliveryHandler.GetEndpoint)
				r.With(requireWrite).Post("/rotate-secret", deliveryHandler.RotateSecret)
				r.With(requireWrite).Post("/test", deliveryHandler.SendTest)
			})

			// /webhook-endpoints (plural) is Slice 8's multi-endpoint CRUD
			// surface (PD45): an org may register up to
			// BEECON_WEBHOOK_ENDPOINT_CAP endpoints, each with its own
			// event-type filter, status, and secret. The singular
			// /webhook-endpoint block above stays exactly as Slice 3 left
			// it — a compatibility alias over the org's first endpoint —
			// so existing SDK/Phase 3 callers keep working unchanged.
			r.Route("/webhook-endpoints", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.Get("/", deliveryHandler.ListEndpoints)
				r.With(requireWrite).Post("/", deliveryHandler.CreateEndpoint)
				r.With(requireWrite).Put("/{wepId}", deliveryHandler.UpdateEndpoint)
				r.With(requireWrite).Delete("/{wepId}", deliveryHandler.DeleteEndpoint)
				r.With(requireWrite).Post("/{wepId}/rotate-secret", deliveryHandler.RotateEndpointSecret)
				r.With(requireWrite).Post("/{wepId}/enable", deliveryHandler.EnableEndpoint)
				r.With(requireWrite).Post("/{wepId}/disable", deliveryHandler.DisableEndpoint)
			})

			r.Route("/events", func(r chi.Router) {
				r.Use(authmw.OrgAuth(verifyOrgKey))
				r.Get("/", deliveryHandler.ListEvents)
				r.With(requireWrite).Post("/{evtId}/redeliver", deliveryHandler.Redeliver)
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
