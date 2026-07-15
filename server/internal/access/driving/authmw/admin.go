// Package authmw is the shared HTTP auth middleware for installation- and
// org-level operations. It is a cross-cutting concern, not a feature, so it
// lives outside any single module's driving adapter.
package authmw

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"beecon/internal/access"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

const bearerPrefix = "Bearer "

// AdminAuth guards installation-level endpoints (PD1): the request must carry
// `Authorization: Bearer <BEECON_ADMIN_API_KEY>`, compared in constant time.
// A missing or wrong key is rejected with the PD5 unauthorized envelope.
func AdminAuth(adminKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isValidAdminKey(r.Header.Get("Authorization"), adminKey) {
				httpx.WriteDomainError(w, httpx.Unauthorized("missing or invalid admin key"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// InjectOrgFromPath reads the {orgId} path param and injects it into
// context via organizations.WithOrgID — no auth check of its own. Use this
// when mounting the console's org-scoped routes *inside* a route tree an
// outer middleware already guards (Slice 2: the
// /organizations/{orgId}/connections and /trigger-instances mount sits
// inside the /organizations block's own guard, ConsoleAuth since Phase 5),
// so the guard runs exactly once per request rather than once by the outer
// middleware and again here.
func InjectOrgFromPath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgID := organizations.OrgID(chi.URLParam(r, "orgId"))
		ctx := organizations.WithOrgID(r.Context(), orgID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireWrite rejects a read-only org API key on a mutating route with a
// scope-explaining 403 (PD41, Slice 4). It reads the scope OrgAuth (or
// OrgOrUser's org-key branch) injected into context via
// access.ScopeFromContext; a request carrying no scope at all — an
// admin-key request (AdminAuth/ConsoleAuth inject no scope) or a
// user-token request (UserAuth/OrgOrUser's user-token branch inject none
// either) — passes through untouched, since scope is an org-key concept
// only (BOUNDARIES). Mount this behind OrgAuth/OrgOrUser on every org-key
// mutating route (connections disable/delete/reconnect, tools execute,
// users create, trigger-instances create/disable/enable/delete, files
// upload, webhook-endpoint set/rotate, events redeliver) — never on an
// admin-key or user-token-only route.
func RequireWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if scope, ok := access.ScopeFromContext(r.Context()); ok && scope.IsReadOnly() {
			httpx.WriteDomainError(w, httpx.Forbidden("a read-only api key cannot perform this action"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isValidAdminKey(authorizationHeader, adminKey string) bool {
	presented, ok := strings.CutPrefix(authorizationHeader, bearerPrefix)
	if !ok || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(adminKey)) == 1
}
