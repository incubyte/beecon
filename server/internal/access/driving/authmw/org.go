package authmw

import (
	"context"
	"net/http"
	"strings"

	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// Verify authenticates a presented org API key secret and returns the
// organization it belongs to. Satisfied by (*access.Facade).Verify, passed
// as a plain function value so authmw carries no import on the access
// module's concrete Facade type — the org-scoped verification behavior is a
// parameter, not a hardcoded dependency.
type Verify func(ctx context.Context, secret string) (organizations.OrgID, error)

// OrgAuth guards org-scoped endpoints: the request must carry
// `Authorization: Bearer beecon_sk_...`. A valid, unrevoked key's
// organization is injected into the request context via
// organizations.WithOrgID; handlers must read it only through
// organizations.OrgIDFromContext — never from path, body, or query. A
// missing, malformed, unknown, or revoked key is rejected with the PD5
// unauthorized envelope; an infrastructure failure while verifying (e.g. a
// database error) surfaces as 500, never 401 (PD38b, Phase 2 review
// carry-forward) — see writeAuthError.
func OrgAuth(verify Verify) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			secret, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				httpx.WriteDomainError(w, httpx.Unauthorized("missing or malformed api key"))
				return
			}
			org, err := verify(r.Context(), secret)
			if err != nil {
				writeAuthError(w, err)
				return
			}
			ctx := organizations.WithOrgID(r.Context(), org)
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
