package authmw

import (
	"context"
	"net/http"
	"strings"

	"beecon/internal/access"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// Verify authenticates a presented org API key secret and returns the
// organization and scope it carries (PD41, FD4). Satisfied by
// (*access.Facade).Verify, passed as a plain function value so authmw
// carries no import on the access module's concrete Facade type — the
// org-scoped verification behavior is a parameter, not a hardcoded
// dependency.
type Verify func(ctx context.Context, secret string) (access.VerifiedKey, error)

// OrgAuth guards org-scoped endpoints: the request must carry
// `Authorization: Bearer beecon_sk_...`. A valid, unrevoked key's
// organization is injected into the request context via
// organizations.WithOrgID, and its scope via access.WithScope (PD41) —
// handlers must read the organization only through
// organizations.OrgIDFromContext, never from path, body, or query;
// authmw.RequireWrite reads the scope to reject a read-only key on a
// mutating route. A missing, malformed, unknown, or revoked key is rejected
// with the PD5 unauthorized envelope; an infrastructure failure while
// verifying (e.g. a database error) surfaces as 500, never 401 (PD38b,
// Phase 2 review carry-forward) — see writeAuthError.
func OrgAuth(verify Verify) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			secret, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				httpx.WriteDomainError(w, httpx.Unauthorized("missing or malformed api key"))
				return
			}
			verified, err := verify(r.Context(), secret)
			if err != nil {
				writeAuthError(w, err)
				return
			}
			ctx := organizations.WithOrgID(r.Context(), verified.OrgID)
			ctx = access.WithScope(ctx, verified.Scope)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(authorizationHeader string) (string, bool) {
	presented, ok := strings.CutPrefix(authorizationHeader, bearerPrefix)
	if !ok || presented == "" {
		return "", false
	}
	return presented, true
}
